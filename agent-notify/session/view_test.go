package session_test

import (
	"testing"

	"github.com/lassoColombo/agent-notify/session"
)

// TestAViewIsHandedOverForTheFirstWorldAChangeOrADeparture is the one render
// decision, made in one place for a display core runs and one that owns its
// process (R24, D-94). The three cases are three different things: the first
// world is drawn with no changes (R22); a change is what the display asked to
// be woken for and nothing else; a departure is a change to no record at all.
func TestAViewIsHandedOverForTheFirstWorldAChangeOrADeparture(t *testing.T) {
	one := session.Record{Key: session.Key{Host: "mac", Agent: "claude", SessionID: "one"},
		Kernel: session.Working, Rank: session.Working.Rank()}
	two := session.Record{Key: session.Key{Host: "mac", Agent: "claude", SessionID: "two"},
		Kernel: session.Working, Rank: session.Working.Rank()}
	wakeOn := []string{"kernel"}

	var shown session.LastShown

	view, worth := shown.Replace([]session.Record{one, two}, wakeOn)
	if !worth || len(view.Sessions) != 2 || len(view.Changed) != 0 {
		t.Fatalf("the first world: worth=%v, %d sessions, %d changes; want handed over with no changes",
			worth, len(view.Sessions), len(view.Changed))
	}

	if view, worth := shown.Replace([]session.Record{one, two}, wakeOn); worth {
		t.Errorf("the same world again was handed over: %+v", view)
	}

	talking := one
	talking.Message = "still here"
	if view, worth := shown.Replace([]session.Record{talking, two}, wakeOn); worth {
		t.Errorf("a message is not on the wake list and was handed over: %+v", view)
	}

	finished := talking
	finished.Kernel, finished.Rank = session.FinishedATurn, session.FinishedATurn.Rank()
	view, worth = shown.Replace([]session.Record{finished, two}, wakeOn)
	if !worth || len(view.Changed) != 1 {
		t.Fatalf("a kernel moved: worth=%v with %d changes, want one", worth, len(view.Changed))
	}
	if moved := view.Changed[0]; moved.Record.Key != one.Key || moved.PreviousKernel != session.Working {
		t.Errorf("the change is %+v, want one from working", moved)
	}

	view, worth = shown.Replace([]session.Record{two}, wakeOn)
	if !worth || len(view.Sessions) != 1 || len(view.Changed) != 0 {
		t.Fatalf("a departure: worth=%v, %d sessions, %d changes; want handed over, one left, nothing changed",
			worth, len(view.Sessions), len(view.Changed))
	}
}

// TestACloneIsWhatWasShownBefore: put back after a display could not draw a
// view, it hands the same change over again; and replacing the world in one
// leaves the other as it was.
func TestACloneIsWhatWasShownBefore(t *testing.T) {
	one := session.Record{Key: session.Key{Host: "mac", Agent: "claude", SessionID: "one"},
		Kernel: session.Working, Rank: session.Working.Rank()}
	finished := one
	finished.Kernel, finished.Rank = session.FinishedATurn, session.FinishedATurn.Rank()
	wakeOn := []string{"kernel"}

	var shown session.LastShown
	shown.Replace([]session.Record{one}, wakeOn)
	before := shown.Clone()
	if _, worth := shown.Replace([]session.Record{finished}, wakeOn); !worth {
		t.Fatal("a kernel moved and nothing was handed over")
	}

	shown = before
	view, worth := shown.Replace([]session.Record{finished}, wakeOn)
	if !worth || len(view.Changed) != 1 || view.Changed[0].PreviousKernel != session.Working {
		t.Errorf("put back, the same change: worth=%v, %+v", worth, view.Changed)
	}

	var never session.LastShown
	cloned := never.Clone()
	if _, worth := cloned.Replace([]session.Record{one}, wakeOn); !worth {
		t.Error("a clone of nothing shown does not draw a first view")
	}
}
