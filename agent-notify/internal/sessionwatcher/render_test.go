package sessionwatcher_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lassoColombo/agent-notify/internal/config"
	"github.com/lassoColombo/agent-notify/internal/paths"
	"github.com/lassoColombo/agent-notify/internal/sessionstore"
	"github.com/lassoColombo/agent-notify/internal/sessionwatcher"
	"github.com/lassoColombo/agent-notify/session"
)

// A display is tested against a REAL program, because everything about this
// happens between processes: a view on a stdin, an exit code, a process that is
// not started at all. The displays here are shell scripts, which is the most
// honest stand-in for somebody else's code.

func shortRoot(t *testing.T) string {
	t.Helper()
	// Short, so that the shell scripts written into it have short paths.
	root, err := os.MkdirTemp("/tmp", "an-sup")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	return root
}

// answersCapabilities writes a script that answers `capabilities` with exactly
// this JSON and nothing else.
func answersCapabilities(t *testing.T, directory, name, answer string) string {
	t.Helper()
	path := filepath.Join(directory, "agent-notify-"+name)
	script := fmt.Sprintf("#!/bin/sh\n"+
		"if [ \"$1\" = %q ]; then printf '%%s\\n' '%s'; exit 0; fi\nexit 1\n",
		session.CapabilitiesCommand, answer)
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

// aDisplay writes one that answers `capabilities` with `render` and this wake
// list, and appends every view it is handed to a file, one per line.
func aDisplay(t *testing.T, directory, name string, wakeOn ...string) (binary, painted string) {
	t.Helper()
	binary = filepath.Join(directory, "agent-notify-"+name)
	painted = filepath.Join(directory, name+".painted")

	waking, err := json.Marshal(wakeOn)
	if err != nil {
		t.Fatal(err)
	}
	answer := fmt.Sprintf(`{"version":"0.0.0-dev","methods":["render"],"wake_on":%s}`, waking)

	script := fmt.Sprintf("#!/bin/sh\ncase \"$1\" in\n  %s) printf '%%s\\n' '%s' ;;\n"+
		"  %s) cat >> %s ; printf '\\n' >> %s ;;\n  *) exit 1 ;;\nesac\n",
		session.CapabilitiesCommand, answer, session.MethodRender, painted, painted)
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return binary, painted
}

// everythingPainted is one view per render, in the order they were handed over.
func everythingPainted(t *testing.T, path string) []session.View {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var views []session.View
	for _, line := range strings.Split(strings.TrimSpace(string(content)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var view session.View
		if err := json.Unmarshal([]byte(line), &view); err != nil {
			t.Fatalf("a display was handed something that is not a view: %v\n%s", err, line)
		}
		views = append(views, view)
	}
	return views
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// atATestablePace shortens the loop these tests watch go round.
//
// The sweep is five seconds in production, which is right for a safety net and
// absurd for a test that has to see it fire three times. It is not
// configuration — nobody has an opinion about how often a safety net checks —
// so it is moved here, in the one place that needs it moved, and put back.
func atATestablePace(t *testing.T, sweep time.Duration) {
	t.Helper()
	was := sessionwatcher.SweepInterval
	sessionwatcher.SweepInterval = sweep
	t.Cleanup(func() { sessionwatcher.SweepInterval = was })
}

// running starts a session-watcher against root and stops it with the test.
func running(t *testing.T, root string) paths.Layout {
	t.Helper()
	t.Setenv(paths.TheVariableThatNamesTheRoot, root)
	layout, err := paths.FromEnvironment()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if err := layout.Create(); err != nil {
		t.Fatalf("Create: %v", err)
	}

	started, err := sessionwatcher.Start(layout, false)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = started.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		stop()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("the session-watcher did not stop")
		}
	})
	return layout
}

func waitFor(t *testing.T, what string, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("waited for %s and it never happened", what)
}

