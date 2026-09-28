//go:build unix

package sessionwatcher

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"slices"
	"sync"
	"syscall"
	"time"

	"github.com/lassoColombo/agent-notify/internal/config"
	"github.com/lassoColombo/agent-notify/internal/containers"
	"github.com/lassoColombo/agent-notify/internal/core"
	"github.com/lassoColombo/agent-notify/internal/paths"
	"github.com/lassoColombo/agent-notify/internal/process"
	"github.com/lassoColombo/agent-notify/session"
)

// SweepInterval is the safety net behind the exit watches: the longest a
// session that died without saying so can sit in the store looking alive.
//
// It is a var and it is exported for one reason, and it is not configuration:
// these are the pace of a loop a test has to watch go round several times, and
// waiting the real interval would make that test a minute long. Nothing at
// runtime writes it, and no user can — this package is internal.
var SweepInterval = 5 * time.Second

// interpretTimeout bounds one `interpret-environment`, which runs here and may
// talk to the tool it is asking about (§A11.1). On expiry that container has no
// coordinates for that session, and the next capture or configuration reload
// tries again.
const interpretTimeout = 5 * time.Second

// Watcher is the long-lived process.
//
// What it owns today is the half M8 delivers: one exit watch per live session,
// the sweep behind it, and the reducer for transitions it *observes* rather than
// is told about — which means death and supersession, and never the passage of
// time (D-12). Fan-out to subscribers and supervising integrations arrive in M9
// and M13.
type Watcher struct {
	opened *core.Core
	held   *TheOnlyRunningWatcher
	exits  *Exits
	socket *net.UnixConn
	subs   *Subscribers
	boot   string
	prober process.ReadsProcessFacts
	logger *slog.Logger

	// known is every live session as this process last saw it. It exists so
	// that a snapshot can be served from memory rather than from the disk
	// (§A7.6), and so that fan-out can skip a change nobody can see (§A9.3).
	mu    sync.Mutex
	known map[string]session.Record
	// caused remembers what a poke said caused a change, so that the delta can
	// say so too. Losing it costs the `event` field on one delta and nothing
	// else (§A13.1).
	caused map[string]session.Event
	// refused counts how often a container failed to interpret one session, so
	// that a broken one stops being run every sweep. It is a count and not a
	// flag because the first failure is not evidence: a session-watcher can
	// easily ask zellij about a pane a moment before zellij is ready. Cleared
	// on reload, which is how a person retries after fixing something.
	refused map[string]int
	// complained remembers a configuration mistake already reported, because
	// it will be the same mistake every sweep until somebody edits the file.
	complained map[string]bool

	// answers is what each integration said when it was asked what it answers,
	// as of this configuration. Filled at startup and on reload and read
	// everywhere else, because asking is a process per integration and the
	// answer cannot change without the program on disk changing.
	answers map[string]WhatAnIntegrationAnswers

	// renderers is one per display core runs, each with the goroutine that
	// runs it and its own picture of what it was last handed.
	//
	// There used to be a supervisor here instead — children, consecutive
	// failures, a grace period, a count of attempts before giving up — and it
	// is gone with the last long-lived integration. A display core runs is a
	// program it runs when something moves; a display that must own a process
	// is started by launchd and connects on its own (§A10.2).
	renderers map[string]*renderer
}

// Start takes the lock, binds the socket, and opens the exit queue.
//
// Losing the race for the lock is not a failure. Anyone may start a
// session-watcher — a hook whose poke found nobody, a subscriber that lost its
// connection — and the lock is what makes that race harmless, so the loser
// simply has nothing to do (§A9.2).
func Start(layout paths.Layout, toTerminal bool) (*Watcher, error) {
	held, err := TakeIfNobodyElseHasIt(layout)
	if err != nil {
		return nil, err
	}

	opened, err := core.OpenAt(layout, "session-watcher")
	if err != nil {
		held.Release()
		return nil, err
	}
	logger := opened.Logger
	if toTerminal {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}

	socket, err := Listen(layout, held)
	if err != nil {
		opened.Close()
		held.Release()
		return nil, err
	}

	exits, err := WatchExits()
	if err != nil {
		socket.Close()
		opened.Close()
		held.Release()
		return nil, err
	}

	boot, err := process.BootIdentity()
	if err != nil {
		// Not fatal: without it the reboot rule cannot apply and every session
		// is judged by probing instead, which is slower and still correct.
		logger.Warn("cannot read this boot's identity", "problem", err.Error())
	}

	watching := &Watcher{
		opened: opened, held: held, exits: exits, socket: socket,
		boot: boot, prober: process.ProcessesOnThisMachine{}, logger: logger,
		known: map[string]session.Record{}, caused: map[string]session.Event{},
	}

	subs, err := Serve(layout, held, logger, opened.Settings.SubscriberQueue, watching.snapshotFor)
	if err != nil {
		exits.Close()
		socket.Close()
		opened.Close()
		held.Release()
		return nil, err
	}
	watching.subs = subs
	return watching, nil
}

