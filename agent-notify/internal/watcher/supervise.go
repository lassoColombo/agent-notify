//go:build unix

package watcher

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"syscall"
	"time"

	"github.com/lassoColombo/agent-notify/internal/config"
	"github.com/lassoColombo/agent-notify/internal/paths"
	"github.com/lassoColombo/agent-notify/logs"
)

// This file is the supervisor, and it is a RECONCILER INSIDE THE SWEEP THAT
// ALREADY RUNS rather than a scheduler of its own (D-23).
//
// Each tick asks one question per integration — should this be running, and is
// it? — and starts whatever is missing. That shape is what makes configuration
// reload free: an integration removed from the file simply stops being started,
// one added gets started on the next tick, and there is no second code path for
// the two to disagree about.
//
// There is deliberately no exponential backoff. Backoff exists to prevent
// restart storms, and a storm cannot happen when the fastest possible retry is
// one per tick. What it would have bought is already bought, and it would have
// cost a timer and a reset rule per child.

// steadyPeriod is how long a child must stay connected before its failures are
// forgiven.
//
// "Consecutive failures" cannot mean "since the last handshake": a child that
// connects and dies immediately would reset the count for ever and be restarted
// until the end of time. A minute is long enough that a crash loop cannot reach
// it and short enough that a display which ran all day and then died starts
// again from zero, which is the behaviour a person expects (§A9.4).
const steadyPeriod = time.Minute

// stopGrace is how long a child gets to exit on SIGTERM before SIGKILL.
const stopGrace = 2 * time.Second

// A child is one integration the session-watcher started.
type child struct {
	name    string
	binary  string
	command *exec.Cmd
	started time.Time

	// connectedAt is when this pid completed a handshake, zero if it never has.
	connectedAt time.Time
	// exited is set by the goroutine waiting on it. The tick is not how a death
	// is noticed — it is only when it is acted on, which bounds recovery at one
	// tick (§A9.4).
	exited bool
	why    string

	failures int
	gaveUp   string
}

// Integration is one line of the supervisor's report, written to disk for
// doctor.
type Integration struct {
	Name      string   `json:"name"`
	Binary    string   `json:"binary"`
	State     string   `json:"state"`
	PID       int      `json:"pid,omitempty"`
	Since     string   `json:"since,omitempty"`
	Failures  int      `json:"failures,omitempty"`
	Attempts  int      `json:"attempts,omitempty"`
	Reason    string   `json:"reason,omitempty"`
	Supervise bool     `json:"supervised"`
	Roles     []string `json:"roles,omitempty"`
}

// Report is what the session-watcher knows about its integrations.
//
// It is a FILE rather than a socket message, and that is a diagnostic decision
// rather than a lazy one: doctor's hardest job is a session-watcher that holds
// the lock and does not answer (§A9.2), and a report you have to ask for over
// the socket is exactly the report you cannot get in that case. This one is
// also readable with `cat`.
type Report struct {
	PID          int           `json:"pid"`
	Written      string        `json:"written"`
	Integrations []Integration `json:"integrations"`
}

// supervisable is every integration the session-watcher should keep running.
//
// The rule derives the answer from configuration that already exists rather
// than adding a key that configures a role (§A10.3): an integration has a
// binary, is enabled, and is NOT a container. Containers are named in
// `[container] order` — the one thing about them that genuinely is configured,
// because nesting is not discoverable — and they are programs that get run when
// something needs an answer, not daemons that stay connected (D-38).
func supervisable(settings config.Config) []string {
	var wanted []string
	for name, integration := range settings.Integration {
		if !integration.IsEnabled() || integration.Binary == "" {
			continue
		}
		if slices.Contains(settings.Container.Order, name) {
			continue
		}
		wanted = append(wanted, name)
	}
	slices.Sort(wanted)
	return wanted
}

// supervise is one tick of the reconciler.
func (w *Watcher) supervise() {
	settings := w.opened.Settings
	wanted := supervisable(settings)
	listening := w.subs.Listening()

	if w.children == nil {
		w.children = map[string]*child{}
	}

	// Anything no longer wanted is stopped. An integration disabled or deleted
	// from the file goes away here, with no code that knows it was a reload.
	for name, running := range w.children {
		if !slices.Contains(wanted, name) {
			w.stopChild(running, "no longer configured")
			delete(w.children, name)
		}
	}

	for _, name := range wanted {
		running, have := w.children[name]
		if !have {
			w.startIfAllowed(name, settings)
			continue
		}

		if dead, why := w.isDead(running); dead {
			running.why = why
			w.bury(running, settings)
			delete(w.children, name)
			w.startIfAllowed(name, settings)
			continue
		}

		// Has it connected? The handshake is the health check.
		if connected, since := whoIs(listening, running); connected {
			if running.connectedAt.IsZero() {
				running.connectedAt = since
				w.logger.Info("integration connected",
					"integration", name, "pid", running.command.Process.Pid)
			}
			if running.failures > 0 && time.Since(running.connectedAt) > steadyPeriod {
				w.logger.Info("integration is steady; its failures are forgiven",
					"integration", name, "were", running.failures)
				running.failures = 0
			}
			continue
		}

		if running.connectedAt.IsZero() && time.Since(running.started) > IntegrationGrace {
			// Started, never handshook, and out of time. Without this rule a
			// child that hangs before connecting accumulates for ever.
			w.stopChild(running, fmt.Sprintf(
				"did not connect within %s", IntegrationGrace))
			running.exited, running.why = true, "did not connect"
			w.bury(running, settings)
			delete(w.children, name)
		}
	}
	w.writeReport(settings)
}

