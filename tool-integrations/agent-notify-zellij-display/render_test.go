package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	agentnotify "github.com/lassoColombo/agent-notify"
)

// The glyphs under test are ASCII, on purpose: what is being asserted is the
// shape of the plan, and a test that carries private-use codepoints around in
// its own expectations proves nothing about the ones that ship.
var testGlyphs = agentnotify.NewPalette(map[agentnotify.Kernel]string{
	agentnotify.BlockedOnYou:  "!",
	agentnotify.Broke:         "x",
	agentnotify.FinishedATurn: ">",
	agentnotify.Working:       "*",
	agentnotify.Idle:          "",
	agentnotify.Ended:         "",
}, nil)

var nine = time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)

func placed(name string, kernel agentnotify.Kernel, zellijSession string, pane int) agentnotify.Record {
	blob, err := json.Marshal(map[string]string{
		sessionVariable: zellijSession,
		paneVariable:    fmt.Sprint(pane),
	})
	if err != nil {
		panic(err)
	}
	return agentnotify.Record{
		Key:        agentnotify.Key{Host: "mac", Agent: "claude", SessionID: name},
		Name:       name,
		Kernel:     kernel,
		Rank:       kernel.Rank(),
		StateSince: nine,
		CapturedContext: agentnotify.CapturedContext{
			CapturedAt: nine,
			By:         map[string]json.RawMessage{Name: blob},
		},
	}
}

func pane(id int, title string, tab int, tabName string) Pane {
	return Pane{ID: id, Title: title, TabID: tab, TabName: tabName}
}

// ran renders a plan as one line per command, which is what the expectations
// below are written against: a list of commands compared field by field would
// be unreadable, and the argv is exactly what would have been run.
func ran(commands []Command) []string {
	lines := make([]string, 0, len(commands))
	for _, command := range commands {
		lines = append(lines, "zellij --session "+command.Session+" "+strings.Join(command.Args, " "))
	}
	return lines
}

func same(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("planned %d command(s), want %d:\n got: %s\nwant: %s",
			len(got), len(want), strings.Join(got, "\n      "), strings.Join(want, "\n      "))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("command %d:\n got: %s\nwant: %s", i, got[i], want[i])
		}
	}
}

func TestAPaneAndItsTabCarryTheState(t *testing.T) {
	plan := Plan("home",
		[]agentnotify.Record{placed("monomodules", agentnotify.Working, "home", 3)},
		[]Pane{pane(3, "~", 1, "root"), pane(0, "notes", 0, "notes")},
		testGlyphs)

	same(t, ran(plan), []string{
		"zellij --session home action rename-pane --pane-id terminal_3 * monomodules",
		"zellij --session home action rename-tab-by-id 1 * root",
	})
}

// TestNothingIsRunWhenNothingMoved is the property that makes a render cheap
// enough to do on every change: the plan is the difference, not the state.
func TestNothingIsRunWhenNothingMoved(t *testing.T) {
	plan := Plan("home",
		[]agentnotify.Record{placed("monomodules", agentnotify.Working, "home", 3)},
		[]Pane{pane(3, "* monomodules", 1, "* root")},
		testGlyphs)

	if len(plan) != 0 {
		t.Errorf("planned %v against a zellij that already says exactly that", ran(plan))
	}
}

// TestARestartDoesNotStackGlyphs: the tab's name is the user's and is recovered
// from what it says now, so a daemon that comes back finds its own glyph, takes
// it off, and puts the current one on — rather than adding a second.
func TestARestartDoesNotStackGlyphs(t *testing.T) {
	plan := Plan("home",
		[]agentnotify.Record{placed("monomodules", agentnotify.BlockedOnYou, "home", 3)},
		[]Pane{pane(3, "* monomodules", 1, "* root")},
		testGlyphs)

	same(t, ran(plan), []string{
		"zellij --session home action rename-pane --pane-id terminal_3 ! monomodules",
		"zellij --session home action rename-tab-by-id 1 ! root",
	})
}

