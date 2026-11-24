package main

import (
	"encoding/json"
	"testing"

	"github.com/lassoColombo/agent-notify/session"
)

const socket = "/tmp/tmux-501/default"

func placed(name string, kernel session.Kernel, socket, pane string) session.Record {
	blob, _ := json.Marshal(captured{Server: socket + ",42,0", Pane: pane})
	return session.Record{
		Name:            name,
		Kernel:          kernel,
		Rank:            kernel.Rank(),
		CapturedContext: session.CapturedContext{By: map[string]json.RawMessage{"tmux": blob}},
	}
}

// unplaced is a session whose tmux capture ran and found no server, as one in
// Claude's background daemon is captured.
func unplaced(name string, kernel session.Kernel) session.Record {
	blob, _ := json.Marshal(captured{})
	return session.Record{
		Name:            name,
		Kernel:          kernel,
		Rank:            kernel.Rank(),
		CapturedContext: session.CapturedContext{By: map[string]json.RawMessage{"tmux": blob}},
	}
}

func names(agents []Agent) []string {
	var names []string
	for _, agent := range agents {
		names = append(names, agent.Record.Name)
	}
	return names
}

func TestOnlyWhatTheStageCouldShowIsListed(t *testing.T) {
	records := []session.Record{
		placed("here", session.Working, socket, "%5"),
		placed("on the stage", session.Working, socket, "%6"),
		placed("elsewhere", session.Working, "/tmp/tmux-501/other", "%5"),
		placed("ended", session.Ended, socket, "%7"),
		placed("gone", session.Working, socket, "%99"),
		placed("in the tab", session.Working, socket, "%8"),
		placed("the list", session.Working, socket, "%1"),
		{Name: "not in tmux", Kernel: session.Working},
	}
	w := world(true, theList,
		at("%5", "@1"), at("%7", "@1"), at("%8", tab),
		at("%6", tab).awayFrom("%2"), at("%2", "@3").as(roleStandIn),
	)
	agents := Agents(records, socket, w, "%1")
	if got := names(agents); len(got) != 2 || got[0] != "here" || got[1] != "on the stage" {
		t.Fatalf("agents = %v, want `here` and `on the stage`", got)
	}
}

// TestAServerStartedAgainIsAnotherServer: the socket is the same and the pane
// ids start again from %0, so a record of the old server names a pane that is
// now somebody else's (D-126).
func TestAServerStartedAgainIsAnotherServer(t *testing.T) {
	w := world(true, theList, at("%5", "@1"))
	w.Server = 4242
	if got := names(Agents([]session.Record{placed("before", session.Working, socket, "%5")}, socket, w, "%1")); len(got) != 0 {
		t.Errorf("agents = %v; the record is of the server that was there before", got)
	}
}

func TestTheMoreUrgentSessionKeepsAContestedPane(t *testing.T) {
	records := []session.Record{
		placed("stale", session.Idle, socket, "%5"),
		placed("blocked", session.BlockedOnYou, socket, "%5"),
	}
	agents := Agents(records, socket, world(true, theList, at("%5", "@1")), "%1")
	if got := names(agents); len(got) != 1 || got[0] != "blocked" {
		t.Errorf("agents = %v", got)
	}
}

// TestSectionsGoMostUrgentFirst, and a state this build has never heard of is
// placed by the rank it came with rather than dropped (R5, R21).
func TestSectionsGoMostUrgentFirst(t *testing.T) {
	pondering := placed("p", session.Kernel("pondering-deeply"), socket, "%8")
	pondering.Rank = 35
	records := []session.Record{
		placed("w", session.Working, socket, "%5"),
		pondering,
		placed("b", session.BlockedOnYou, socket, "%6"),
		placed("w2", session.Working, socket, "%7"),
	}
	w := world(true, theList, at("%5", "@1"), at("%6", "@1"), at("%7", "@1"), at("%8", "@1"))
	sections := Sections(Agents(records, socket, w, "%1"))
	var titles []string
	for _, section := range sections {
		titles = append(titles, section.Title)
	}
	if len(titles) != 3 || titles[0] != "needs you" || titles[1] != "pondering deeply" || titles[2] != "working" {
		t.Errorf("sections = %v", titles)
	}
	if got := names(Flatten(sections)); got[0] != "b" || got[1] != "p" || len(got) != 4 {
		t.Errorf("order = %v", got)
	}
}

// TestTheBackgroundIsWhatNoPaneHolds: a session is in the background when
// tmux's capture found no server and nothing else placed it. One never
// captured for is not known to be anywhere, and is left out (D-128).
func TestTheBackgroundIsWhatNoPaneHolds(t *testing.T) {
	inAerospace := unplaced("in aerospace", session.Working)
	inAerospace.DerivedContext = map[string]json.RawMessage{"aerospace-container": json.RawMessage(`{"title":"home | "}`)}
	records := []session.Record{
		unplaced("idle", session.Idle),
		unplaced("blocked", session.BlockedOnYou),
		unplaced("ended", session.Ended),
		inAerospace,
		placed("here", session.Working, socket, "%5"),
		placed("elsewhere", session.Working, "/tmp/tmux-501/other", "%5"),
		{Name: "never captured", Kernel: session.Working},
	}
	var got []string
	for _, record := range Background(records) {
		got = append(got, record.Name)
	}
	if len(got) != 2 || got[0] != "blocked" || got[1] != "idle" {
		t.Errorf("background = %v, want `blocked` then `idle`", got)
	}
}
