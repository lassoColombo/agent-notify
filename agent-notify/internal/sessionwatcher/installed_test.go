package sessionwatcher_test

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lassoColombo/agent-notify/internal/config"
	"github.com/lassoColombo/agent-notify/internal/handshake"
	"github.com/lassoColombo/agent-notify/internal/paths"
	"github.com/lassoColombo/agent-notify/internal/sessionstore"
	"github.com/lassoColombo/agent-notify/session"
)

// TestAnIntegrationInstalledWhileTheWatcherRunsIsAskedOnTheSweep is D-102's
// "on PATH is installed" holding for a watcher that is already running: the
// sweep sees a new program on PATH and asks it, with nobody sending a signal
// (D-105).
func TestAnIntegrationInstalledWhileTheWatcherRunsIsAskedOnTheSweep(t *testing.T) {
	atATestablePace(t, 100*time.Millisecond)
	root := shortRoot(t)
	layout := running(t, root)
	waitFor(t, "the first report", func() bool {
		_, found := handshake.Read(layout)
		return found
	})

	answersCapabilities(t, root, "placer",
		`{"version":"0.0.0-dev","methods":["interpret-environment","focus","focused"]}`)

	waitFor(t, "the new program to be reported without a reload", func() bool {
		one, found := reported(t, layout, "placer")
		return found && len(one.Answers.Methods) == 3
	})
}

// TestARendererIsHandedWhatEndedRecentlyAndNotWhatIsRetained is the bound on
// `want_ended` (D-106): a display that asked for ended sessions needs the one
// that ended a moment ago, to give its pane back, and not a week of them on
// every wake.
func TestARendererIsHandedWhatEndedRecentlyAndNotWhatIsRetained(t *testing.T) {
	atATestablePace(t, 200*time.Millisecond)
	root := shortRoot(t)
	_, painted := aDisplayAnswering(t, root, "painter", session.Capabilities{
		Version: "0.0.0-dev", Methods: []string{session.MethodRender},
		WakeOn: []string{"kernel"}, WantEnded: true,
	})

	t.Setenv(paths.TheVariableThatNamesTheRoot, root)
	layout, err := paths.FromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if err := layout.Create(); err != nil {
		t.Fatal(err)
	}
	settings, _ := config.Load(layout)
	store, err := sessionstore.Open(layout, settings)
	if err != nil {
		t.Fatal(err)
	}
	for _, one := range []struct {
		id    string
		ended time.Time
	}{
		{"recent", time.Now().UTC()},
		{"retained", time.Now().UTC().Add(-2 * time.Hour)},
	} {
		key := session.Key{Host: "mac", Agent: "fake", SessionID: one.id}
		if _, err := store.Apply(session.Report{Key: key, Event: session.TurnFinished}, one.ended); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Apply(session.Report{Key: key, Event: session.SessionEnded, Detail: "exited"}, one.ended); err != nil {
			t.Fatal(err)
		}
	}

	running(t, root)
	waitFor(t, "the first render", func() bool {
		return len(everythingPainted(t, painted)) > 0
	})

	var handed []string
	for _, record := range everythingPainted(t, painted)[0].Sessions {
		handed = append(handed, record.Key.SessionID)
	}
	if !slices.Equal(handed, []string{"recent"}) {
		t.Errorf("the renderer was handed %v, want the one that ended recently and not the retained one", handed)
	}
}

// TestAReplacedCaptureIsInterpretedAgainAfterGivingUp is D-109: giving up
// is per capture. A container that failed three times against one pane is
// asked again when the session comes back in a new process with a new
// capture, because nothing about the old failures is about the new pane.
func TestAReplacedCaptureIsInterpretedAgainAfterGivingUp(t *testing.T) {
	atATestablePace(t, 100*time.Millisecond)
	root := shortRoot(t)
	placer := root + "/agent-notify-placer"
	attempts := root + "/placer.attempts"
	write(t, placer, "#!/bin/sh\ncase \"$1\" in\n"+
		"  capabilities) printf '%s\\n' '{\"version\":\"0.0.0-dev\",\"methods\":[\"interpret-environment\",\"focus\",\"focused\"]}' ;;\n"+
		"  interpret-environment) echo x >> "+attempts+"; exit 1 ;;\n  *) exit 1 ;;\nesac\n")
	if err := os.Chmod(placer, 0o700); err != nil {
		t.Fatal(err)
	}
	attemptsSoFar := func() int {
		content, _ := os.ReadFile(attempts)
		return strings.Count(string(content), "x")
	}

	layout := running(t, root)
	key := session.Key{Host: "mac", Agent: "fake", SessionID: "back"}
	applyToTheStore(t, layout, session.Report{
		Key: key, Event: session.TurnFinished, Process: session.Process{PID: os.Getpid()},
		CapturedContext: &session.CapturedContext{
			By: map[string]json.RawMessage{"placer": json.RawMessage(`{"pane":"1"}`)},
		},
	})
	waitFor(t, "the placer to be given up on", func() bool { return attemptsSoFar() >= 3 })
	time.Sleep(500 * time.Millisecond)
	if got := attemptsSoFar(); got != 3 {
		t.Fatalf("the placer was asked %d times, want 3 and then nothing", got)
	}

	// The session comes back in another process: a new capture, and
	// everything derived from the old one void (D-90).
	applyToTheStore(t, layout, session.Report{
		Key: key, Event: session.TurnFinished, Process: session.Process{PID: os.Getppid()},
		CapturedContext: &session.CapturedContext{
			By: map[string]json.RawMessage{"placer": json.RawMessage(`{"pane":"2"}`)},
		},
	})
	waitFor(t, "the placer to be asked about the new capture", func() bool { return attemptsSoFar() >= 4 })
}

