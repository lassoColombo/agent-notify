package main

import (
	"testing"

	"github.com/lassoColombo/agent-notify/container"
)

// The terminals of the pure tests: three, on two sessions, one of which has
// lost the focus to another application.
var (
	early   = Client{Name: "/dev/ttys001", Session: "$0", Pane: "%1", Activity: 100, Focused: true}
	late    = Client{Name: "/dev/ttys002", Session: "$1", Pane: "%2", Activity: 300}
	middle  = Client{Name: "/dev/ttys003", Session: "$1", Pane: "%3", Activity: 200, Focused: true}
	clients = []Client{early, late, middle}
)

func TestChoose(t *testing.T) {
	for _, given := range []struct {
		why     string
		clients []Client
		asker   Asker
		want    string
	}{
		{"asked from a pane, the terminal showing it, over the session $TMUX names",
			clients, Asker{Pane: "%1", Session: "$1"}, early.Name},
		{"asked from a popup, the terminal on its session that was used last",
			clients, Asker{Session: "$1"}, late.Name},
		{"asked from inside, but from nothing a terminal shows: as from outside",
			clients, Asker{Pane: "%9", Session: "$9"}, middle.Name},
		{"asked from outside, the terminal with the focus that was used last",
			clients, Asker{}, middle.Name},
		{"asked from outside with nothing focused, the terminal used last",
			[]Client{{Name: "a", Activity: 1}, {Name: "b", Activity: 2}}, Asker{}, "b"},
		{"no terminal at all", nil, Asker{Session: "$0"}, ""},
	} {
		got, found := Choose(given.clients, given.asker)
		if found != (given.want != "") || got.Name != given.want {
			t.Errorf("%s: chose %q (%v), want %q", given.why, got.Name, found, given.want)
		}
	}
}

func TestThrough(t *testing.T) {
	agents := View{Session: "$2", Window: "@3"}
	mine := View{Session: "$0", Window: "@3", LastAttached: 100}
	theirs := View{Session: "$1", Window: "@3", LastAttached: 300}
	for _, given := range []struct {
		why    string
		views  []View
		client Client
		want   string
	}{
		{"the terminal's own session, when it holds the window",
			[]View{agents, theirs, mine}, Client{Session: "$0"}, "$0"},
		{"else the one a terminal was attached to last",
			[]View{agents, mine, theirs}, Client{Session: "$9"}, "$1"},
		{"and one nobody was ever attached to only when there is no other",
			[]View{agents}, Client{Session: "$9"}, "$2"},
	} {
		if got, found := Through(given.views, given.client); !found || got.Session != given.want {
			t.Errorf("%s: %+v (%v), want %s", given.why, got, found, given.want)
		}
	}
}

func TestVerdict(t *testing.T) {
	panes := map[string]Mark{"%1": {}, "%2": {}, "%3": {}, "%4": {}}
	for _, given := range []struct {
		why    string
		server Server
		pane   string
		want   container.Answer
	}{
		{"shown by a terminal with the focus", Server{PID: pid, Panes: panes, Clients: clients}, "%1", container.Yes},
		{"shown only by a terminal without it", Server{PID: pid, Panes: panes, Clients: clients}, "%2", container.No},
		{"shown by two, one with the focus",
			Server{PID: pid, Panes: panes, Clients: []Client{late, {Name: "x", Pane: "%2", Focused: true}}}, "%2", container.Yes},
		{"shown by nobody", Server{PID: pid, Panes: panes, Clients: clients}, "%4", container.No},
		{"not there", Server{PID: pid, Panes: panes, Clients: clients}, "%9", container.No},
		{"in the server that was on this socket before", Server{PID: pid + 1, Panes: panes, Clients: clients}, "%1", container.No},
	} {
		got := Verdict(given.server, Place{Socket: home, Server: pid, Pane: given.pane})
		if got.Answer != given.want {
			t.Errorf("%s: %+v, want %s", given.why, got, given.want)
		}
		if got.Answer == container.No && got.Detail == "" {
			t.Errorf("%s: a no that does not say why", given.why)
		}
	}
}