// isDead reads what the waiting goroutine wrote. Both halves of that exchange
// go through the session-watcher's one mutex, because a child's exit is
// reported to us on its own goroutine and read on the tick.
func (w *Watcher) isDead(running *child) (bool, string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return running.exited, running.why
}

// whoIs matches a connection to a child.
//
// By pid where the kernel can say, which is exact, and by name where it cannot.
// The difference matters when somebody starts their own copy of an integration
// by hand: a self-started subscriber sharing a name would otherwise make a hung
// child look healthy for ever.
func whoIs(listening []Listener, running *child) (bool, time.Time) {
	if running.command == nil || running.command.Process == nil {
		return false, time.Time{}
	}
	pid := running.command.Process.Pid
	for _, one := range listening {
		if one.PID != 0 {
			if one.PID == pid {
				return true, one.Since
			}
			continue
		}
		if one.Name == running.name {
			return true, one.Since
		}
	}
	return false, time.Time{}
}

// startIfAllowed starts one integration, unless it has already been given up on.
func (w *Watcher) startIfAllowed(name string, settings config.Config) {
	if previous, had := w.retired[name]; had && previous != "" {
		return
	}

	binary := settings.Integration[name].Binary
	started, err := w.startChild(name, binary)
	if err != nil {
		// A binary that does not exist reaches the limit quickly, with the
		// message naming the path that was looked at — which is the whole
		// point of counting rather than retrying for ever.
		w.countFailure(name, binary, err.Error(), settings)
		return
	}
	w.children[name] = started
}

// startChild spawns one integration and watches for its death.
func (w *Watcher) startChild(name, binary string) (*child, error) {
	program := binary
	if !filepath.IsAbs(program) {
		found, err := exec.LookPath(program)
		if err != nil {
			return nil, fmt.Errorf("%s is not runnable: %w", binary, err)
		}
		program = found
	}

	command := exec.Command(program)
	command.Dir = "/"
	command.Env = keptEnvironment()
	// Its own process group, so that stopping it stops anything it started, and
	// so that a terminal signal aimed at us does not reach it twice.
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	defer null.Close()
	command.Stdin, command.Stdout = null, null

	// Its stderr is the log file itself, which is the answer to "where does a
	// supervised integration's diagnostics go" (Q17) and is deliberately not a
	// channel: an integration logs its own errors through logs.Open, into this
	// same file, tagged with its own name, and nothing here reads or parses a
	// word of what it says.
	//
	// What arrives on this handle is therefore only what no program can log for
	// itself — a panic on a goroutine other than the one holding the recover, a
	// runtime fatal error, a crash inside cgo — which the runtime writes to
	// fd 2 over the program's head. Losing that would mean a child that died of
	// the least explicable thing is also the one that died saying nothing.
	command.Stderr = null
	if file, err := logs.File(w.opened.Layout.LogFile()); err == nil {
		defer file.Close()
		command.Stderr = file
	}

	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("cannot start %s: %w", program, err)
	}
	started := &child{name: name, binary: program, command: command, started: time.Now()}

	go func() {
		err := command.Wait()
		w.mu.Lock()
		started.exited = true
		started.why = "exited"
		if err != nil {
			started.why = err.Error()
		}
		w.mu.Unlock()
	}()

	w.logger.Info("started an integration", "integration", name,
		"binary", program, "pid", command.Process.Pid)
	return started, nil
}

// bury counts what a dead child's death was worth.
func (w *Watcher) bury(dead *child, settings config.Config) {
	steady := !dead.connectedAt.IsZero() && time.Since(dead.connectedAt) > steadyPeriod
	if steady {
		// It worked for long enough to count as working. A display that ran all
		// day and then crashed starts again from zero.
		w.logger.Info("integration died after a good run",
			"integration", dead.name, "ran for", time.Since(dead.connectedAt).Round(time.Second).String())
		w.mu.Lock()
		delete(w.failures, dead.name)
		w.mu.Unlock()
		return
	}
	w.countFailure(dead.name, dead.binary, dead.why, settings)
}

