package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/lassoColombo/agent-notify/tool"
)

// World is what one tmux server holds, as one `list-panes -a` said it. It is
// read afresh before anything is decided, and never remembered across a
// decision (R17, R22).
type World struct {
	Panes map[string]Pane
}

// Pane is one pane and what this program wrote on it.
type Pane struct {
	ID     string
	Window string
	// Active is whether it is its window's active pane.
	Active bool
	// Dead is a pane whose program exited and which `remain-on-exit` kept.
	Dead bool
	// Role is `list` or `stand-in` on the tab's own two panes.
	Role string
	// StandIn and HomeSession are set on a borrowed pane.
	StandIn, HomeSession string
	// Views is one per session the pane's window is linked into.
	Views []View
}

// View is a pane's window as one session shows it.
type View struct {
	SessionID, SessionName string
	WindowIndex            int
	// WindowActive is whether the window is that session's current one, and
	// Attached how many terminals are on the session.
	WindowActive bool
	Attached     int
}

var worldFormat = strings.Join([]string{
	"#{pane_id}", "#{window_id}", "#{pane_active}", "#{pane_dead}",
	"#{" + roleOption + "}", "#{" + standInOption + "}", "#{" + homeSessionOption + "}",
	"#{session_id}", "#{session_name}", "#{window_index}", "#{window_active}", "#{session_attached}",
}, separator)

const worldFields = 12

// Observe asks the server what it holds.
func (t Tmux) Observe() (World, error) {
	out, err := t.run("list-panes", "-a", "-F", worldFormat)
	if err != nil {
		return World{}, err
	}
	return ParseWorld(out)
}

// ParseWorld is the pure half of Observe. The parse is the test of whether the
// server answered: anything that is not rows of this format is not believed.
func ParseWorld(output string) (World, error) {
	world := World{Panes: map[string]Pane{}}
	for _, line := range strings.Split(output, "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, separator)
		if len(fields) != worldFields || !IsPaneID(fields[0]) {
			return World{}, fmt.Errorf("this is not what `list-panes -F` was asked to print: %q",
				tool.Summarise([]byte(line)))
		}
		pane, seen := world.Panes[fields[0]]
		if !seen {
			pane = Pane{
				ID: fields[0], Window: fields[1], Active: fields[2] == "1", Dead: fields[3] == "1",
				Role: fields[4], StandIn: fields[5], HomeSession: fields[6],
			}
		}
		index, _ := strconv.Atoi(fields[9])
		attached, _ := strconv.Atoi(fields[11])
		pane.Views = append(pane.Views, View{
			SessionID: fields[7], SessionName: fields[8], WindowIndex: index,
			WindowActive: fields[10] == "1", Attached: attached,
		})
		world.Panes[pane.ID] = pane
	}
	if len(world.Panes) == 0 {
		// A running server always has a session, and a session a pane.
		return World{}, fmt.Errorf("no panes at all")
	}
	return world, nil
}

// sorted is the panes by number, so that nothing downstream depends on the
// order a map hands them out in.
func (w World) sorted() []Pane {
	panes := make([]Pane, 0, len(w.Panes))
	for _, pane := range w.Panes {
		panes = append(panes, pane)
	}
	slices.SortFunc(panes, func(a, b Pane) int { return number(a.ID) - number(b.ID) })
	return panes
}

func number(id string) int {
	n, _ := strconv.Atoi(strings.TrimLeft(id, "%@$"))
	return n
}

// List is the tab's list pane, for a caller that is not the list itself.
func (w World) List() (Pane, bool) {
	for _, pane := range w.sorted() {
		if pane.Role == roleList {
			return pane, true
		}
	}
	return Pane{}, false
}

// Borrowed is every pane that says it is away from home.
func (w World) Borrowed() []Pane {
	var borrowed []Pane
	for _, pane := range w.sorted() {
		if pane.StandIn != "" {
			borrowed = append(borrowed, pane)
		}
	}
	return borrowed
}

// holding is whether `standIn` is a stand-in that is still there, which is
// what makes a borrowed pane's way home.
func (w World) holding(standIn string) bool {
	pane, exists := w.Panes[standIn]
	return exists && pane.Role == roleStandIn
}

// StandIns is every stand-in: the one the tab works with first, then any
// other. That one is the stand-in holding a borrowed pane's place if there is
// one, else the one in the tab's window, else the oldest.
func (w World) StandIns(tab string) []Pane {
	named := map[string]bool{}
	for _, pane := range w.Borrowed() {
		named[pane.StandIn] = true
	}
	var standIns []Pane
	for _, pane := range w.sorted() {
		if pane.Role == roleStandIn {
			standIns = append(standIns, pane)
		}
	}
	weight := func(pane Pane) int {
		switch {
		case named[pane.ID]:
			return 0
		case pane.Window == tab:
			return 1
		}
		return 2
	}
	slices.SortStableFunc(standIns, func(a, b Pane) int { return weight(a) - weight(b) })
	return standIns
}

// Visible is whether some terminal is showing the window `pane` is in.
func (w World) Visible(pane string) bool {
	for _, view := range w.Panes[pane].Views {
		if view.Attached > 0 && view.WindowActive {
			return true
		}
	}
	return false
}

// Where is `session:window` for a pane, as a person would say it. A borrowed
// pane is where its stand-in is, which is where it lives; and of the sessions
// a window is linked into, the tab's own hidden one is the last choice.
func (w World) Where(pane string) string {
	at := w.Panes[pane]
	if standIn, away := w.Panes[at.StandIn]; away {
		at = standIn
	}
	if len(at.Views) == 0 {
		return ""
	}
	view := at.Views[0]
	for _, other := range at.Views {
		if other.SessionName != tabSession {
			view = other
			break
		}
	}
	return view.SessionName + ":" + strconv.Itoa(view.WindowIndex)
}

// SessionFor is the session to give a pane a window in, when the place it was
// borrowed from is gone: the session it came from if that is still there, else
// any that is not the tab's own.
func (w World) SessionFor(home string) string {
	fallback, hidden := "", ""
	for _, pane := range w.sorted() {
		for _, view := range pane.Views {
			switch {
			case view.SessionID == home:
				return home
			case view.SessionName == tabSession:
				hidden = view.SessionID
			case fallback == "":
				fallback = view.SessionID
			}
		}
	}
	if fallback != "" {
		return fallback
	}
	return hidden
}

// Stage is the pane on the tab's right: the borrowed one, or the stand-in
// while nothing is.
func (w World) Stage(list string) (string, bool) {
	tab := w.Panes[list].Window
	stage, found := "", false
	for _, pane := range w.sorted() {
		if pane.Window != tab {
			continue
		}
		if pane.StandIn != "" {
			return pane.ID, true
		}
		if pane.Role == roleStandIn && !found {
			stage, found = pane.ID, true
		}
	}
	return stage, found
}

// StageFocused is whether somebody is on the stage: a terminal shows the tab,
// and the list is not its active pane.
func (w World) StageFocused(list string) bool {
	return w.Visible(list) && !w.Panes[list].Active
}
