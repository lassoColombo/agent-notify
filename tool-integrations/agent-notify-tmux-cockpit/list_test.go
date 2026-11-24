package main

import (
	"strings"
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

// TestATerminalHasToRestOnTheTab: the hooks type the list's key every time a
// terminal comes to the tab, and one passing through on its way to another
// window must not have an agent swapped, and resized, for it.
func TestATerminalHasToRestOnTheTab(t *testing.T) {
	w := world(true, theList, theStandIn, at("%5", "@1"))
	l := aList(w, placed("a", session.Working, socket, "%5"))
	if !strings.EqualFold(wake, wakeKey) {
		t.Fatalf("the hooks type %s and the list waits for %s", wakeKey, wake)
	}
	if rest := l.key(wake); rest == nil || l.busy {
		t.Fatal("the list looked at once, and a rest was to be waited for")
	}
	l.key(wake)
	if _, look := l.Update(arrivedMsg(l.arrivals - 1)); look != nil || l.busy {
		t.Error("an earlier arrival's rest made the list look")
	}
	if _, look := l.Update(arrivedMsg(l.arrivals)); look == nil || !l.busy {
		t.Error("the terminal rested on the tab and the list did not look")
	}
}

// TestAnAgentSomebodyIsLookingAtIsLeftThere: the stage says where it is
// instead, once, and again only when it has something else to say.
func TestAnAgentSomebodyIsLookingAtIsLeftThere(t *testing.T) {
	w := world(true, theList, theStandIn.sized(100, 30), at("%5", "@1").onScreen())
	l := aList(w, placed("alpha", session.Working, socket, "%5"))
	if got := Want(w, "%1", Desire{Staged: l.staged}); got != "" {
		t.Fatalf("%q is wanted on the stage from under a terminal showing it", got)
	}
	if l.show() == nil || !strings.Contains(l.drawn, "alpha is on screen in home:1") {
		t.Fatalf("the stand-in was drawn as %q", l.drawn)
	}
	if l.show() != nil {
		t.Error("the stand-in was drawn again to say the same")
	}
	// away from the stage it is another size, and is drawn afresh once back
	l.world = world(true, theList, at("%5", tab).awayFrom("%2"), at("%2", "@1").as(roleStandIn))
	if l.show() != nil || l.drawn != "" {
		t.Errorf("a stand-in holding a place was drawn: %q", l.drawn)
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