// TestATabWearsItsMostUrgentAgent is §A12.1's one rule: max(rank) over the
// panes, the same aggregate an LED over the whole machine computes.
func TestATabWearsItsMostUrgentAgent(t *testing.T) {
	plan := Plan("home",
		[]agentnotify.Record{
			placed("one", agentnotify.Working, "home", 3),
			placed("two", agentnotify.BlockedOnYou, "home", 4),
			placed("three", agentnotify.Idle, "home", 5),
		},
		[]Pane{pane(3, "~", 1, "root"), pane(4, "~", 1, "root"), pane(5, "~", 2, "other")},
		testGlyphs)

	same(t, ran(plan), []string{
		// Most urgent first, because that is the order the records come in.
		"zellij --session home action rename-pane --pane-id terminal_4 ! two",
		"zellij --session home action rename-pane --pane-id terminal_3 * one",
		"zellij --session home action rename-pane --pane-id terminal_5 three",
		"zellij --session home action rename-tab-by-id 1 ! root",
		// Tab 2 holds only an idle agent, whose glyph is deliberately empty,
		// so the tab is left saying exactly what it said.
	})
}

// TestAnEndedSessionGivesItsPaneBack is why this display asks for ended
// sessions it will never draw: it owns a piece of somebody else's UI and the
// record is the only thing that remembers which piece.
func TestAnEndedSessionGivesItsPaneBack(t *testing.T) {
	gone := placed("monomodules", agentnotify.Ended, "home", 3)
	plan := Plan("home",
		[]agentnotify.Record{gone},
		[]Pane{pane(3, "* monomodules", 1, "* root")},
		testGlyphs)

	same(t, ran(plan), []string{
		"zellij --session home action undo-rename-pane --pane-id terminal_3",
		"zellij --session home action rename-tab-by-id 1 root",
	})
}

// TestAPaneNobodyPaintedIsLeftAlone: giving a pane back must never clobber a
// name somebody else chose.
func TestAPaneNobodyPaintedIsLeftAlone(t *testing.T) {
	plan := Plan("home",
		[]agentnotify.Record{placed("monomodules", agentnotify.Ended, "home", 3)},
		[]Pane{pane(3, "my notes", 1, "root")},
		testGlyphs)

	if len(plan) != 0 {
		t.Errorf("planned %v against a pane this display never touched", ran(plan))
	}
}

// TestAnIdlePaneIsStillRecognisedAsOurs covers the states whose glyph is empty
// on purpose: there is no mark to recognise, so the name is the evidence.
func TestAnIdlePaneIsStillRecognisedAsOurs(t *testing.T) {
	plan := Plan("home",
		[]agentnotify.Record{placed("monomodules", agentnotify.Ended, "home", 3)},
		[]Pane{pane(3, "monomodules", 1, "root")},
		testGlyphs)

	same(t, ran(plan), []string{
		"zellij --session home action undo-rename-pane --pane-id terminal_3",
	})
}

// TestATabRenamedByHandKeepsItsNewName: the name is the user's, and recovering
// it from what the tab says now — rather than remembering what we wrote — is
// what makes that true.
func TestATabRenamedByHandKeepsItsNewName(t *testing.T) {
	plan := Plan("home",
		[]agentnotify.Record{placed("monomodules", agentnotify.Working, "home", 3)},
		[]Pane{pane(3, "* monomodules", 1, "* the interesting one")},
		testGlyphs)

	if len(plan) != 0 {
		t.Errorf("planned %v; the tab already says the right thing under a name we did not choose",
			ran(plan))
	}
}

func TestATabWithNothingLeftOnItGoesBackToItsOwnName(t *testing.T) {
	// Nothing of ours in this zellij session any more, and a tab still wearing
	// a glyph: the glyph comes off and the user's name stays.
	plan := Plan("home",
		[]agentnotify.Record{placed("monomodules", agentnotify.Ended, "home", 9)},
		[]Pane{pane(3, "~", 1, "! root")},
		testGlyphs)

	same(t, ran(plan), []string{
		"zellij --session home action rename-tab-by-id 1 root",
	})
}

