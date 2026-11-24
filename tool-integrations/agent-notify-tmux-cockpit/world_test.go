package main

import (
	"slices"
	"strings"
	"testing"
)

// The worlds the tests are about are written with these: a pane in a window,
// and what is then said about it.
//
//	at("%5", "@1")                        an ordinary pane, in session home
//	at("%1", tab).as(roleList)            the tab's list
//	at("%5", tab).awayFrom("%2")          a borrowed pane, with %2 holding its place
const tab = "@9"

func at(id, window string) Pane {
	return Pane{ID: id, Window: window, Views: []View{{
		SessionID: "$1", SessionName: "home", WindowIndex: number(window), Attached: 1,
	}}}
}

func (p Pane) as(role string) Pane { p.Role = role; return p }
func (p Pane) active() Pane        { p.Active = true; return p }
func (p Pane) dead() Pane          { p.Dead = true; return p }

// naming is a pane whose window takes its name from it, and onScreen one
// whose window some terminal is showing.
func (p Pane) naming() Pane { p.AutoRename = true; return p }

func (p Pane) sized(width, height int) Pane { p.Width, p.Height = width, height; return p }

func (p Pane) onScreen() Pane {
	p.Views = slices.Clone(p.Views)
	for i := range p.Views {
		p.Views[i].WindowActive = true
	}
	return p
}

func (p Pane) awayFrom(standIn string) Pane {
	p.StandIn, p.HomeSession = standIn, "$1"
	return p
}

// in puts the pane's window in another session.
func (p Pane) in(id, name string) Pane {
	p.Views = []View{{SessionID: id, SessionName: name, WindowIndex: number(p.Window), Attached: 1}}
	return p
}

// serverPID is the pid of the server the worlds here are read from, which is
// the one `placed` captures its sessions in.
const serverPID = 42

// world is these panes, with the tab's window in front or not.
func world(tabInFront bool, panes ...Pane) World {
	w := World{Server: serverPID, Panes: map[string]Pane{}}
	for _, pane := range panes {
		if pane.Window == tab {
			pane.Views = slices.Clone(pane.Views)
			for i := range pane.Views {
				pane.Views[i].WindowActive = tabInFront
			}
		}
		w.Panes[pane.ID] = pane
	}
	return w
}

// line is one row of `list-panes`: what a test says of the pane, its window
// and its session, and then the server's pid, the pane's size, whether its
// window names itself, and no window held from doing so.
func line(fields ...string) string {
	return strings.Join(append(fields, "42", "80", "24", "1", ""), separator)
}

// TestALinkedWindowIsOnePaneSeenTwice: `list-panes -a` prints a pane once per
// session its window is linked into, which is how the tab looks from its
// hidden session and from the one showing it.
func TestALinkedWindowIsOnePaneSeenTwice(t *testing.T) {
	w, err := ParseWorld(strings.Join([]string{
		line("%1", "@9", "1", "0", "list", "", "", "$3", "_agents", "1", "1", "0"),
		line("%1", "@9", "1", "0", "list", "", "", "$1", "home", "4", "1", "1"),
		line("%5", "@9", "0", "0", "", "%2", "$1", "$3", "_agents", "1", "1", "0"),
		line("%5", "@9", "0", "0", "", "%2", "$1", "$1", "home", "4", "1", "1"),
		line("%2", "@1", "1", "0", "stand-in", "", "", "$1", "home", "2", "0", "1"),
	}, "\n"))
	if err != nil {
		t.Fatalf("ParseWorld: %v", err)
	}
	if len(w.Panes) != 3 || len(w.Panes["%1"].Views) != 2 {
		t.Fatalf("panes = %+v", w.Panes)
	}
	if !w.Visible("%1") {
		t.Error("the tab is the current window of a session somebody is on, and is not seen as visible")
	}
	if got := w.Panes["%5"]; got.StandIn != "%2" || got.HomeSession != "$1" {
		t.Errorf("the borrowed pane reads %+v", got)
	}
	if got := w.Panes["%2"]; w.Server != 42 || got.Width != 80 || got.Height != 24 || !got.AutoRename {
		t.Errorf("the server is %d and the stand-in reads %+v", w.Server, got)
	}
	if got := w.Where("%5"); got != "home:2" {
		t.Errorf("a borrowed pane is where its stand-in is: got %q, want home:2", got)
	}
	if got := w.Where("%1"); got != "home:4" {
		t.Errorf("the hidden session is the last choice: got %q, want home:4", got)
	}
}

func TestWhatIsNotRowsIsNotBelieved(t *testing.T) {
	for _, answer := range []string{"", "no server running on /tmp/x", line("%1", "@9")} {
		if _, err := ParseWorld(answer); err == nil {
			t.Errorf("%q was believed", answer)
		}
	}
}

// TestNobodyLookingIsNotVisible: the window being a session's current one is
// not somebody seeing it; the tab's hidden session always says so.
func TestNobodyLookingIsNotVisible(t *testing.T) {
	list := at("%1", tab).as(roleList)
	list.Views[0].Attached = 0
	if world(true, list).Visible("%1") {
		t.Error("a window no terminal is on was seen as visible")
	}
}

// TestAPaneIsOnScreenWhereItLives: in its own window, or in the one its
// stand-in holds its place in; and never for being on the tab.
func TestAPaneIsOnScreenWhereItLives(t *testing.T) {
	w := world(true, theList,
		at("%5", "@1"), at("%6", "@2").onScreen(),
		at("%7", tab).awayFrom("%2"), at("%2", "@3").as(roleStandIn).onScreen(),
		at("%8", tab).awayFrom("%3"), at("%3", "@4").as(roleStandIn),
	)
	for pane, want := range map[string]bool{"%5": false, "%6": true, "%7": true, "%8": false, "%99": false} {
		if got := w.OnScreenAtHome(pane, tab); got != want {
			t.Errorf("OnScreenAtHome(%s) = %v, want %v", pane, got, want)
		}
	}
}

func TestAPlaceThatIsGoneFallsBackToASessionThatIsNot(t *testing.T) {
	w := world(true,
		at("%1", tab).as(roleList).in("$3", tabSession),
		at("%5", "@1"),
		at("%6", "@2").in("$2", "work"),
	)
	for home, want := range map[string]string{"$2": "$2", "$7": "$1", "": "$1"} {
		if got := w.SessionFor(home); got != want {
			t.Errorf("SessionFor(%q) = %q, want %q", home, got, want)
		}
	}
	alone := world(true, at("%1", tab).as(roleList).in("$3", tabSession))
	if got := alone.SessionFor("$7"); got != "$3" {
		t.Errorf("with no other session there is only the tab's: got %q", got)
	}
}