// snapshotFor is what a subscriber gets on connect, on asking, and after
// falling behind.
//
// It is served from memory, which is the whole reason there is no snapshot file
// to keep correct (§A7.6, Q3). Ended sessions are included only for a
// subscriber that asked for them: a bar says no, a picker says yes, and the
// transition to ended reaches both regardless (D-26).
func (w *Watcher) snapshotFor(wantEnded bool) []session.Record {
	w.mu.Lock()
	sessions := make([]session.Record, 0, len(w.known))
	for _, record := range w.known {
		sessions = append(sessions, record)
	}
	w.mu.Unlock()

	if wantEnded {
		ended, err := w.opened.Store.ListEnded()
		if err != nil {
			w.logger.Warn("reading ended sessions for a snapshot", "problem", err.Error())
		}
		sessions = append(sessions, ended...)
	}
	session.ByUrgency(sessions)
	return sessions
}

// Run is the loop. It returns when the context is cancelled or a signal says so.
func (w *Watcher) Run(ctx context.Context) error {
	defer w.close()

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	reload := make(chan os.Signal, 1)
	signal.Notify(reload, syscall.SIGHUP)
	defer signal.Stop(reload)

	w.logger.Info("session-watcher started",
		"pid", os.Getpid(), "version", session.Version,
		"state", w.opened.Layout.State, "runtime", w.opened.Layout.Runtime)

	// Before anything is run, what is there to run. Everything core does with
	// an integration from here on is decided by what it said here.
	w.askWhatEachIntegrationAnswers()
	w.startRenderers(ctx)

	// Reconciling from the store at startup is what makes every poke losable:
	// a session-watcher that has just started knows everything a session-watcher
	// that has been running all day knows (§A9.1, R4).
	w.reconcile("startup")

	woken := make(chan string, 64)
	go w.listenForPokes(ctx, woken)
	go w.listenForExits(ctx, woken)
	go w.subs.Accept()

	tick := time.NewTicker(SweepInterval)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("session-watcher stopping")
			return nil
		case <-reload:
			w.reloadConfiguration(ctx)
		case why := <-woken:
			w.reconcile(why)
		case <-tick.C:
			w.writeReport(w.opened.Settings)
			if w.worldIsGone() {
				w.logger.Info("the state directory is gone; nothing left to watch",
					"state", w.opened.Layout.State)
				return nil
			}
			w.reconcile("sweep")
			w.prune()
		}
	}
}

// listenForPokes turns datagrams into wake-ups. What the poke says is not used:
// the store is the truth, and re-reading it is both simpler and correct after
// any number of lost messages (R4).
func (w *Watcher) listenForPokes(ctx context.Context, woken chan<- string) {
	for ctx.Err() == nil {
		if poke, arrived := Receive(w.socket, time.Second); arrived {
			if poke.Event != "" {
				w.mu.Lock()
				w.caused[poke.Key] = poke.Event
				w.mu.Unlock()
			}
			select {
			case woken <- "poke":
			default:
				// The queue is full, so a reconcile is already coming. One
				// reconcile answers every poke that is waiting.
			}
		}
	}
}

