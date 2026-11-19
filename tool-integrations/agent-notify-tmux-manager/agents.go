package main

import (
	"encoding/json"
	"strings"

	"github.com/lassoColombo/agent-notify/session"
)

// Agent is one row of the list: a live session whose agent runs in a pane of
// this server, and where that pane is now.
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
func placeOf(record session.Record) (string, string, bool) {
	blob, present := record.CapturedContext.By["tmux"]
	if !present {
		return "", "", false
	}
	var given captured
	if err := json.Unmarshal(blob, &given); err != nil {
		return "", "", false
	}
	socket, found := SocketOf(given.Server)
	if !found || !IsPaneID(given.Pane) {
		return "", "", false
	}
	return socket, given.Pane, true
}

// Agents is every live session in a pane of this server that still exists,
// in the order the records came in (most urgent first). `panes` is the
// server's panes and where each one is; a session whose pane is gone is left
// out rather than guessed about, and so is anything in `own`, the tab's own
// two panes. When two sessions claim one pane, the more urgent one has it.
func Agents(records []session.Record, socket string, panes map[string]string, own map[string]bool) []Agent {
	var agents []Agent
	claimed := map[string]bool{}
	for _, record := range records {
		if !record.Kernel.Live() {
			continue
		}
		server, pane, found := placeOf(record)
		if !found || server != socket || own[pane] || claimed[pane] {
			continue
		}
		where, exists := panes[pane]
		if !exists {
			continue
		}
		claimed[pane] = true
		agents = append(agents, Agent{Record: record, Pane: pane, Where: where})
	}
	return agents
}

// Section is one group of the list.
type Section struct {
	Title  string
	Kernel session.Kernel
	Agents []Agent
}

// order is the list's sections, top to bottom: what costs you most to leave
// waiting first, which is core's own urgency order.
var order = []struct {
	kernel session.Kernel
	title  string
}{
	{session.BlockedOnYou, "needs you"},
	{session.Broke, "broke"},
	{session.FinishedATurn, "finished"},
	{session.Working, "working"},
	{session.Idle, "idle"},
}

// Sections groups agents by state, in `order`, leaving out empty groups. A
// state this build does not know still gets a group, at the bottom, rather
// than disappearing (R5).
func Sections(agents []Agent) []Section {
	var sections []Section
	known := map[session.Kernel]bool{}
	for _, one := range order {
		known[one.kernel] = true
		section := Section{Title: one.title, Kernel: one.kernel}
		for _, agent := range agents {
			if agent.Record.Kernel == one.kernel {
				section.Agents = append(section.Agents, agent)
			}
		}
		if len(section.Agents) > 0 {
			sections = append(sections, section)
		}
	}
	other := Section{Title: "other"}
	for _, agent := range agents {
		if !known[agent.Record.Kernel] {
			other.Agents = append(other.Agents, agent)
		}
	}
	if len(other.Agents) > 0 {
		sections = append(sections, other)
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

// PanesOf parses `list-panes -a -F "#{pane_id}<sep>#{session_name}:#{window_index}"`.
func PanesOf(output string) map[string]string {
	panes := map[string]string{}
	for _, line := range strings.Split(trimmed(output), "\n") {
		pane, where, found := strings.Cut(line, separator)
		if found && IsPaneID(pane) {
			panes[pane] = where
		}
	}
	return panes
}