func reported(
	t *testing.T, layout paths.Layout, name string,
) (sessionwatcher.Integration, bool) {
	t.Helper()
	report, found := sessionwatcher.ReadReport(layout)
	if !found {
		return sessionwatcher.Integration{}, false
	}
	for _, one := range report.Integrations {
		if one.Name == name {
			return one, true
		}
	}
	return sessionwatcher.Integration{}, false
}

// TestADisplayIsDrawnOnceWithNothingHavingChanged is the cold path (§A7.6).
//
// A session-watcher that has just started, or a display that has just been
// installed, has a world to draw and no change to be told about — and drawing
// only on changes would leave it blank until the next agent said something.
func TestADisplayIsDrawnOnceWithNothingHavingChanged(t *testing.T) {
	atATestablePace(t, 200*time.Millisecond)
	root := shortRoot(t)
	binary, painted := aDisplay(t, root, "painter", "kernel")
	write(t, root+"/config.toml", fmt.Sprintf(
		"[integration.painter]\nbinary = %q\n", binary))

	running(t, root)
	waitFor(t, "the first render", func() bool {
		return len(everythingPainted(t, painted)) > 0
	})

	first := everythingPainted(t, painted)[0]
	if len(first.Changed) != 0 {
		// The world arriving is not the world moving. A notifier written to
		// the contract would otherwise announce every live agent at startup.
		t.Errorf("the first view carries %d change(s), want none", len(first.Changed))
	}
}

// TestADisplayIsHandedTheViewOnStdin: a process started fresh for one render
// remembers nothing, so everything it needs has to arrive with it.
func TestADisplayIsHandedTheViewOnStdin(t *testing.T) {
	atATestablePace(t, 200*time.Millisecond)
	root := shortRoot(t)
	binary, painted := aDisplay(t, root, "painter", "kernel")
	write(t, root+"/config.toml", fmt.Sprintf(
		"[integration.painter]\nbinary = %q\n", binary))

	layout := running(t, root)
	waitFor(t, "the first render", func() bool {
		return len(everythingPainted(t, painted)) > 0
	})

	aTurnFinished(t, layout, "one")
	waitFor(t, "a render carrying the session", func() bool {
		for _, view := range everythingPainted(t, painted) {
			if len(view.Sessions) > 0 {
				return true
			}
		}
		return false
	})

	var carried session.View
	for _, view := range everythingPainted(t, painted) {
		if len(view.Sessions) > 0 {
			carried = view
			break
		}
	}
	if got := carried.Sessions[0].Kernel; got != session.FinishedATurn {
		t.Errorf("the view carries kernel %q, want finished-a-turn", got)
	}
	if len(carried.Changed) != 1 {
		t.Errorf("the view carries %d change(s), want the one that moved", len(carried.Changed))
	}
}

// TestADisplayIsDrawnAgainWhenASessionLeaves is the change no comparison of
// records can see.
//
// A display is woken, compares every record it still holds against every record
// it is now given, and finds them all identical — because the one that moved is
// not in either list. It moved out. Deciding "nothing happened" there is how a
// bar keeps a row for an agent that finished ten minutes ago, and it is why
// Replace reports a departure separately from a change.
func TestADisplayIsDrawnAgainWhenASessionLeaves(t *testing.T) {
	atATestablePace(t, 200*time.Millisecond)
	root := shortRoot(t)
	// It watches the kernel, and the kernel of what leaves is not in the view
	// it is given: this display never asked for ended sessions.
	binary, painted := aDisplay(t, root, "painter", "kernel")
	write(t, root+"/config.toml", fmt.Sprintf(
		"[integration.painter]\nbinary = %q\n", binary))

	layout := running(t, root)
	aTurnFinished(t, layout, "one")
	waitFor(t, "a render carrying the session", func() bool {
		painted := everythingPainted(t, painted)
		return len(painted) > 0 && len(painted[len(painted)-1].Sessions) == 1
	})

	applyToTheStore(t, layout, session.Report{
		Key:    session.Key{Host: "mac", Agent: "fake", SessionID: "one"},
		Event:  session.SessionEnded,
		Detail: "exited",
	})

	waitFor(t, "a render with it gone", func() bool {
		painted := everythingPainted(t, painted)
		return len(painted) > 0 && len(painted[len(painted)-1].Sessions) == 0
	})
}

