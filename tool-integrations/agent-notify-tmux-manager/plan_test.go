package main

import (
	"slices"
	"strings"
	"testing"
)

var testLayout = Layout{StandIn: []string{"stand-in-command"}, ListWidth: 30}

// said is a step as one string, which is how these tests read it.
func said(step Step) string { return strings.Join(step.Args, " ") }

func next(t *testing.T, w World, want string) (Step, bool) {
	t.Helper()
	return Next(w, "%1", want, testLayout)
}

// nothing fails if there is a step.
func nothing(t *testing.T, w World, want string) {
	t.Helper()
	if step, more := next(t, w, want); more {
		t.Errorf("nothing was to be run, and this was: %s (%s)", said(step), step.Why)
	}
}

// the tab at rest: the list, and the stand-in on the stage.
var (
	theList    = at("%1", tab).as(roleList).active()
	theStandIn = at("%2", tab).as(roleStandIn)
)

func TestATabWithNoStandInGetsOne(t *testing.T) {
	step, more := next(t, world(true, theList, at("%5", "@1")), "%5")
	if !more || !step.Spawn || !strings.HasPrefix(said(step), "split-window -h -d -t %1") ||
		!strings.HasSuffix(said(step), "stand-in-command") {
		t.Errorf("step = %q, spawn = %v", said(step), step.Spawn)
	}
}

func TestTheWantedPaneTakesTheStage(t *testing.T) {
	step, more := next(t, world(true, theList, theStandIn, at("%5", "@1")), "%5")
	if !more {
		t.Fatal("nothing was run")
	}
	got := said(step)
	for _, part := range []string{
		"if-shell -F -t %5", "swap-pane -d -Z -s %2 -t %5",
		standInOption + " %2", homeSessionOption + " '$1'",
	} {
		if !strings.Contains(got, part) {
			t.Errorf("the step does not say %q: %s", part, got)
		}
	}
}

func TestNothingIsRunWhenTheStageShowsWhatIsWanted(t *testing.T) {
	nothing(t, world(true, theList, at("%5", tab).awayFrom("%2"), at("%2", "@1").as(roleStandIn)), "%5")
	nothing(t, world(true, theList, theStandIn, at("%5", "@1")), "")
}

// TestMovingTheSelectionIsOneInvocation: the agent that was on the stage goes
// home and the next one arrives without the tab being drawn in between.
func TestMovingTheSelectionIsOneInvocation(t *testing.T) {
	w := world(true, theList, at("%5", tab).awayFrom("%2"), at("%2", "@1").as(roleStandIn), at("%6", "@2"))
	step, more := next(t, w, "%6")
	if !more {
		t.Fatal("nothing was run")
	}
	got := said(step)
	back, forth := strings.Index(got, "swap-pane -d -Z -s %2 -t %5"), strings.Index(got, "swap-pane -d -Z -s %2 -t %6")
	if back < 0 || forth < back {
		t.Errorf("%%5 goes home and then %%6 takes the stage, in one step: %s", got)
	}
}

func TestNobodyWantedSendsTheAgentHome(t *testing.T) {
	w := world(true, theList, at("%5", tab).awayFrom("%2"), at("%2", "@1").as(roleStandIn), at("%6", "@2"))
	step, more := next(t, w, "")
	got := said(step)
	if !more || !strings.Contains(got, "swap-pane -d -Z -s %2 -t %5") || strings.Contains(got, "%6") {
		t.Errorf("step = %s", got)
	}
	if !strings.Contains(got, "#{==:#{"+standInOption+"},%2}") {
		t.Errorf("the swap is not conditional on the pane still naming its stand-in: %s", got)
	}
}

// TestAPaneThatLostItsPlaceGetsAWindow: the window the agent lived in was
// killed while it was on the stage. The agent is not (R5).
func TestAPaneThatLostItsPlaceGetsAWindow(t *testing.T) {
	w := world(true, theList, at("%5", tab).awayFrom("%2"), at("%6", "@2"))
	step, more := next(t, w, "%5")
	if got := said(step); !more || !strings.HasPrefix(got, "break-pane -d -s %5 -t $1: ;") ||
		!strings.Contains(got, "-u -t %5 "+standInOption) {
		t.Errorf("step = %s", got)
	}
	for _, arg := range step.Args {
		if arg == "kill-pane" {
			t.Error("a pane that lost its place was killed")
		}
	}
}

