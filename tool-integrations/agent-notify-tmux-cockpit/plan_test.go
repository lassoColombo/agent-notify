package main

import (
	"slices"
	"strings"
	"testing"
)

const testListWidth = 30

// said is a step as one string, which is how these tests read it.
func said(step Step) string { return strings.Join(step.Args, " ") }

func next(t *testing.T, w World, want string) (Step, bool) {
	t.Helper()
	return Next(w, "%1", want, testListWidth)
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
	if !more || !step.Spawn || !strings.HasPrefix(said(step), "split-window -h -d -t %1") {
		t.Fatalf("step = %q, spawn = %v", said(step), step.Spawn)
	}
	if command := step.Args[len(step.Args)-1]; command != "" {
		t.Errorf("the stand-in runs %q, and is to be an empty pane, with no program in it", command)
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
	if strings.Contains(got, "automatic-rename") {
		t.Errorf("a window that was given its name has nothing to keep it from: %s", got)
	}
}

// TestTheWayBackIsWrittenBeforeThePaneIsTaken: the hooks come first in the
// invocation, and tmux stops at the first command it refuses, so there is no
// borrowed pane the server does not know how to return. And the swap is
// tested by tmux as it runs: the tab on screen, the pane's own window not,
// the pane not away already.
func TestTheWayBackIsWrittenBeforeThePaneIsTaken(t *testing.T) {
	step, _ := next(t, world(true, theList, theStandIn, at("%5", "@1")), "%5")
	last, hooks := 0, 0
	for i, arg := range step.Args {
		if arg == "set-hook" {
			last, hooks = i, hooks+1
		}
	}
	if hooks != len(events)+1 {
		t.Fatalf("%d hooks are written, and there is one per event and one on the tab's window: %s", hooks, said(step))
	}
	theSwap := step.Args[len(step.Args)-1]
	if got := step.Args[last+1:]; len(got) < 6 || got[len(got)-6] != "if-shell" || got[len(got)-3] != tab ||
		got[len(got)-2] != onScreen(tab) {
		t.Errorf("after the hooks comes %q, and the swap was to be tried only with the tab on screen", got)
	}
	for _, part := range []string{
		"if-shell -F -t %5 '#{&&:#{==:#{window_active_clients},0},#{?#{" + standInOption + "},0,1}}'",
		"swap-pane -d -Z -s %2 -t %5",
	} {
		if !strings.Contains(theSwap, part) {
			t.Errorf("the swap does not say %q: %s", part, theSwap)
		}
	}
}

// TestTheHooksSendTheBorrowedPaneHome is what the server is left to run by
// itself: with nobody on the tab the pane goes home; with somebody on it the
// pane goes home all the same if a terminal has come to the window it lives
// in, and the list's key is typed into its pane. And a tab whose panes change wakes its list, or,
// when the list is what went, closes itself, the pane sent home first.
func TestTheHooksSendTheBorrowedPaneHome(t *testing.T) {
	borrowed := at("%5", tab).awayFrom("%2")
	hooks := watching(tab, "%1", "%2", &borrowed)
	key := "send-keys -t %1 " + wakeKey
	home := "if-shell -F -t %5 '#{==:#{" + standInOption + "},%2}' { swap-pane -d -Z -s %2 -t %5 ; " +
		"set-option -p -u -t %5 " + standInOption + " ; set-option -p -u -t %5 " + homeSessionOption + " }"
	want := "if-shell -F -t " + tab + " '" + onScreen(tab) + "' " +
		"{ if-shell -F -t %2 '#{&&:#{==:#{pane_id},%2},#{&&:#{window_active_clients},#{!=:#{window_id}," + tab + "}}}' { " + home + " } ; " + key + " } " +
		"{ " + home + " }"
	for i, event := range events {
		got := hooks[i : i+1].argv()
		if len(got) != 4 || got[0] != "set-hook" || got[1] != "-g" || got[2] != event+"["+hookName+"]" || got[3] != want {
			t.Errorf("on %s:\n%s\nwant\n%s", event, strings.Join(got, " "), want)
		}
	}
	onTheTab := strings.Join(hooks[len(events):].argv(), " ")
	if !strings.HasPrefix(onTheTab, "set-hook -w -t "+tab+" window-layout-changed["+hookName+"] if-shell -F -t "+tab+
		" '#{&&:#{==:#{window_id},"+tab+"},#{==:#{P:#{?#{==:#{"+roleOption+"},list},1,}},}}' { if-shell -F -t "+tab+
		" '#{&&:#{==:#{window_id},"+tab+"},#{P:#{?#{"+standInOption+"},1,}}}' { "+home+" } ; ") ||
		!strings.HasSuffix(onTheTab, "; kill-pane -t %2 } { "+key+" }") {
		t.Errorf("on the tab's window: %s", onTheTab)
	}

	// and with nothing borrowed there is only the list to wake
	idle := watching(tab, "%1", "%2", nil)
	if got := idle[:1].argv()[3]; got != "if-shell -F -t "+tab+" '"+onScreen(tab)+"' { "+key+" }" {
		t.Errorf("with nothing borrowed, an event runs: %s", got)
	}
	if got := strings.Join(idle.argv(), " "); strings.Contains(got, "swap-pane") {
		t.Errorf("with nothing borrowed, something is swapped: %s", got)
	}
}

// TestAWindowKeepsItsNameWhileItsPaneIsAway: a window that names itself would
// take the stand-in's name, which is none. Its `automatic-rename` is held off
// by the borrow and given back with the pane, by whoever gives it back.
func TestAWindowKeepsItsNameWhileItsPaneIsAway(t *testing.T) {
	step, _ := next(t, world(true, theList, theStandIn, at("%5", "@1").naming()), "%5")
	got := said(step)
	// after the swap, which tmux refuses if a pane is gone: nothing is then
	// written on a window that nobody would come back to
	held, swapped := strings.LastIndex(got, "set-option -w -t @1 automatic-rename off"), strings.LastIndex(got, "swap-pane")
	if swapped < 0 || held < swapped || !strings.Contains(got, "set-option -p -t %5 "+renamedOption+" @1") {
		t.Errorf("the name is not held after the swap, or the pane does not say so: %s", got)
	}
	if !strings.Contains(step.Args[3], "set-option -w -u -t @1 automatic-rename") {
		t.Errorf("the hook that sends it home does not give the name back: %s", step.Args[3])
	}

	away := at("%5", tab).awayFrom("%2")
	away.Renamed = "@1"
	w := world(true, theList, away, at("%2", "@1").as(roleStandIn))
	step, _ = next(t, w, "")
	if got := said(step); !strings.Contains(got, "set-option -w -u -t @1 automatic-rename") ||
		!strings.Contains(got, "set-option -p -u -t %5 "+renamedOption) {
		t.Errorf("giving it back does not give the name back: %s", got)
	}
	// a pane whose window is gone has nothing to give a name back to
	step, _ = next(t, world(true, theList, away), "")
	if got := said(step); strings.Contains(got, "automatic-rename") || !strings.Contains(got, "-u -t %5 "+renamedOption) {
		t.Errorf("step = %s", got)
	}
}

// TestACommandIsWrittenTwoWays: as arguments, and as text inside one.
func TestACommandIsWrittenTwoWays(t *testing.T) {
	commands := block{
		{"if-shell", "-F", "-t", "%5", "#{a,b}", block{{"set-option", "-p", "-t", "%5", "@x", "$1"}, {"kill-pane"}}},
		{"split-window", ""},
	}
	if got := commands.argv(); !slices.Equal(got, []string{
		"if-shell", "-F", "-t", "%5", "#{a,b}", "set-option -p -t %5 @x '$1' ; kill-pane", ";", "split-window", "",
	}) {
		t.Errorf("argv = %q", got)
	}
	if got := commands.text(); got != "if-shell -F -t %5 '#{a,b}' { set-option -p -t %5 @x '$1' ; kill-pane } ; split-window ''" {
		t.Errorf("text = %s", got)
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
	if !strings.Contains(got, "; set-hook -g "+events[0]+"["+hookName+"] ") {
		t.Errorf("the hooks still say how to send home a pane that is home: %s", got)
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
	seen, unseen := world(true, theList, theStandIn, at("%5", "@1")), world(false, theList, theStandIn, at("%5", "@1"))
	lookedAt := world(true, theList, theStandIn, at("%5", "@1").onScreen())
	lookedAtWhileAway := world(true, theList, at("%5", tab).awayFrom("%2"), at("%2", "@1").as(roleStandIn).onScreen())
	for _, one := range []struct {
		name   string
		world  World
		desire Desire
		want   string
	}{
		{"on screen", seen, Desire{Staged: "%5"}, "%5"},
		{"nobody looking", unseen, Desire{Staged: "%5"}, ""},
		{"a terminal is on the window it lives in", lookedAt, Desire{Staged: "%5"}, ""},
		{"a terminal came to the window it lives in", lookedAtWhileAway, Desire{Staged: "%5"}, ""},
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