// TestARebuiltDisplayIsWokenForWhatItNowAsks is D-111: a display rebuilt
// with another `wake_on` is re-asked on the sweep (D-105), and what it now
// answers is what gates its renders, without the watcher restarting.
func TestARebuiltDisplayIsWokenForWhatItNowAsks(t *testing.T) {
	atATestablePace(t, 100*time.Millisecond)
	root := shortRoot(t)
	_, painted := aDisplay(t, root, "painter", "kernel")

	layout := running(t, root)
	waitFor(t, "the first render", func() bool {
		return len(everythingPainted(t, painted)) > 0
	})

	key := session.Key{Host: "mac", Agent: "fake", SessionID: "one"}
	said := "first"
	applyToTheStore(t, layout, session.Report{Key: key, Event: session.UserSentPrompt, Message: &said})
	waitFor(t, "the session to arrive", func() bool {
		views := everythingPainted(t, painted)
		return len(views[len(views)-1].Sessions) == 1
	})

	// Rebuilt: the same program, answering that it wakes on the message now.
	// The reconcile that follows the reload draws it afresh: a first view,
	// with nothing having changed.
	drawn := len(everythingPainted(t, painted))
	aDisplay(t, root, "painter", "message")
	waitFor(t, "the rebuilt display to be re-asked", func() bool {
		one, found := reported(t, layout, "painter")
		return found && slices.Equal(one.Answers.WakeOn, []string{"message"})
	})
	waitFor(t, "the fresh renderer's first view", func() bool {
		views := everythingPainted(t, painted)
		return len(views) > drawn && len(views[len(views)-1].Changed) == 0
	})

	said = "second"
	applyToTheStore(t, layout, session.Report{Key: key, Event: session.AgentProgressed, Message: &said})
	waitFor(t, "a render for the message alone", func() bool {
		for _, view := range everythingPainted(t, painted) {
			for _, change := range view.Changed {
				if change.Record.Message == "second" {
					return true
				}
			}
		}
		return false
	})
}

// TestAReloadMovesRetention is D-117: `keep-ended-sessions` edited and picked
// up by the sweep takes effect without a restart, and at the reload rather
// than at the hour.
func TestAReloadMovesRetention(t *testing.T) {
	atATestablePace(t, 100*time.Millisecond)
	root := shortRoot(t)

	t.Setenv(paths.TheVariableThatNamesTheRoot, root)
	layout, err := paths.FromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if err := layout.Create(); err != nil {
		t.Fatal(err)
	}
	settings, _ := config.Load(layout)
	store, err := sessionstore.Open(layout, settings)
	if err != nil {
		t.Fatal(err)
	}
	key := session.Key{Host: "mac", Agent: "fake", SessionID: "old"}
	twoHoursAgo := time.Now().UTC().Add(-2 * time.Hour)
	for _, event := range []session.Event{session.TurnFinished, session.SessionEnded} {
		if _, err := store.Apply(session.Report{Key: key, Event: event}, twoHoursAgo); err != nil {
			t.Fatal(err)
		}
	}

	running(t, root)
	waitFor(t, "the first report", func() bool {
		_, found := handshake.Read(layout)
		return found
	})
	if _, err := os.Stat(layout.EndedFile(key.String())); err != nil {
		t.Fatalf("forgotten under the default week of retention: %v", err)
	}

	write(t, layout.ConfigFile, "keep-ended-sessions: 1h\n")
	waitFor(t, "the session to be forgotten on the reload", func() bool {
		_, err := os.Stat(layout.EndedFile(key.String()))
		return os.IsNotExist(err)
	})
}

// TestTwoRendersOfOneDisplayAreNeverInFlightTogether is D-111's other half: a
// display rebuilt while its render is in flight is drawn afresh by the same
// goroutine, after that render, and never beside it.
func TestTwoRendersOfOneDisplayAreNeverInFlightTogether(t *testing.T) {
	atATestablePace(t, 100*time.Millisecond)
	root := shortRoot(t)
	painter, log := root+"/agent-notify-painter", root+"/painter.log"
	slowPainter := func(wakeOn string) {
		t.Helper()
		script := "#!/bin/sh\ncase \"$1\" in\n" +
			"  capabilities) printf '%s\\n' '{\"version\":\"0.0.0-dev\",\"methods\":[\"render\"],\"wake_on\":[\"" + wakeOn + "\"]}' ;;\n" +
			"  render) echo start >> " + log + "; cat > /dev/null; sleep 1; echo end >> " + log + " ;;\n" +
			"  *) exit 1 ;;\nesac\n"
		// Renamed into place rather than rewritten: the first render is
		// still running the old file when the second is written, and the
		// temporary is named so that the PATH scan does not take it for an
		// integration.
		if err := os.WriteFile(root+"/painter.next", []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(root+"/painter.next", painter); err != nil {
			t.Fatal(err)
		}
	}
	logged := func() []string {
		content, _ := os.ReadFile(log)
		return strings.Fields(string(content))
	}

	slowPainter("kernel")
	running(t, root)
	waitFor(t, "the first render to start", func() bool { return len(logged()) == 1 })

	// Rebuilt mid-render with another answer: the sweep re-asks, and the
	// reconcile after the reload wakes it again.
	slowPainter("message")
	waitFor(t, "the second render to end", func() bool { return len(logged()) == 4 })

	if got := logged(); !slices.Equal(got, []string{"start", "end", "start", "end"}) {
		t.Errorf("the painter saw %v, want one render finished before the next started", got)
	}
}