func TestATabWhoseNameWasOnlyEverAGlyphIsHandedBack(t *testing.T) {
	plan := Plan("home",
		[]agentnotify.Record{placed("monomodules", agentnotify.Ended, "home", 9)},
		[]Pane{pane(3, "~", 1, "!")},
		testGlyphs)

	same(t, ran(plan), []string{
		"zellij --session home action undo-rename-tab --tab-id 1",
	})
}

func TestASessionOutsideZellijCostsNothing(t *testing.T) {
	bare := agentnotify.Record{
		Key:    agentnotify.Key{Host: "mac", Agent: "claude", SessionID: "bare"},
		Kernel: agentnotify.Working, Rank: agentnotify.RankWorking,
	}
	if grouped := Group([]agentnotify.Record{bare}); len(grouped) != 0 {
		t.Errorf("a session with nothing captured was grouped into %v", grouped)
	}
	if plan := Plan("home", []agentnotify.Record{bare}, []Pane{pane(3, "~", 1, "root")}, testGlyphs); len(plan) != 0 {
		t.Errorf("planned %v for a session that is not in zellij", ran(plan))
	}
}

// TestAPaneThatIsGoneIsNotAnOpinion: whether a session is alive is core's
// judgement, made from the agent's process. A display that concluded anything
// from a missing pane would eventually disagree with it (R4).
func TestAPaneThatIsGoneIsNotAnOpinion(t *testing.T) {
	plan := Plan("home",
		[]agentnotify.Record{placed("monomodules", agentnotify.Working, "home", 42)},
		[]Pane{pane(3, "~", 1, "root")},
		testGlyphs)

	if len(plan) != 0 {
		t.Errorf("planned %v for a pane that is not there", ran(plan))
	}
}

func TestTwoSessionsInOnePaneGoToTheMoreUrgent(t *testing.T) {
	// A pane that hosted one session and now hosts another — /clear does this
	// — can be claimed twice. Most urgent wins, and only one rename is run.
	plan := Plan("home",
		[]agentnotify.Record{
			placed("old", agentnotify.Working, "home", 3),
			placed("new", agentnotify.BlockedOnYou, "home", 3),
		},
		[]Pane{pane(3, "~", 1, "root")},
		testGlyphs)

	same(t, ran(plan), []string{
		"zellij --session home action rename-pane --pane-id terminal_3 ! new",
		"zellij --session home action rename-tab-by-id 1 ! root",
	})
}

func TestPanesInAnotherZellijSessionAreNotOurs(t *testing.T) {
	plan := Plan("home",
		[]agentnotify.Record{placed("elsewhere", agentnotify.Working, "work", 3)},
		[]Pane{pane(3, "~", 1, "root")},
		testGlyphs)

	if len(plan) != 0 {
		t.Errorf("planned %v against a pane id that belongs to a different zellij session", ran(plan))
	}
}

func TestPluginPanesAreNotPanes(t *testing.T) {
	// Pane ids are unique per kind, so plugin_3 and terminal_3 both exist and
	// only one of them is a place an agent can be.
	plan := Plan("home",
		[]agentnotify.Record{placed("monomodules", agentnotify.Working, "home", 3)},
		[]Pane{{ID: 3, Plugin: true, Title: "zellij:tab-bar", TabID: 1, TabName: "root"}},
		testGlyphs)

	if len(plan) != 0 {
		t.Errorf("planned %v against a plugin pane", ran(plan))
	}
}

func TestTheOrderOfTheRecordsDoesNotChangeThePlan(t *testing.T) {
	records := []agentnotify.Record{
		placed("one", agentnotify.Idle, "home", 5),
		placed("two", agentnotify.BlockedOnYou, "home", 4),
		placed("three", agentnotify.Working, "home", 3),
	}
	panes := []Pane{pane(3, "~", 1, "root"), pane(4, "~", 1, "root"), pane(5, "~", 1, "root")}

	first := ran(Plan("home", records, panes, testGlyphs))
	shuffled := []agentnotify.Record{records[2], records[0], records[1]}
	second := ran(Plan("home", shuffled, panes, testGlyphs))
	same(t, second, first)
}