// listenForExits turns a process going away into a wake-up, which is what makes
// `kill -9` visible in milliseconds rather than at the next tick.
func (w *Watcher) listenForExits(ctx context.Context, woken chan<- string) {
	for ctx.Err() == nil {
		gone, err := w.exits.WhichPidsExited(time.Second)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			w.logger.Warn("the exit queue", "problem", err.Error())
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if len(gone) == 0 {
			continue
		}
		w.logger.Debug("a watched process exited", "pids", gone)
		select {
		case woken <- "exit":
		default:
		}
	}
}

// reconcile is the whole of the watcher's work: read the store, watch what is
// alive, end what is provably gone.
//
// It is the same function whatever woke it, because the store is the truth and
// there is nothing else to consult. A poke, a process exit and a tick therefore
// differ only in latency.
func (w *Watcher) reconcile(why string) {
	live, err := w.opened.Store.List()
	if err != nil {
		w.logger.Warn("reading the store", "why", why, "problem", err.Error())
	}

	live = w.derive(live)

	seen := make(map[string]bool, len(live))
	for _, record := range live {
		if record.Process.PID > 0 {
			// A pid that has already gone fails to register, and the sweep
			// below judges it anyway. Registration is an optimisation on top
			// of a correct mechanism, never the mechanism (R5).
			_ = w.exits.Watch(record.Process.PID)
		}
		seen[record.Key.String()] = true
		w.publish(record)
	}
	w.publishDepartures(seen)

	ending := process.Ended(live, w.boot, process.Self(), w.prober)
	for _, end := range ending {
		w.end(end)
	}

	// Once, at the end, rather than once per record: a render paints the whole
	// world, so a sweep that touched nine sessions is still one render. Each
	// display works out for itself whether anything it watches moved, and most
	// of the time nothing did and no process is started at all.
	w.drawEverythingAgain()
}

// derive turns what the hook captured into coordinates, by running each
// configured container's `interpret-environment` (§A11.1, D-38).
//
// It happens HERE and not in the hook for the reason D-27 gives: this is the
// half that talks to the tool, and asking zellij which tab holds pane 7 is
// exactly the kind of question that hangs when the tool is wedged. Here it costs
// a sweep; there it would cost the agent.
//
// A container is asked once per session per capture. Replacing a captured
// context voids everything derived from it in the same write (§A7.4.1), so the
// question "does this record have coordinates from this container" is the whole
// of the scheduling: no timers, no queue, and a container enabled today places a
// session that started on Tuesday.
func (w *Watcher) derive(live []session.Record) []session.Record {
	configured, problems := containers.Configured(w.opened.Settings, w.methodsByIntegration())
	for _, problem := range problems {
		w.complainOnce(problem.Error())
	}
	if len(configured) == 0 {
		return live
	}

	updated := make([]session.Record, 0, len(live))
	for _, record := range live {
		for _, one := range configured {
			captured, present := record.CapturedContext.By[one.Name]
			if !present {
				// This container never saw this session — it started outside
				// zellij, or before the container was installed. Not a
				// failure, and not something to retry (§A11.7).
				continue
			}
			if _, already := record.DerivedContext[one.Name]; already {
				continue
			}
			if w.gaveUpOn(record.Key.String(), one.Name) {
				continue
			}
			if !slices.Contains(one.Methods, session.MethodInterpret) {
				// It places sessions but cannot turn a capture into
				// coordinates. Asking anyway would cost a process to be told
				// so, once per session per sweep.
				continue
			}

			coordinates, err := containers.Interpret(one, captured, interpretTimeout)
			if err != nil {
				// One container failing says nothing about the others and
				// nothing about the session (R13). Remembered so that a broken
				// container is not run again every sweep; forgotten on reload,
				// which is how a person retries after fixing it.
				failures := w.giveUpOn(record.Key.String(), one.Name)
				level := w.logger.Warn
				if failures >= attemptsBeforeGivingUp {
					level = w.logger.Error
				}
				level("interpret-environment", "container", one.Name,
					"session", record.Key.String(), "attempt", failures,
					"giving up after", attemptsBeforeGivingUp, "problem", err.Error())
				continue
			}

			written, err := w.opened.Store.Update(record.Key, time.Now().UTC(),
				func(previous session.Record) session.Record {
					next := previous.Clone()
					// Against the record as it is NOW, not as it was read: a
					// capture that was replaced while this ran must not have
					// coordinates from the old one written over it.
					if !bytes.Equal(next.CapturedContext.By[one.Name], captured) {
						return previous
					}
					if next.DerivedContext == nil {
						next.DerivedContext = map[string]json.RawMessage{}
					}
					next.DerivedContext[one.Name] = coordinates
					return next
				})
			if err != nil {
				w.logger.Warn("recording coordinates",
					"container", one.Name, "session", record.Key.String(), "problem", err.Error())
				continue
			}
			w.logger.Info("placed", "container", one.Name, "session", record.Key.String())
			record = written
		}
		updated = append(updated, record)
	}
	return updated
}

