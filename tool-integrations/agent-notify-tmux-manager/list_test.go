package main

import (
	"testing"

	"github.com/lassoColombo/agent-notify/session"
)

// These are about what the cursor does when the list is rebuilt, which is
// pure: the list's model with records and a server put straight into it.

func aList(w World, records ...session.Record) *list {
	l := &list{tab: Tab{Tmux: Tmux{Socket: socket}, List: "%1", Logger: quiet}, world: w, records: records}
	l.rebuild()
	return l
}

func TestTheFirstAgentIsOnTheStageAtOnce(t *testing.T) {
	l := aList(world(true, theList, theStandIn, at("%5", "@1"), at("%6", "@1")),
		placed("idle", session.Idle, socket, "%5"),
		placed("blocked", session.BlockedOnYou, socket, "%6"),
	)
	if l.selected != "%6" || l.staged != "%6" {
		t.Errorf("selected %q, staged %q; the most urgent is %%6", l.selected, l.staged)
	}
}

// TestTheCursorStaysOnItsAgent while the list reorders around it.
func TestTheCursorStaysOnItsAgent(t *testing.T) {
	w := world(true, theList, theStandIn, at("%5", "@1"), at("%6", "@1"))
	l := aList(w, placed("a", session.Working, socket, "%5"), placed("b", session.Idle, socket, "%6"))
	l.records = []session.Record{placed("a", session.Working, socket, "%5"), placed("b", session.BlockedOnYou, socket, "%6")}
	l.rebuild()
	if l.selected != "%5" || l.index() != 1 {
		t.Errorf("selected %q at %d; the cursor was on %%5, which is now second", l.selected, l.index())
	}
}

// TestMovingTheCursorStagesNothingUntilItRests: going down a list must not
// swap every agent it passes.
func TestMovingTheCursorStagesNothingUntilItRests(t *testing.T) {
	w := world(true, theList, theStandIn, at("%5", "@1"), at("%6", "@1"))
	l := aList(w, placed("a", session.Working, socket, "%5"), placed("b", session.Idle, socket, "%6"))
	if rest := l.moveTo(1); rest == nil || l.selected != "%6" || l.staged != "%5" {
		t.Fatalf("selected %q, staged %q, and a rest was to be waited for", l.selected, l.staged)
	}
	l.Update(settledMsg(l.moves - 1))
	if l.staged != "%5" {
		t.Error("an earlier move's rest staged the pane")
	}
	l.Update(settledMsg(l.moves))
	if l.staged != "%6" {
		t.Errorf("the cursor rested on %%6 and %q is staged", l.staged)
	}
}

// TestWhileSomebodyIsOnTheStageNothingMoves: the agent they are typing into
// ends. The next one must not arrive under their hands.
func TestWhileSomebodyIsOnTheStageNothingMoves(t *testing.T) {
	list, onStage := at("%1", tab).as(roleList), at("%5", tab).awayFrom("%2").active()
	typing := world(true, list, onStage, at("%2", "@1").as(roleStandIn), at("%6", "@1"))
	a, b := placed("a", session.Working, socket, "%5"), placed("b", session.Idle, socket, "%6")
	l := aList(typing, a, b)

	l.records = []session.Record{b}
	l.rebuild()
	if l.selected != "%5" || l.staged != "%5" {
		t.Fatalf("selected %q, staged %q: the stage changed under whoever is typing into it", l.selected, l.staged)
	}

	// back at the list, the cursor takes the agent now in its place
	l.world = world(true, list.active(), at("%5", tab).awayFrom("%2"), at("%2", "@1").as(roleStandIn), at("%6", "@1"))
	l.rebuild()
	if l.selected != "%6" {
		t.Errorf("selected %q once the list has the focus again", l.selected)
	}
}

// TestAnAgentTakenBackIsAskedForAgainByComingBackToIt: Enter in its stand-in
// releases it; it stays at its own place while the cursor is on it, and
// leaving and returning asks for it again.
func TestAnAgentTakenBackIsAskedForAgainByComingBackToIt(t *testing.T) {
	w := world(true, theList, theStandIn, at("%5", "@1"), at("%6", "@1"))
	l := aList(w, placed("a", session.Working, socket, "%5"), placed("b", session.Idle, socket, "%6"))
	// the stand-in is holding %5's place, and Enter is pressed in it
	l.world = world(true, theList, at("%5", tab).awayFrom("%2"), at("%2", "@1").as(roleStandIn), at("%6", "@1"))
	l.Update(heardMsg(pressedEnter))
	if got := Want(w, "%1", Desire{Staged: l.staged, Released: l.released}); got != "" {
		t.Fatalf("%q is wanted on the stage after being taken back", got)
	}
	// and Enter in the stand-in once it is back on the stage asks for it again
	l.world = w
	l.Update(heardMsg(pressedEnter))
	if got := Want(w, "%1", Desire{Staged: l.staged, Released: l.released}); got != "%5" {
		t.Fatalf("Want = %q after Enter on the stage", got)
	}
	l.released = "%5"
	l.moveTo(1)
	l.Update(settledMsg(l.moves))
	l.moveTo(0)
	l.Update(settledMsg(l.moves))
	if got := Want(w, "%1", Desire{Staged: l.staged, Released: l.released}); got != "%5" {
		t.Errorf("Want = %q after coming back to it", got)
	}
}

func TestNoAgentsIsNothingSelected(t *testing.T) {
	w := world(true, theList, theStandIn, at("%5", "@1"))
	l := aList(w, placed("a", session.Working, socket, "%5"))
	l.records = nil
	l.rebuild()
	if l.selected != "" || l.staged != "" {
		t.Errorf("selected %q, staged %q, with no agents", l.selected, l.staged)
	}
}