// countFailure records one failed attempt and gives up at the limit.
//
// The log records STATE CHANGES rather than attempts: "zellij failed after 5
// attempts, binary not found at …" once, not on every retry. A supervisor that
// spams is a supervisor nobody reads (§A9.4).
func (w *Watcher) countFailure(name, binary, why string, settings config.Config) {
	w.mu.Lock()
	if w.failures == nil {
		w.failures = map[string]int{}
	}
	w.failures[name]++
	count := w.failures[name]
	w.mu.Unlock()

	if count < settings.IntegrationTries {
		w.logger.Warn("integration failed", "integration", name,
			"attempt", count, "of", settings.IntegrationTries, "why", why)
		return
	}

	reason := fmt.Sprintf("%s failed %d times; last: %s", name, count, why)
	w.mu.Lock()
	if w.retired == nil {
		w.retired = map[string]string{}
	}
	already := w.retired[name] != ""
	w.retired[name] = reason
	w.mu.Unlock()

	if !already {
		w.logger.Error("gave up on an integration", "integration", name,
			"binary", binary, "attempts", count, "why", why,
			"to retry", "fix it and run `agent-notify watcher reload`")
	}
}

// stopChild is SIGTERM, a short grace, then SIGKILL — to the process group, so
// that anything it started goes with it.
func (w *Watcher) stopChild(running *child, why string) {
	if running.command == nil || running.command.Process == nil {
		return
	}
	pid := running.command.Process.Pid
	w.logger.Info("stopping an integration", "integration", running.name, "pid", pid, "why", why)

	_ = syscall.Kill(-pid, syscall.SIGTERM)
	deadline := time.Now().Add(stopGrace)
	for time.Now().Before(deadline) {
		if gone, _ := w.isDead(running); gone {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}

// StopIntegrations is shutdown: everything we started, stopped, before we go.
func (w *Watcher) StopIntegrations() {
	w.stopAll("the session-watcher is stopping")
}

// restartIntegrations stops every child so that the next reconcile starts it
// again, which is the whole of what a reload has to do to them.
//
// It is not optional politeness. An integration reads its own settings ONCE, at
// startup, out of the same file this just re-read (§A10.4) — so without this a
// reload changes core's mind and nothing else, and a person who has just edited
// their glyphs sees no change and concludes the setting does nothing. Measured
// the hard way: a rebuilt display kept running the old binary through a reload.
func (w *Watcher) restartIntegrations() {
	w.stopAll("the configuration was re-read")
}

func (w *Watcher) stopAll(reason string) {
	for name, running := range w.children {
		w.stopChild(running, reason)
		delete(w.children, name)
	}
}

// writeReport puts what the supervisor knows on disk, for doctor.
func (w *Watcher) writeReport(settings config.Config) {
	report := Report{PID: os.Getpid(), Written: time.Now().UTC().Format(time.RFC3339)}
	listening := w.subs.Listening()

	seen := map[string]bool{}
	for _, name := range supervisable(settings) {
		seen[name] = true
		line := Integration{Name: name, Binary: settings.Integration[name].Binary, Supervise: true}

		w.mu.Lock()
		line.Failures = w.failures[name]
		line.Reason = w.retired[name]
		w.mu.Unlock()
		line.Attempts = settings.IntegrationTries

		switch running, have := w.children[name]; {
		case line.Reason != "":
			line.State = "gave up"
		case !have:
			line.State = "starting"
		case running.connectedAt.IsZero():
			line.State = "started, not yet connected"
			line.PID = running.command.Process.Pid
		default:
			line.State = "connected"
			line.PID = running.command.Process.Pid
			line.Since = running.connectedAt.UTC().Format(time.RFC3339)
		}
		for _, one := range listening {
			if one.Name == name {
				line.Roles = one.Roles
			}
		}
		report.Integrations = append(report.Integrations, line)
	}

	// Anything connected that we did not start. They are not our children, a
	// disconnection is their own business, and they may come and go as they
	// like — but a person running doctor should still see them.
	for _, one := range listening {
		if seen[one.Name] {
			continue
		}
		seen[one.Name] = true
		report.Integrations = append(report.Integrations, Integration{
			Name: one.Name, State: "connected, not ours", PID: one.PID,
			Since: one.Since.UTC().Format(time.RFC3339), Roles: one.Roles,
		})
	}

	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return
	}
	_ = writeAtomically(w.opened.Layout.IntegrationsFile(), encoded)
}

// writeAtomically is the same tmp-and-rename the store uses, in miniature: a
// reader must never see half a report.
func writeAtomically(path string, content []byte) error {
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, content, paths.FileMode); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

// ReadReport is how doctor reads it, from another process entirely.
func ReadReport(layout paths.Layout) (Report, bool) {
	content, err := os.ReadFile(layout.IntegrationsFile())
	if err != nil {
		return Report{}, false
	}
	var report Report
	if err := json.Unmarshal(content, &report); err != nil {
		return Report{}, false
	}
	return report, true
}
