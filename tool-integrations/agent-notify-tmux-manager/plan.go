package main

import (
	"fmt"
	"strconv"
	"strings"
)

// This file is pure: it decides what should be run and returns it as data.
//
// The tab shows an agent by borrowing its pane. `swap-pane` exchanges it with
// the stand-in, a pane of this program's that holds its place: the window the
// agent lives in keeps its layout, its zoom and its active pane, with the
// stand-in where the agent was, and giving the agent back is the same swap
// again [measured on 3.8]. Nothing is remembered between two steps. Each one
// is read off the server as it is now, so the tab comes right from whatever
// it finds — a pane that closed, a window that was killed, a list that was
// restarted (R22).

// Step is one tmux invocation.
type Step struct {
	Args []string
	// Spawn says Args print the id of a new pane running the stand-in, which
	// is to be marked as one before anything else is decided.
	Spawn bool
	Why   string
}

// Layout is how the tab is built.
type Layout struct {
	// StandIn is the command a stand-in pane runs.
	StandIn []string
	// ListWidth is the list's width in columns, set when the stand-in is put
	// beside it and left to the user after that.
	ListWidth int
}

// Desire is what the list asks of the tab.
type Desire struct {
	// Staged is the pane the stage should show, once the cursor has rested on
	// it.
	Staged string
	// Released is a pane that was taken back at its own place, and stays there
	// until it is asked for again.
	Released string
	// Leaving is on the way out of the tab, to a pane or for good: everything
	// goes home first.
	Leaving bool
}

// Want is the pane that should be on the stage now, or none. An agent is away
// from home only while some terminal shows the tab: when nobody is looking,
// everything is where it lives.
func Want(world World, list string, desire Desire) string {
	if desire.Leaving || desire.Staged == desire.Released || !world.Visible(list) {
		return ""
	}
	return desire.Staged
}

// Next is the one step that brings the tab nearer to showing `want`, and false
// when there is nothing left to do. The caller runs it, looks at the server
// again and asks again.
func Next(world World, list, want string, layout Layout) (Step, bool) {
	self, alive := world.Panes[list]
	if !alive {
		return Step{}, false
	}
	tab := self.Window

	for _, pane := range world.Borrowed() {
		if !world.holding(pane.StandIn) {
			return astray(world, pane, tab), true
		}
	}

	standIns := world.StandIns(tab)
	if len(standIns) == 0 {
		return Step{
			Args: append([]string{"split-window", "-h", "-d", "-t", list, "-P", "-F", "#{pane_id}"},
				layout.StandIn...),
			Spawn: true,
			Why:   "the tab has no stand-in",
		}, true
	}
	standIn := standIns[0]
	if standIn.Dead {
		// Its program died and `remain-on-exit` kept the pane, and with it
		// the place it was holding.
		return Step{
			Args: append([]string{"respawn-pane", "-k", "-t", standIn.ID}, layout.StandIn...),
			Why:  fmt.Sprintf("the stand-in %s died where it stood", standIn.ID),
		}, true
	}

	borrowed := world.Borrowed()
	for _, extra := range standIns[1:] {
		if !holdsAPlace(borrowed, extra.ID) {
			return Step{Args: []string{"kill-pane", "-t", extra.ID},
				Why: fmt.Sprintf("%s is a second stand-in holding nothing", extra.ID)}, true
		}
	}

	for _, pane := range borrowed {
		if pane.ID == want {
			continue
		}
		step := Step{
			Args: giveBack(pane),
			Why:  fmt.Sprintf("%s goes home", pane.ID),
		}
		// The stand-in comes back to the stage, and the next agent can take it
		// in the same invocation: one redraw of the tab rather than two.
		if next, there := world.Panes[want]; there && len(borrowed) == 1 &&
			pane.Window == tab && pane.StandIn == standIn.ID && borrowable(next, tab) {
			step.Args = append(append(step.Args, ";"), take(next, standIn.ID)...)
			step.Why += fmt.Sprintf(", and %s takes the stage", want)
		}
		return step, true
	}
	if len(borrowed) > 0 {
		return Step{}, false
	}

	if standIn.Window != tab {
		// The agent's pane closed while it was on the stage, and the stand-in
		// is left holding a place nobody will come back to.
		return Step{
			Args: []string{"join-pane", "-h", "-d", "-s", standIn.ID, "-t", list, ";",
				"resize-pane", "-t", list, "-x", strconv.Itoa(layout.ListWidth)},
			Why: fmt.Sprintf("the stand-in %s comes back to the tab", standIn.ID),
		}, true
	}

	if next, there := world.Panes[want]; there && borrowable(next, tab) {
		return Step{Args: take(next, standIn.ID), Why: fmt.Sprintf("%s takes the stage", want)}, true
	}
	return Step{}, false
}