// TestADisplayIsNotRunForSomethingItDoesNotWatch is what `wake_on` buys now.
//
// It used to save a write to a socket somebody was already listening on. It
// saves a process, which is the difference between a display that costs
// something per keystroke and one that does not.
func TestADisplayIsNotRunForSomethingItDoesNotWatch(t *testing.T) {
	atATestablePace(t, 200*time.Millisecond)
	root := shortRoot(t)
	// It watches the kernel and nothing else. A new message is not a kernel.
	binary, painted := aDisplay(t, root, "painter", "kernel")
	write(t, root+"/config.toml", fmt.Sprintf(
		"[integration.painter]\nbinary = %q\n", binary))

	layout := running(t, root)
	aTurnFinished(t, layout, "one")
	waitFor(t, "the session to be drawn", func() bool {
		for _, view := range everythingPainted(t, painted) {
			if len(view.Sessions) > 0 {
				return true
			}
		}
		return false
	})
	sofar := len(everythingPainted(t, painted))

	// Same kernel, different message, several times over.
	for i := range 3 {
		somethingWasSaid(t, layout, "one", fmt.Sprintf("still talking %d", i))
	}
	time.Sleep(time.Second)

	if now := len(everythingPainted(t, painted)); now != sofar {
		t.Errorf("drawn %d times and then %d: a message is not on its wake list", sofar, now)
	}
}

// TestAContainerIsNeverDrawn: what core runs a program FOR is what the program
// said it answers, and a container never said `render`.
func TestAContainerIsNeverDrawn(t *testing.T) {
	atATestablePace(t, 100*time.Millisecond)
	root := shortRoot(t)
	binary := answersCapabilities(t, root, "placer",
		`{"version":"0.0.0-dev","methods":["interpret-environment","focus","focused"]}`)
	write(t, root+"/config.toml", fmt.Sprintf(
		"[integration.placer]\nbinary = %q\n\n[container]\norder = [\"placer\"]\n", binary))

	layout := running(t, root)
	waitFor(t, "the container to be reported", func() bool {
		_, found := reported(t, layout, "placer")
		return found
	})
	time.Sleep(400 * time.Millisecond)

	one, _ := reported(t, layout, "placer")
	if one.State != "run when core needs it" {
		t.Errorf("a container is reported as %q", one.State)
	}
}

// aTurnFinished puts one session in the store, the way an agent-integration
// would: a report, applied, and the session-watcher notices on its next sweep.
func aTurnFinished(t *testing.T, layout paths.Layout, id string) {
	t.Helper()
	applyToTheStore(t, layout, session.Report{
		Key:   session.Key{Host: "mac", Agent: "fake", SessionID: id},
		Event: session.TurnFinished,
		Name:  "session " + id,
	})
}

// somethingWasSaid changes a field no display in these tests watches, without
// moving the kernel: the agent is still finished, and still says so.
func somethingWasSaid(t *testing.T, layout paths.Layout, id, said string) {
	t.Helper()
	applyToTheStore(t, layout, session.Report{
		Key:     session.Key{Host: "mac", Agent: "fake", SessionID: id},
		Event:   session.TurnFinished,
		Message: &said,
	})
}

func applyToTheStore(t *testing.T, layout paths.Layout, report session.Report) {
	t.Helper()
	settings, _ := config.Load(layout)
	store, err := sessionstore.Open(layout, settings)
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	if _, err := store.Apply(report, time.Now().UTC()); err != nil {
		t.Fatalf("applying %s: %v", report.Event, err)
	}
}