// gaveUpOn and giveUpOn remember a container that could not interpret one
// session, so that a broken one is not run every sweep for ever.
// attemptsBeforeGivingUp is deliberately small and deliberately not one. One
// failure is usually a race — the session-watcher asking zellij about a pane a
// moment before zellij is listening — and three sweeps is long enough for that
// to settle and short enough that a genuinely broken container is not run for
// ever.
const attemptsBeforeGivingUp = 3

func (w *Watcher) gaveUpOn(session, container string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.refused[session+"\x00"+container] >= attemptsBeforeGivingUp
}

func (w *Watcher) giveUpOn(session, container string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.refused == nil {
		w.refused = map[string]int{}
	}
	key := session + "\x00" + container
	w.refused[key]++
	return w.refused[key]
}

// complainOnce keeps a configuration mistake from filling the log: it is the
// same mistake every sweep until somebody edits the file.
func (w *Watcher) complainOnce(message string) {
	w.mu.Lock()
	if w.complained == nil {
		w.complained = map[string]bool{}
	}
	said := w.complained[message]
	w.complained[message] = true
	w.mu.Unlock()
	if !said {
		w.logger.Warn("configuration", "problem", message)
	}
}

// publish offers one record to the subscribers, if anything about it moved.
func (w *Watcher) publish(record session.Record) {
	key := record.Key.String()

	w.mu.Lock()
	previous, had := w.known[key]
	w.known[key] = record
	event := w.caused[key]
	delete(w.caused, key)
	w.mu.Unlock()

	// The generous question, on purpose: whether anybody could want this. The
	// narrow one — whether *this* subscriber asked for the fields that moved —
	// is Publish's, per subscriber, and asking it here as well would answer it
	// with a default that no subscriber chose (§A7.4.3).
	if had && !session.WorthOfferingAround(previous, record) {
		return
	}
	w.subs.Publish(previous, record, event)
}

// publishDepartures handles a session that has left sessions/ since the last
// look: it either ended and was filed, or it was pruned and is gone for good.
func (w *Watcher) publishDepartures(seen map[string]bool) {
	w.mu.Lock()
	var left []session.Record
	for key, record := range w.known {
		if !seen[key] {
			left = append(left, record)
			delete(w.known, key)
		}
	}
	w.mu.Unlock()

	for _, previous := range left {
		record, found, err := w.opened.Store.Read(previous.Key)
		if err != nil {
			w.logger.Warn("reading a departed session", "session", previous.Key.String(),
				"problem", err.Error())
			continue
		}
		if !found {
			// Pruned. Nobody's map should keep it forever.
			w.subs.Forget(previous.Key)
			continue
		}
		w.subs.Publish(previous, record, session.SessionEnded)
	}
}

// end files a session that stopped without saying so.
//
// It goes through the same reducer as everything else. A transition discovered
// by observation and one reported by an agent are the same transition, and
// having two ways to make one would be having two state machines (D-8).
func (w *Watcher) end(ending process.Ending) {
	written, err := w.opened.Store.Apply(session.Report{
		Key:    ending.Key,
		Event:  session.SessionEnded,
		Detail: ending.Detail,
	}, time.Now().UTC())
	if err != nil {
		w.logger.Warn("cannot end a session that has gone",
			"session", ending.Key.String(), "problem", err.Error())
		return
	}
	w.logger.Info("ended a session nobody announced",
		"session", ending.Key.String(), "why", ending.Detail, "sequence", written.Sequence)

	w.mu.Lock()
	previous := w.known[ending.Key.String()]
	delete(w.known, ending.Key.String())
	w.mu.Unlock()
	w.subs.Publish(previous, written, session.SessionEnded)
}

