package sessionwatcher_test

import (
	"fmt"
	"os/exec"
	"testing"
	"time"

	"github.com/lassoColombo/agent-notify/internal/process"
	"github.com/lassoColombo/agent-notify/session"
)

// TestOneWakeHandsEveryDisplayTheSameWorld is what reading once buys (D-92).
//
// Two displays, one wanting ended sessions and one not, are handed the world
// the session-watcher read and judged once per wake. An agent that is killed
// is `ended` on the first, with the reason, and gone from the second — from
// the same read, before the store has caught up.
func TestOneWakeHandsEveryDisplayTheSameWorld(t *testing.T) {
	atATestablePace(t, 200*time.Millisecond)
	root := shortRoot(t)
	keeper, kept := aDisplayAnswering(t, root, "keeper", session.Capabilities{
		Version: "0.0.0-dev", Methods: []string{session.MethodRender},
		WakeOn: []string{"kernel"}, WantEnded: true,
	})
	bar, barred := aDisplay(t, root, "bar", "kernel")
	write(t, root+"/config.toml", fmt.Sprintf(
		"[integration.keeper]\nbinary = %q\n\n[integration.bar]\nbinary = %q\n", keeper, bar))

	layout := running(t, root)

	victim := exec.Command("/bin/sleep", "30")
	if err := victim.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = victim.Process.Kill(); _ = victim.Wait() })
	facts, err := process.ProcessesOnThisMachine{}.FactsAbout(victim.Process.Pid)
	if err != nil {
		t.Fatalf("cannot see the process just started: %v", err)
	}
	applyToTheStore(t, layout, session.Report{
		Key:     session.Key{Host: "mac", Agent: "fake", SessionID: "doomed"},
		Event:   session.TurnFinished,
		Process: session.Process{PID: facts.PID, StartedAt: facts.StartedAt},
	})
	for _, painted := range []string{kept, barred} {
		waitFor(t, "the session to be drawn on "+painted, func() bool {
			views := everythingPainted(t, painted)
			return len(views) > 0 && len(views[len(views)-1].Sessions) == 1
		})
	}

	if err := victim.Process.Kill(); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	_ = victim.Wait()

	waitFor(t, "the keeper to be told it ended", func() bool {
		views := everythingPainted(t, kept)
		last := views[len(views)-1]
		return len(last.Sessions) == 1 && last.Sessions[0].Kernel == session.Ended
	})
	views := everythingPainted(t, kept)
	last := views[len(views)-1]
	if last.Sessions[0].Detail != process.ProcessGone {
		t.Errorf("the keeper was told %q, want %s", last.Sessions[0].Detail, process.ProcessGone)
	}
	if len(last.Changed) != 1 || last.Changed[0].PreviousKernel != session.FinishedATurn {
		t.Errorf("the keeper's change is %+v, want one from finished-a-turn", last.Changed)
	}

	waitFor(t, "the bar to see it leave", func() bool {
		views := everythingPainted(t, barred)
		return len(views[len(views)-1].Sessions) == 0
	})
	for _, view := range everythingPainted(t, barred) {
		for _, record := range view.Sessions {
			if record.Kernel == session.Ended {
				t.Errorf("the bar did not ask for ended sessions and was handed one: %+v", record)
			}
		}
	}
}