// borrowable is whether a pane can be put on the stage: a live pane of
// somebody else's, in another window, that is not away already.
func borrowable(pane Pane, tab string) bool {
	return !pane.Dead && pane.Role == "" && pane.StandIn == "" && pane.Window != tab
}

func holdsAPlace(borrowed []Pane, standIn string) bool {
	for _, pane := range borrowed {
		if pane.StandIn == standIn {
			return true
		}
	}
	return false
}

// take puts a pane on the stage and writes on it where its place is. The swap
// runs only if the pane is not away already, and tmux tests that as it runs
// the command: the list, a stand-in left alone and `restore` may all act on
// one pane, and a swap done twice is a swap undone.
func take(pane Pane, standIn string) []string {
	session := ""
	if len(pane.Views) > 0 {
		session = pane.Views[0].SessionID
	}
	return []string{"if-shell", "-F", "-t", pane.ID, "#{?#{" + standInOption + "},0,1}", strings.Join([]string{
		"swap-pane -d -Z -s " + standIn + " -t " + pane.ID,
		"set-option -p -t " + pane.ID + " " + standInOption + " " + standIn,
		// quoted: a session id is `$` and a number, which tmux's parser would
		// otherwise look up as a variable
		"set-option -p -t " + pane.ID + " " + homeSessionOption + " '" + session + "'",
	}, " ; ")}
}

// giveBack swaps a borrowed pane with the stand-in that holds its place, and
// takes off what was written on it; only if that stand-in is still the one it
// names.
func giveBack(pane Pane) []string {
	return []string{"if-shell", "-F", "-t", pane.ID, "#{==:#{" + standInOption + "}," + pane.StandIn + "}",
		"swap-pane -d -Z -s " + pane.StandIn + " -t " + pane.ID + " ; " + strings.Join(forgetting(pane.ID), " ")}
}

// forgetting takes what was written on a borrowed pane off it.
func forgetting(pane string) []string {
	return []string{
		"set-option", "-p", "-u", "-t", pane, standInOption, ";",
		"set-option", "-p", "-u", "-t", pane, homeSessionOption,
	}
}

// marking makes a new pane the tab's stand-in. `remain-on-exit` is what keeps
// the place it holds if its program dies: the pane stays, dead, and is
// respawned where it stands.
func marking(pane string) []string {
	return []string{
		"set-option", "-p", "-t", pane, roleOption, roleStandIn, ";",
		"set-option", "-p", "-t", pane, "remain-on-exit", "on",
	}
}

// astray is the step for a borrowed pane whose stand-in is gone, and with it
// the place to go back to: the window it lived in was killed, or the stand-in
// was. It is never killed for that (R5). Out of the tab it goes, into a window
// of its own in the session it came from, and from there it is an agent like
// any other.
func astray(world World, pane Pane, tab string) Step {
	forget := forgetting(pane.ID)
	if pane.Window != tab {
		return Step{Args: forget, Why: fmt.Sprintf("%s was moved by somebody else, and lives where it is", pane.ID)}
	}
	session := world.SessionFor(pane.HomeSession)
	return Step{
		Args: append([]string{"break-pane", "-d", "-s", pane.ID, "-t", session + ":", ";"}, forget...),
		Why:  fmt.Sprintf("%s lost its place, and gets a window in %s", pane.ID, session),
	}
}

// Leaving is everything borrowed going home: what `restore` runs, and what
// closing the tab runs first.
func Leaving(world World) []Step {
	tab := ""
	if list, found := world.List(); found {
		tab = list.Window
	}
	var steps []Step
	for _, pane := range world.Borrowed() {
		if world.holding(pane.StandIn) {
			steps = append(steps, Step{Args: giveBack(pane), Why: fmt.Sprintf("%s goes home", pane.ID)})
		} else {
			steps = append(steps, astray(world, pane, tab))
		}
	}
	return steps
}