// TestAPaneSomebodyMovedIsLeftWhereItIs: borrowed, then taken out of the tab
// by hand, with its stand-in gone. It lives where it was put.
func TestAPaneSomebodyMovedIsLeftWhereItIs(t *testing.T) {
	w := world(true, theList, theStandIn.as(""), at("%5", "@4").awayFrom("%3"))
	step, more := next(t, w, "")
	if got := said(step); !more || strings.Contains(got, "break-pane") || !strings.Contains(got, "-u -t %5 "+homeSessionOption) {
		t.Errorf("step = %s", got)
	}
}

func TestADeadStandInIsRespawnedWhereItStands(t *testing.T) {
	w := world(true, theList, at("%5", tab).awayFrom("%2"), at("%2", "@1").as(roleStandIn).dead())
	step, more := next(t, w, "%5")
	if got := said(step); !more || got != "respawn-pane -k -t %2 stand-in-command" {
		t.Errorf("step = %s", got)
	}
}

// TestAStandInLeftBehindComesBack: the agent's pane closed while it was on the
// stage, so the tab is the list alone and the stand-in holds a place nobody
// will return to.
func TestAStandInLeftBehindComesBack(t *testing.T) {
	step, more := next(t, world(true, theList, at("%2", "@1").as(roleStandIn)), "")
	if got := said(step); !more || got != "join-pane -h -d -s %2 -t %1 ; resize-pane -t %1 -x 30" {
		t.Errorf("step = %s", got)
	}
}

func TestASecondStandInHoldingNothingIsClosed(t *testing.T) {
	w := world(true, theList, at("%5", tab).awayFrom("%2"), at("%2", "@1").as(roleStandIn), at("%3", tab).as(roleStandIn))
	step, more := next(t, w, "%5")
	if got := said(step); !more || got != "kill-pane -t %3" {
		t.Errorf("step = %s", got)
	}
}

func TestWhatCannotBeBorrowedIsNot(t *testing.T) {
	for name, w := range map[string]World{
		"a pane that is gone":        world(true, theList, theStandIn),
		"a dead pane":                world(true, theList, theStandIn, at("%5", "@1").dead()),
		"a pane in the tab's window": world(true, theList, theStandIn, at("%5", tab)),
		"the tab's own pane":         world(true, theList, theStandIn, at("%5", "@1").as(roleStandIn).awayFrom("")),
	} {
		if step, more := next(t, w, "%5"); more && strings.Contains(said(step), "swap-pane") {
			t.Errorf("%s was borrowed: %s", name, said(step))
		}
	}
}

func TestAListThatIsGonePlansNothing(t *testing.T) {
	nothing(t, world(true, at("%5", "@1")), "%5")
}

// TestAnAgentIsAwayOnlyWhileTheTabIsOnScreen is the rule everything else
// leans on.
func TestAnAgentIsAwayOnlyWhileTheTabIsOnScreen(t *testing.T) {
	seen, unseen := world(true, theList, theStandIn), world(false, theList, theStandIn)
	for _, one := range []struct {
		name   string
		world  World
		desire Desire
		want   string
	}{
		{"on screen", seen, Desire{Staged: "%5"}, "%5"},
		{"nobody looking", unseen, Desire{Staged: "%5"}, ""},
		{"taken back at its own place", seen, Desire{Staged: "%5", Released: "%5"}, ""},
		{"another was taken back", seen, Desire{Staged: "%5", Released: "%6"}, "%5"},
		{"leaving", seen, Desire{Staged: "%5", Leaving: true}, ""},
		{"nothing selected", seen, Desire{}, ""},
	} {
		if got := Want(one.world, "%1", one.desire); got != one.want {
			t.Errorf("%s: Want = %q, want %q", one.name, got, one.want)
		}
	}
}

func TestLeavingSendsEverythingSomewhere(t *testing.T) {
	w := world(true, theList,
		at("%5", tab).awayFrom("%2"), at("%2", "@1").as(roleStandIn),
		at("%6", tab).awayFrom("%3"),
	)
	var got []string
	for _, step := range Leaving(w) {
		got = append(got, strings.Fields(said(step))[0])
	}
	if !slices.Equal(got, []string{"if-shell", "break-pane"}) {
		t.Errorf("steps = %v; %%5 is swapped back and %%6, with no place left, gets a window", got)
	}
}
