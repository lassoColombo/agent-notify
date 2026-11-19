package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/lassoColombo/agent-notify/session"
)

// The glyphs under test are ASCII, on purpose: what is being asserted is the
// shape of the plan, and a test that carries private-use codepoints around in
// its own expectations proves nothing about the ones that ship.
var testGlyphs = session.NewPalette(map[session.Kernel]string{
	session.BlockedOnYou:  "!",
	session.Broke:         "x",
	session.FinishedATurn: ">",
	session.Working:       "*",
	session.Idle:          "",
	session.Ended:         "",
}, nil)

var nine = time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)

const home = "/tmp/tmux-501/default"

func placed(name string, kernel session.Kernel, socket string, pane string) session.Record {
	blob, err := json.Marshal(map[string]string{
		serverVariable: socket + ",4242,0",
		paneVariable:   pane,
	})
	if err != nil {
		panic(err)
	}
	return session.Record{
		Key:        session.Key{Host: "mac", Agent: "claude", SessionID: name},
		Name:       name,
		Kernel:     kernel,
		Rank:       kernel.Rank(),
		StateSince: nine,
		CapturedContext: session.CapturedContext{
			CapturedAt: nine,
			By:         map[string]json.RawMessage{Name: blob},
		},
	}
}

// row is a pane of session `home` ($0), in front unless a test says otherwise.
func row(pane, window, paneMark, windowMark string) Row {
	return Row{
		Pane: pane, SessionID: "$0", SessionName: "home", Attached: 1,
		WindowID: window, WindowIndex: number(window), WindowName: "nu",
		WindowActive: true, PaneActive: true,
		PaneMark: paneMark, WindowMark: windowMark,
	}
}

