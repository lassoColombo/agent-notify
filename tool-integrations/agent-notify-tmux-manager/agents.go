package main

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/lassoColombo/agent-notify/session"
)

// This file is pure: which sessions the list shows, and in what groups.

// Agent is one row of the list: a live session whose agent runs in a pane of
// this server, and where that pane lives.
type Agent struct {
	Record session.Record
	Pane   string
	// Where is `session:window`, read off tmux when the list was drawn rather
	// than off the record, because a pane can be moved after it was captured.
	Where string
}

// captured is the tmux integration's section of a record: the two variables
// its hook read inside the agent. Read here rather than imported, because
// agent-notify-tmux is a program, not a library.
type captured struct {
	Server string `json:"TMUX"`
	Pane   string `json:"TMUX_PANE"`
}

// placeOf is the server socket and pane a session was captured in.
func placeOf(record session.Record) (socket, pane string, found bool) {
	blob, present := record.CapturedContext.By["tmux"]
	if !present {
		return "", "", false
	}
	var given captured
	if err := json.Unmarshal(blob, &given); err != nil {
		return "", "", false
	}
	socket, found = SocketOf(given.Server)
	if !found || !IsPaneID(given.Pane) {
		return "", "", false
	}
	return socket, given.Pane, true
}

// Agents is every live session in a pane of this server that the stage could
// show, most urgent first. A session whose pane is gone is left out rather
// than guessed about, and so is one that lives in the tab's own window, which
// cannot be borrowed into itself. When two sessions claim one pane, the more
// urgent has it.
func Agents(records []session.Record, socket string, world World, list string) []Agent {
	tab := world.Panes[list].Window
	// Sorted here rather than trusted from the caller, by the one rule every
	// display orders with (R24).
	ordered := slices.Clone(records)
	session.ByUrgency(ordered)

	var agents []Agent
	claimed := map[string]bool{}
	for _, record := range ordered {
		if !record.Kernel.Live() {
			continue
		}
		server, id, found := placeOf(record)
		if !found || server != socket || claimed[id] {
			continue
		}
		pane, exists := world.Panes[id]
		if !exists || pane.Dead || pane.Role != "" || (pane.Window == tab && pane.StandIn == "") {
			continue
		}
		claimed[id] = true
		agents = append(agents, Agent{Record: record, Pane: id, Where: world.Where(id)})
	}
	return agents
}

// Background is every live session that runs in no tmux server and that no
// other integration has placed either, most urgent first: today, the sessions
// Claude runs in its background daemon. A window shows one from whatever pane
// it is in, and nothing says which (D-127), so they have no pane to borrow and
// the list shows them in a group of their own, to be seen and not selected.
//
// PROVISIONAL (D-128), to be revisited. "Outside tmux and placed by nobody"
// stands in for "in the background" only because nothing better is recorded:
// Claude writes nowhere which window shows which background session, and a
// session in a plain terminal that no integration places would land here too.
// Once the window showing a session can be known, the session belongs in the
// groups above, on that window's pane, and this group goes.
func Background(records []session.Record) []session.Record {
	ordered := slices.Clone(records)
	session.ByUrgency(ordered)
	var background []session.Record
	for _, record := range ordered {
		if record.Kernel.Live() && outsideTmux(record) && len(record.DerivedContext) == 0 {
			background = append(background, record)
		}
	}
	return background
}

// outsideTmux is whether the tmux integration's capture ran and found no
// server. A session it never captured for is not known to be outside.
func outsideTmux(record session.Record) bool {
	blob, present := record.CapturedContext.By["tmux"]
	if !present {
		return false
	}
	var given captured
	return json.Unmarshal(blob, &given) == nil && given.Server == ""
}

// Section is one group of the list: the agents in one state.
type Section struct {
	Title  string
	Kernel session.Kernel
	Agents []Agent
}

// titles say a state the way the list does: by what it wants from you.
var titles = map[session.Kernel]string{
	session.BlockedOnYou:  "needs you",
	session.Broke:         "broke",
	session.FinishedATurn: "finished",
	session.Working:       "working",
	session.Idle:          "idle",
}

// Sections groups agents by state, in the order they came in, which is by
// urgency. A state this build has never heard of gets a group like any other,
// placed by the rank its records carry and named as it is spelled (R5, R21).
func Sections(agents []Agent) []Section {
	var sections []Section
	at := map[session.Kernel]int{}
	for _, agent := range agents {
		kernel := agent.Record.Kernel
		i, seen := at[kernel]
		if !seen {
			title, known := titles[kernel]
			if !known {
				title = strings.ReplaceAll(string(kernel), "-", " ")
			}
			i = len(sections)
			at[kernel] = i
			sections = append(sections, Section{Title: title, Kernel: kernel})
		}
		sections[i].Agents = append(sections[i].Agents, agent)
	}
	return sections
}

// Flatten is the sections' agents in the order the list draws them, which is
// the order the cursor moves through.
func Flatten(sections []Section) []Agent {
	var agents []Agent
	for _, section := range sections {
		agents = append(agents, section.Agents...)
	}
	return agents
}
