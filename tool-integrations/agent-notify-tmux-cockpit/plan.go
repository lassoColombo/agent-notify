package main

import (
	"fmt"
	"strconv"
	"strings"
)

// This file is pure: it decides what should be run and returns it as data.
//
// The tab shows an agent by borrowing its pane. `swap-pane` exchanges it with
// the stand-in, an empty pane that holds its place: the window the agent
// lives in keeps its layout, its zoom and its active pane, with the stand-in
// where the agent was, and giving the agent back is the same swap again
// [measured on 3.8]. Nothing is remembered between two steps. Each one is
// read off the server as it is now, so the tab comes right from whatever it
// finds — a pane that closed, a window that was killed, a list that was
// restarted (R22).
//
// A borrow leaves its own way back behind, as hooks (D-129). The server runs
// them the moment a terminal comes to show another window, before it draws
// that window: so an agent is away only while the tab is on screen and the
// window it lives in is not, whether or not this program is there to see to
// it.

// Step is one tmux invocation.
type Step struct {
	Args []string
	// Spawn says Args print the id of a new stand-in, which is to be marked
	// as one before anything else is decided.
	Spawn bool
	Why   string
}

// Desire is what the list asks of the tab.
type Desire struct {
	// Staged is the pane the stage should show, once the cursor has rested on
	// it.
	Staged string
	// Leaving is on the way out of the tab, to a pane or for good: everything
	// goes home first.
	Leaving bool
}

// Want is the pane that should be on the stage now, or none. An agent is away
// from home only while some terminal shows the tab and none shows the window
// it lives in: when nobody is looking at the tab, or somebody is looking at
// that window, everything is where it lives.
func Want(world World, list string, desire Desire) string {
	if desire.Leaving || !world.Visible(list) || world.OnScreenAtHome(desire.Staged, world.Panes[list].Window) {
		return ""
	}
	return desire.Staged
}