// ran renders a plan as one line per command, which is what the expectations
// below are written against: the argv is exactly what would have been run.
func ran(commands []Command) []string {
	lines := make([]string, 0, len(commands))
	for _, command := range commands {
		lines = append(lines, "tmux -S "+command.Socket+" "+strings.Join(command.Args, " "))
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

func TestAPaneAndItsWindowCarryTheState(t *testing.T) {
	plan := Plan(home,
		[]session.Record{placed("monomodules", session.Working, home, "%3")},
		[]Row{row("%3", "@1", "", ""), row("%0", "@0", "", "")},
		testGlyphs)

	same(t, ran(plan), []string{
		"tmux -S " + home + " set-option -p -t %3 @agent-notify-pane * monomodules",
		"tmux -S " + home + " set-option -w -t @1 @agent-notify-window *",
	})
}

// TestNothingIsRunWhenNothingMoved is the property that makes a render cheap
// enough to do on every change: the plan is the difference, not the state.
func TestNothingIsRunWhenNothingMoved(t *testing.T) {
	plan := Plan(home,
		[]session.Record{placed("monomodules", session.Working, home, "%3")},
		[]Row{row("%3", "@1", "* monomodules", "*")},
		testGlyphs)

	if len(plan) != 0 {
		t.Errorf("planned %v against a tmux that already says exactly that", ran(plan))
	}
}

// TestARestartPaintsOverWhatItFinds: the marks are read, never remembered, so a
// daemon that comes back finds the old state on the options and corrects it.
func TestARestartPaintsOverWhatItFinds(t *testing.T) {
	plan := Plan(home,
		[]session.Record{placed("monomodules", session.BlockedOnYou, home, "%3")},
		[]Row{row("%3", "@1", "* monomodules", "*")},
		testGlyphs)

	same(t, ran(plan), []string{
		"tmux -S " + home + " set-option -p -t %3 @agent-notify-pane ! monomodules",
		"tmux -S " + home + " set-option -w -t @1 @agent-notify-window !",
	})
}

// TestAWindowWearsItsMostUrgentAgent is §A12.1's one rule: max(rank) over the
// panes, the same aggregate an LED over the whole machine computes.
func TestAWindowWearsItsMostUrgentAgent(t *testing.T) {
	plan := Plan(home,
		[]session.Record{
			placed("one", session.Working, home, "%3"),
			placed("two", session.BlockedOnYou, home, "%4"),
			placed("three", session.Idle, home, "%5"),
		},
		[]Row{row("%3", "@1", "", ""), row("%4", "@1", "", ""), row("%5", "@2", "", "")},
		testGlyphs)

	same(t, ran(plan), []string{
		// Most urgent first, because that is the order the records come in.
		"tmux -S " + home + " set-option -p -t %4 @agent-notify-pane ! two",
		"tmux -S " + home + " set-option -p -t %3 @agent-notify-pane * one",
		"tmux -S " + home + " set-option -p -t %5 @agent-notify-pane three",
		"tmux -S " + home + " set-option -w -t @1 @agent-notify-window !",
		// Window 2 holds only an idle agent, whose glyph is deliberately
		// empty, so it gets no mark at all.
	})
}

// TestAnEndedSessionGivesItsPaneBack: giving back is unsetting, and the
// window's mark goes with the last agent in it.
func TestAnEndedSessionGivesItsPaneBack(t *testing.T) {
	plan := Plan(home,
		[]session.Record{placed("monomodules", session.Ended, home, "%3")},
		[]Row{row("%3", "@1", "* monomodules", "*")},
		testGlyphs)

	same(t, ran(plan), []string{
		"tmux -S " + home + " set-option -p -u -t %3 @agent-notify-pane",
		"tmux -S " + home + " set-option -w -u -t @1 @agent-notify-window",
	})
}

// TestAMarkNoRecordRemembersIsStillTakenOff: nobody else writes these options,
// so a mark on a pane no live agent claims is ours and stale, whether or not
// the record that put it there is still in the view.
func TestAMarkNoRecordRemembersIsStillTakenOff(t *testing.T) {
	plan := Plan(home,
		[]session.Record{placed("other", session.Working, home, "%9")},
		[]Row{row("%3", "@1", "! long gone", "!"), row("%9", "@2", "* other", "*")},
		testGlyphs)

	same(t, ran(plan), []string{
		"tmux -S " + home + " set-option -p -u -t %3 @agent-notify-pane",
		"tmux -S " + home + " set-option -w -u -t @1 @agent-notify-window",
	})
}

func TestAPaneNobodyMarkedIsLeftAlone(t *testing.T) {
	plan := Plan(home,
		[]session.Record{placed("monomodules", session.Ended, home, "%3")},
		[]Row{row("%3", "@1", "", "")},
		testGlyphs)

	if len(plan) != 0 {
		t.Errorf("planned %v against a pane this display never touched", ran(plan))
	}
}

func TestASessionOutsideTmuxCostsNothing(t *testing.T) {
	bare := session.Record{
		Key:    session.Key{Host: "mac", Agent: "claude", SessionID: "bare"},
		Kernel: session.Working, Rank: session.RankWorking,
	}
	if grouped := Group([]session.Record{bare}); len(grouped) != 0 {
		t.Errorf("a session with nothing captured was grouped into %v", grouped)
	}
	if plan := Plan(home, []session.Record{bare}, []Row{row("%3", "@1", "", "")}, testGlyphs); len(plan) != 0 {
		t.Errorf("planned %v for a session that is not in tmux", ran(plan))
	}
}

// TestAPaneThatIsGoneIsNotAnOpinion: whether a session is alive is core's
// judgement, made from the agent's process. A display that concluded anything
// from a missing pane would eventually disagree with it (R4).
func TestAPaneThatIsGoneIsNotAnOpinion(t *testing.T) {
	plan := Plan(home,
		[]session.Record{placed("monomodules", session.Working, home, "%42")},
		[]Row{row("%3", "@1", "", "")},
		testGlyphs)

	if len(plan) != 0 {
		t.Errorf("planned %v for a pane that is not there", ran(plan))
	}
}

func TestTwoSessionsInOnePaneGoToTheMoreUrgent(t *testing.T) {
	// A pane that hosted one session and now hosts another — /clear does this
	// — can be claimed twice. Most urgent wins, and only one write is run.
	plan := Plan(home,
		[]session.Record{
			placed("old", session.Working, home, "%3"),
			placed("new", session.BlockedOnYou, home, "%3"),
		},
		[]Row{row("%3", "@1", "", "")},
		testGlyphs)

	same(t, ran(plan), []string{
		"tmux -S " + home + " set-option -p -t %3 @agent-notify-pane ! new",
		"tmux -S " + home + " set-option -w -t @1 @agent-notify-window !",
	})
}

func TestPanesOfAnotherServerAreNotOurs(t *testing.T) {
	plan := Plan(home,
		[]session.Record{placed("elsewhere", session.Working, "/tmp/tmux-501/work", "%3")},
		[]Row{row("%3", "@1", "", "")},
		testGlyphs)

	if len(plan) != 0 {
		t.Errorf("planned %v against a pane id that belongs to a different tmux server", ran(plan))
	}
}

// TestALinkedWindowIsPaintedOnce: a window linked into two sessions is listed
// once for each, and is still one window with one option.
func TestALinkedWindowIsPaintedOnce(t *testing.T) {
	elsewhere := row("%3", "@1", "", "")
	elsewhere.SessionID, elsewhere.SessionName = "$1", "work"
	plan := Plan(home,
		[]session.Record{placed("monomodules", session.Working, home, "%3")},
		[]Row{row("%3", "@1", "", ""), elsewhere},
		testGlyphs)

	same(t, ran(plan), []string{
		"tmux -S " + home + " set-option -p -t %3 @agent-notify-pane * monomodules",
		"tmux -S " + home + " set-option -w -t @1 @agent-notify-window *",
	})
}

func TestTheOrderOfTheRecordsDoesNotChangeThePlan(t *testing.T) {
	records := []session.Record{
		placed("one", session.Idle, home, "%5"),
		placed("two", session.BlockedOnYou, home, "%4"),
		placed("three", session.Working, home, "%3"),
	}
	rows := []Row{row("%3", "@1", "", ""), row("%4", "@1", "", ""), row("%5", "@1", "", "")}

	first := ran(Plan(home, records, rows, testGlyphs))
	shuffled := []session.Record{records[2], records[0], records[1]}
	second := ran(Plan(home, shuffled, rows, testGlyphs))
	same(t, second, first)
}

// TestParseRows needs no tmux: it is the format this program asks for, and the
// answers that are not it.
func TestParseRows(t *testing.T) {
	line := func(fields ...string) string { return strings.Join(fields, separator) }
	good := line("%3", "$0", "home", "1", "@1", "2", "nu", "1", "0", "* alpha", "*") + "\n"

	rows, err := ParseRows([]byte(good))
	if err != nil {
		t.Fatalf("ParseRows: %v", err)
	}
	want := Row{Pane: "%3", SessionID: "$0", SessionName: "home", Attached: 1,
		WindowID: "@1", WindowIndex: 2, WindowName: "nu", WindowActive: true,
		PaneMark: "* alpha", WindowMark: "*"}
	if len(rows) != 1 || rows[0] != want {
		t.Errorf("ParseRows = %+v, want %+v", rows, want)
	}

	for _, refused := range []string{
		"",
		"no server running on /tmp/tmux-501/default\n",
		line("3", "$0", "home", "1", "@1", "2", "nu", "1", "0", "", "") + "\n",
		line("%3", "$0", "home") + "\n",
	} {
		if rows, err := ParseRows([]byte(refused)); err == nil {
			t.Errorf("ParseRows read %+v out of %q", rows, refused)
		}
	}
}
