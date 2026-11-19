package main

import (
	"encoding/json"
	"testing"

	"github.com/lassoColombo/agent-notify/session"
)

const socket = "/tmp/tmux-501/default"

func placed(name string, kernel session.Kernel, server, pane string) session.Record {
	blob, _ := json.Marshal(captured{Server: server + ",42,0", Pane: pane})
	return session.Record{
		Name:            name,
		Kernel:          kernel,
		CapturedContext: session.CapturedContext{By: map[string]json.RawMessage{"tmux": blob}},
	}
}

func TestOnlyThisServersLivePanesAreListed(t *testing.T) {
	records := []session.Record{
		placed("here", session.Working, socket, "%1"),
		placed("elsewhere", session.Working, "/tmp/tmux-501/other", "%1"),
		placed("ended", session.Ended, socket, "%2"),
		placed("gone", session.Working, socket, "%9"),
		placed("the tab", session.Working, socket, "%5"),
		{Name: "zellij", Kernel: session.Working},
	}
	panes := map[string]string{"%1": "home:1", "%2": "home:2", "%5": "home:3"}
	agents := Agents(records, socket, panes, map[string]bool{"%5": true})
	if len(agents) != 1 || agents[0].Record.Name != "here" || agents[0].Where != "home:1" {
		t.Errorf("agents = %+v, want only `here`, at home:1", agents)
	}
}

func TestTheMoreUrgentSessionKeepsAContestedPane(t *testing.T) {
	records := []session.Record{
		placed("blocked", session.BlockedOnYou, socket, "%1"),
		placed("stale", session.Idle, socket, "%1"),
	}
	agents := Agents(records, socket, map[string]string{"%1": "home:1"}, nil)
	if len(agents) != 1 || agents[0].Record.Name != "blocked" {
		t.Errorf("agents = %+v", agents)
	}
}

func TestSectionsGoMostUrgentFirstAndSkipEmptyOnes(t *testing.T) {
	agents := []Agent{
		{Record: session.Record{Name: "w", Kernel: session.Working}},
		{Record: session.Record{Name: "b", Kernel: session.BlockedOnYou}},
		{Record: session.Record{Name: "?", Kernel: session.Kernel("pondering")}},
		{Record: session.Record{Name: "w2", Kernel: session.Working}},
	}
	sections := Sections(agents)
	var titles []string
	for _, section := range sections {
		titles = append(titles, section.Title)
	}
	if len(titles) != 3 || titles[0] != "needs you" || titles[1] != "working" || titles[2] != "other" {
		t.Errorf("sections = %v", titles)
	}
	flat := Flatten(sections)
	if flat[0].Record.Name != "b" || flat[1].Record.Name != "w" || flat[2].Record.Name != "w2" {
		t.Errorf("order = %v", flat)
	}
}

func TestPanesOf(t *testing.T) {
	panes := PanesOf("%1" + separator + "home:1\n%12" + separator + "work:3\n")
	if panes["%1"] != "home:1" || panes["%12"] != "work:3" || len(panes) != 2 {
		t.Errorf("panes = %v", panes)
	}
}