// Next is the one step that brings the tab nearer to showing `want`, and false
// when there is nothing left to do. The caller runs it, looks at the server
// again and asks again.
//
// listWidth is the list's width in columns, set when the stand-in is put
// beside it and left to the user after that.
func Next(world World, list, want string, listWidth int) (Step, bool) {
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
		// An empty pane: no program runs in it, so none can die in it.
		return Step{
			Args:  []string{"split-window", "-h", "-d", "-t", list, "-P", "-F", "#{pane_id}", ""},
			Spawn: true,
			Why:   "the tab has no stand-in",
		}, true
	}
	standIn := standIns[0]

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
		commands := block{giving(pane)}
		why := fmt.Sprintf("%s goes home", pane.ID)
		// The stand-in comes back to the stage, and the next agent can take it
		// in the same invocation: one redraw of the tab rather than two.
		if next, there := world.Panes[want]; there && len(borrowed) == 1 &&
			pane.Window == tab && pane.StandIn == standIn.ID && borrowable(next, tab) {
			commands = append(commands, take(next, list, standIn.ID, tab)...)
			why += fmt.Sprintf(", and %s takes the stage", want)
		} else {
			commands = append(commands, watching(tab, list, standIn.ID, nil)...)
		}
		return Step{Args: commands.argv(), Why: why}, true
	}
	if len(borrowed) > 0 {
		return Step{}, false
	}

	if standIn.Window != tab {
		// The agent's pane closed while it was on the stage, and the stand-in
		// is left holding a place nobody will come back to. The window it held
		// it in gets its name back first, while it is still there: the stand-in
		// may have been all that was left of it.
		back := append(namingBack(world, standIn),
			command{"join-pane", "-h", "-d", "-s", standIn.ID, "-t", list},
			command{"resize-pane", "-t", list, "-x", strconv.Itoa(listWidth)})
		return Step{Args: back.argv(), Why: fmt.Sprintf("the stand-in %s comes back to the tab", standIn.ID)}, true
	}

	if next, there := world.Panes[want]; there && borrowable(next, tab) {
		return Step{Args: take(next, list, standIn.ID, tab).argv(), Why: fmt.Sprintf("%s takes the stage", want)}, true
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

// take puts a pane on the stage: first the way back, as hooks, and then the
// swap, so that there is never a borrowed pane the server does not know how
// to return. tmux tests, as it runs the swap, that the tab is on screen, that
// the window the pane lives in is not, and that the pane is not away already:
// the list decided on what it read a moment ago, and the list, `restore` and
// the hooks may all act on one pane, where a swap done twice is a swap undone.
func take(pane Pane, list, standIn, tab string) block {
	session := ""
	if len(pane.Views) > 0 {
		session = pane.Views[0].SessionID
	}
	taken := pane
	taken.StandIn = standIn
	// The swap first: tmux refuses it if either pane is gone, and stops
	// there, with nothing written anywhere.
	swap := block{
		{"swap-pane", "-d", "-Z", "-s", standIn, "-t", pane.ID},
		{"set-option", "-p", "-t", pane.ID, standInOption, standIn},
		{"set-option", "-p", "-t", pane.ID, homeSessionOption, session},
	}
	if pane.AutoRename {
		// The window would take the stand-in's name, which is no name at all.
		// Written on the stand-in too, which outlives the agent's pane when
		// that closes on the stage and is then what gives the name back.
		taken.Renamed = pane.Window
		swap = append(swap,
			command{"set-option", "-p", "-t", pane.ID, renamedOption, pane.Window},
			command{"set-option", "-p", "-t", standIn, renamedOption, pane.Window},
			command{"set-option", "-w", "-t", pane.Window, "automatic-rename", "off"})
	}
	atHomeAndUnseen := "#{&&:#{==:#{window_active_clients},0},#{?#{" + standInOption + "},0,1}}"
	return append(watching(tab, list, standIn, &taken), command{
		"if-shell", "-F", "-t", tab, onScreen(tab),
		block{{"if-shell", "-F", "-t", pane.ID, atHomeAndUnseen, swap}},
	})
}

// onScreen is the test that some terminal shows the tab, for an `if-shell`
// aimed at the tab's window. It names the window as well, and every test
// here is written like that, to be false of anything but what it is aimed
// at: an `if-shell` whose target is gone is not refused, it tests whatever
// pane is in front instead [measured on 3.8]. What acts is refused when its
// target is gone — `swap-pane`, `set-option`, `kill-pane` — so a test that
// went astray could at worst have run something that tmux then refused. But
// a hook that is refused says so on the status line of whoever pressed the
// key, every time [measured on 3.8], and this keeps it from running at all.
func onScreen(tab string) string {
	return "#{&&:#{==:#{window_id}," + tab + "},#{window_active_clients}}"
}

// giving swaps a borrowed pane with the stand-in that holds its place, and
// takes off what was written on it and on its window; only if that stand-in
// is still the one it names.
func giving(pane Pane) command {
	back := block{
		{"swap-pane", "-d", "-Z", "-s", pane.StandIn, "-t", pane.ID},
		{"set-option", "-p", "-u", "-t", pane.ID, standInOption},
		{"set-option", "-p", "-u", "-t", pane.ID, homeSessionOption},
	}
	if pane.Renamed != "" {
		back = append(back,
			command{"set-option", "-p", "-u", "-t", pane.ID, renamedOption},
			command{"set-option", "-p", "-u", "-t", pane.StandIn, renamedOption},
			command{"set-option", "-w", "-u", "-t", pane.Renamed, "automatic-rename"})
	}
	return command{"if-shell", "-F", "-t", pane.ID, "#{==:#{" + standInOption + "}," + pane.StandIn + "}", back}
}

// giveBack is giving as one invocation.
func giveBack(pane Pane) []string { return block{giving(pane)}.argv() }

// watching is the tab's hooks, written whole each time: what the server runs
// by itself, with nobody to ask.
//
// On every event that can change what a terminal shows: if no terminal shows
// the tab, the borrowed pane goes home; if one does, the borrowed pane goes
// home all the same when a terminal has come to the window it lives in, and
// then the list is woken. The ids are written into the hooks rather than
// read from options as they run, because a command with its ids in it runs
// before the server draws, and one that has to expand them (`run-shell -C`)
// runs after: a frame of the stand-in where the agent lives [measured on
// 3.8].
//
// And on the tab's window, whenever its panes change: the list is woken, as
// when the agent on the stage has closed its pane; and when the list itself
// is gone, however it went, the borrowed pane goes home, the hooks go, and
// the stand-in is closed, which closes the tab.
//
// What is left behind by a tab that was killed does nothing, and says
// nothing, until the next tab writes over it: its tests are false of every
// window and pane that is still there (see onScreen).
func watching(tab, list, standIn string, borrowed *Pane) block {
	woken := command{"send-keys", "-t", list, wakeKey}
	each := command{"if-shell", "-F", "-t", tab, onScreen(tab), block{woken}}
	closing := block{}
	if borrowed != nil {
		home := giving(*borrowed)
		seenAtHome := "#{&&:#{==:#{pane_id}," + standIn + "},#{&&:#{window_active_clients},#{!=:#{window_id}," + tab + "}}}"
		// The way home before the key: whatever tmux refuses ends the hook
		// there.
		each = command{"if-shell", "-F", "-t", tab, onScreen(tab),
			block{{"if-shell", "-F", "-t", standIn, seenAtHome, block{home}}, woken},
			block{home},
		}
		// Only if the tab does hold a borrowed pane, for the same reason.
		closing = append(closing, command{"if-shell", "-F", "-t", tab,
			"#{&&:#{==:#{window_id}," + tab + "},#{P:#{?#{" + standInOption + "},1,}}}", block{home}})
	}
	var hooks block
	for _, event := range events {
		hooks = append(hooks, command{"set-hook", "-g", hookOf(event), block{each}})
	}
	closing = append(append(closing, unwatching()...), command{"kill-pane", "-t", standIn})
	listIsGone := "#{&&:#{==:#{window_id}," + tab + "},#{==:#{P:#{?#{==:#{" + roleOption + "}," + roleList + "},1,}},}}"
	return append(hooks, command{"set-hook", "-w", "-t", tab, hookOf("window-layout-changed"),
		block{{"if-shell", "-F", "-t", tab, listIsGone, closing, block{woken}}}})
}

// unwatching takes the hooks on the server away. The one on the tab's window
// goes with the window.
func unwatching() block {
	var hooks block
	for _, event := range events {
		hooks = append(hooks, command{"set-hook", "-g", "-u", hookOf(event)})
	}
	return hooks
}

// hookOf is this program's own member of a hook, which is an array: setting
// and unsetting it leaves every other member as it was.
func hookOf(event string) string { return event + "[" + hookName + "]" }

// forgetting takes what was written on a borrowed pane off it, and gives the
// window it lived in its name back if that window is still there.
func forgetting(world World, pane Pane) block {
	return append(block{
		{"set-option", "-p", "-u", "-t", pane.ID, standInOption},
		{"set-option", "-p", "-u", "-t", pane.ID, homeSessionOption},
	}, namingBack(world, pane)...)
}

// namingBack gives back a window's naming that a borrow held off, as a
// borrowed pane or a stand-in records it, if that window is still there.
func namingBack(world World, pane Pane) block {
	if pane.Renamed == "" {
		return nil
	}
	back := block{{"set-option", "-p", "-u", "-t", pane.ID, renamedOption}}
	if world.hasWindow(pane.Renamed) {
		back = append(back, command{"set-option", "-w", "-u", "-t", pane.Renamed, "automatic-rename"})
	}
	return back
}

// astray is the step for a borrowed pane whose stand-in is gone, and with it
// the place to go back to: the window it lived in was killed, or the stand-in
// was. It is never killed for that (R5). Out of the tab it goes, into a window
// of its own in the session it came from, and from there it is an agent like
// any other.
func astray(world World, pane Pane, tab string) Step {
	forget := forgetting(world, pane)
	if pane.Window != tab {
		return Step{Args: forget.argv(), Why: fmt.Sprintf("%s was moved by somebody else, and lives where it is", pane.ID)}
	}
	session := world.SessionFor(pane.HomeSession)
	return Step{
		Args: append(block{{"break-pane", "-d", "-s", pane.ID, "-t", session + ":"}}, forget...).argv(),
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

// A command is one tmux command as its words, and a block is commands run one
// after another. A block among a command's words is what that command runs in
// its turn: `if-shell`'s branches, a hook's body. One tree is written two
// ways. On tmux's command line a word is an argument as it stands, and a
// block is one argument holding its commands as text; inside that text a
// block is in braces, and a word is quoted wherever tmux's parser would
// otherwise read something into it (a session's id is `$` and a number, which
// it would look up as a variable).
type (
	command []any
	block   []command
)

// argv is the block as the arguments of one tmux invocation.
func (b block) argv() []string {
	var arguments []string
	for i, one := range b {
		if i > 0 {
			arguments = append(arguments, ";")
		}
		for _, word := range one {
			switch word := word.(type) {
			case string:
				arguments = append(arguments, word)
			case block:
				arguments = append(arguments, word.text())
			}
		}
	}
	return arguments
}

// text is the block as tmux would read it from a file.
func (b block) text() string {
	commands := make([]string, len(b))
	for i, one := range b {
		words := make([]string, len(one))
		for j, word := range one {
			switch word := word.(type) {
			case string:
				words[j] = quoted(word)
			case block:
				words[j] = "{ " + word.text() + " }"
			}
		}
		commands[i] = strings.Join(words, " ")
	}
	return strings.Join(commands, " ; ")
}

// plain is what tmux's parser reads as itself: names, flags and the ids of
// panes and windows.
const plain = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_@%"

// quoted is a word as tmux's parser reads it back unchanged. None of what
// this program writes holds a single quote.
func quoted(word string) string {
	if word != "" && strings.Trim(word, plain) == "" {
		return word
	}
	return "'" + word + "'"
}