// prune forgets what has outlived keep-ended-sessions. It runs on the tick
// only: it is the one thing here that is genuinely periodic.
func (w *Watcher) prune() {
	removed, err := w.opened.Store.ForgetWhatIsTooOld(time.Now().UTC())
	if err != nil {
		w.logger.Warn("pruning", "problem", err.Error())
	}
	for _, key := range removed {
		w.logger.Info("forgot an ended session past its welcome", "session", key.String())
		w.subs.Forget(key)
	}
	if len(removed) > 0 {
		// A display that asked for ended sessions has just lost some, and this
		// is the one change that does not come through the store as a record
		// moving: it comes through as a record ceasing to exist.
		w.drawEverythingAgain()
	}
}

// reloadConfiguration re-reads the file on SIGHUP.
//
// Values that cannot change under a running process say so rather than
// pretending to apply: the paths were fixed when this process started, and a
// socket cannot move without every client being told (§A14).
func (w *Watcher) reloadConfiguration(ctx context.Context) {
	settings, problems := config.Load(w.opened.Layout.ConfigFile)
	for _, problem := range problems {
		w.logger.Warn("configuration", "problem", problem.Error())
	}
	w.opened.Settings = settings

	// Asked again, on the new file and on whatever binary is at each path now.
	// This is the gesture a person makes after rebuilding an integration, so it
	// is the one place a changed answer can be noticed.
	w.askWhatEachIntegrationAnswers()
	w.startRenderers(ctx)

	// Reload is also how a person retries: a container that was broken, or
	// missing, or misspelled, gets another go on the next sweep rather than
	// being written off for the life of this process.
	w.mu.Lock()
	w.refused, w.complained = nil, nil
	w.mu.Unlock()

	w.writeReport(w.opened.Settings)
	w.logger.Info("configuration re-read",
		"note", "paths and sockets are fixed for the life of this process; "+
			"every integration was asked again what it answers, on whatever "+
			"binary is at its path now, and anything that had failed will be tried again")
}

func (w *Watcher) close() {
	if w.subs != nil {
		_ = w.subs.Close()
	}
	if w.exits != nil {
		_ = w.exits.Close()
	}
	if w.socket != nil {
		_ = w.socket.Close()
	}
	if w.opened != nil {
		w.opened.Close()
	}
	if w.held != nil {
		_ = w.held.Release()
	}
}

// StartIfNobodyIs starts a detached session-watcher unless one is already
// running, and is what a hook calls when its poke finds nobody listening.
//
// A race here is harmless by construction: several hooks may decide at once,
// several may spawn, and the lock means exactly one survives to do any work.
func StartIfNobodyIs(layout paths.Layout, configured string) error {
	if Running(layout) {
		return nil
	}
	return Spawn(layout, configured)
}

// Stop asks a running session-watcher to shut down cleanly.
//
// Nothing here ever auto-kills: this is SIGTERM to a process whose pid the lock
// file names, sent because a person asked (§A9.2).
func Stop(layout paths.Layout, within time.Duration) error {
	if !Running(layout) {
		return fmt.Errorf("no session-watcher is running")
	}
	held, err := WhoHolds(layout)
	if err != nil {
		return fmt.Errorf("something holds the lock but did not say who: %w", err)
	}
	if err := syscall.Kill(held.PID, syscall.SIGTERM); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return fmt.Errorf("cannot signal pid %d: %w", held.PID, err)
	}

	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if !Running(layout) {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("pid %d still holds the lock %s after being asked to stop", held.PID, within)
}

// worldIsGone reports that the state directory has been removed under us.
//
// It is the one condition that ends this process without being asked. A
// session-watcher whose store has been deleted has nothing to watch, nothing to
// write, and no way to be useful; staying would mean holding a lock on a
// directory that no longer exists. Deleting the state directory is also exactly
// how a person resets this system, and how a test throws its temporary one
// away.
func (w *Watcher) worldIsGone() bool {
	_, err := os.Stat(w.opened.Layout.Sessions())
	return os.IsNotExist(err)
}
