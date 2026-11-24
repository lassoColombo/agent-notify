package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/lassoColombo/agent-notify/session"
)

var (
	testGlyphs = session.NewPalette(session.DefaultGlyphs, nil)
	testNow    = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
)

// drawn is the list for these records, as plain text.
func drawn(records []session.Record, w World, selected string, width, height int) string {
	sections := Sections(Agents(records, socket, w, "%1"))
	return ansi.Strip(Draw(sections, Background(records), selected, testGlyphs, width, height, testNow))
}

// TestEveryFieldTheListReadsIsOneItWakesFor asks the renderer what it reads
// rather than trusting the list of names (D-72, D-73).
func TestEveryFieldTheListReadsIsOneItWakesFor(t *testing.T) {
	w := world(true, theList, theStandIn, at("%5", "@1"))
	inAPane := placed("alpha", session.Working, socket, "%5")
	inTheBackground := unplaced("beta", session.Working)
	for _, base := range []session.Record{inAPane, inTheBackground} {
		base.Detail = "running-bash"
		base.Cwd = "/Users/somebody/projects/agent-notify"

		missing := session.FieldsRenderedButNotWokenFor(base, WhatToWakeFor(), func(record session.Record) string {
			return drawn([]session.Record{record}, w, "%5", 40, 20)
		})
		if len(missing) > 0 {
			t.Errorf("%s: the list comes out different when %v move, and does not ask to be woken for them",
				base.Name, missing)
		}
	}
}

// TestTheBackgroundIsDrawnBelowAndNeverSelected: each session there wears its
// own state, and none is under the cursor, even with nothing else listed
// (D-128).
func TestTheBackgroundIsDrawnBelowAndNeverSelected(t *testing.T) {
	away := unplaced("away", session.BlockedOnYou)
	away.Cwd = "/Users/somebody/projects/cybergon"
	got := drawn([]session.Record{away, placed("alpha", session.Working, socket, "%5")},
		world(true, theList, at("%5", "@1")), "%5", 40, 20)
	working, background := strings.Index(got, "working"), strings.Index(got, "in background")
	if working < 0 || background < working {
		t.Errorf("the background is not a group of its own below the agents:\n%s", got)
	}
	glyph := testGlyphs.For(session.Record{Kernel: session.BlockedOnYou, Rank: session.BlockedOnYou.Rank()})
	if !strings.Contains(got, "  "+glyph+" away") {
		t.Errorf("a background row does not say its state and its name:\n%s", got)
	}

	alone := drawn([]session.Record{away}, world(true, theList), "", 40, 20)
	if strings.Contains(alone, "▌") || strings.Contains(alone, "no agents") {
		t.Errorf("with nothing else listed, the background is not drawn as it is, unselected:\n%s", alone)
	}
}

// TestARowIsItsNameAndItsWait, on one line, under its state: nothing about
// where it lives or what it said, which `o` and the stage are for (D-132).
func TestARowIsItsNameAndItsWait(t *testing.T) {
	record := placed("alpha", session.BlockedOnYou, socket, "%5")
	record.Cwd = "/Users/somebody/projects/agent-notify"
	record.Message = "Claude needs your permission to use Bash"
	record.StateSince = testNow.Add(-90 * time.Second)
	got := drawn([]session.Record{record}, world(true, theList, at("%5", "@1")), "%5", 40, 20)
	lines := strings.Split(got, "\n")
	if !strings.HasSuffix(strings.TrimSpace(lines[0]), "needs you") || !strings.HasPrefix(lines[1], "▌ alpha") ||
		!strings.HasSuffix(lines[1], " 1m") || strings.TrimSpace(lines[2]) != "" {
		t.Errorf("not a header and then one line for the agent:\n%s", got)
	}
	idle := placed("beta", session.Idle, socket, "%6")
	header := strings.Split(drawn([]session.Record{idle}, world(true, theList, at("%6", "@1")), "%6", 40, 20), "\n")[0]
	if header != " idle" {
		t.Errorf("a header with no glyph reads %q, and is to start one column in, as the others do", header)
	}
	for _, gone := range []string{"home:1", "agent-notify", "permission", " 1\n"} {
		if strings.Contains(got, gone) {
			t.Errorf("the list says %q, which is no longer its to say:\n%s", gone, got)
		}
	}
	if !strings.Contains(got, "q close") {
		t.Errorf("the keys are gone:\n%s", got)
	}
}

// TestATurnThatRunsLongIsMarked: the wait of a working agent turns gold
// once it has gone on longer than a turn usually does.
func TestATurnThatRunsLongIsMarked(t *testing.T) {
	waitOf := func(since time.Duration) string {
		record := placed("alpha", session.Working, socket, "%5")
		record.StateSince = testNow.Add(-since)
		sections := Sections(Agents([]session.Record{record}, socket, world(true, theList, at("%5", "@1")), "%1"))
		return Draw(sections, nil, "", testGlyphs, 40, 10, testNow)
	}
	gold := "\x1b[38;2;246;193;119m"
	if strings.Contains(waitOf(session.StuckAfter-time.Minute), gold) {
		t.Error("a turn shorter than the threshold is marked")
	}
	if !strings.Contains(waitOf(session.StuckAfter+time.Minute), gold) {
		t.Error("a turn longer than the threshold is not marked")
	}
}

// TestNoLineIsWiderThanTheList: a row that wraps tears the list in two.
func TestNoLineIsWiderThanTheList(t *testing.T) {
	record := placed(strings.Repeat("a very long name ", 6), session.Working, socket, "%5")
	record.Cwd = "/" + strings.Repeat("deep", 20)
	for _, line := range strings.Split(drawn([]session.Record{record}, world(true, theList, at("%5", "@1")), "%5", 24, 20), "\n") {
		if width := ansi.StringWidth(line); width > 24 {
			t.Errorf("%q is %d cells wide in a list of 24", line, width)
		}
	}
}

// TestANameCannotDrawOutsideItsRow: a name is the agent's to choose, and an
// escape sequence in one is somebody else's bytes on their way to a terminal.
func TestANameCannotDrawOutsideItsRow(t *testing.T) {
	record := placed("al\x1b[2Jpha\nbeta", session.Working, socket, "%5")
	sections := Sections(Agents([]session.Record{record}, socket, world(true, theList, at("%5", "@1")), "%1"))
	got := Draw(sections, nil, "%5", testGlyphs, 40, 20, testNow)
	if strings.Contains(got, "\x1b[2J") || strings.Count(ansi.Strip(got), "\n") != 19 {
		t.Errorf("the name reached the screen as it was written: %q", got)
	}
}

func TestTheCursorStaysOnScreen(t *testing.T) {
	var records []session.Record
	panes := []Pane{theList}
	for i := range 12 {
		id := fmt.Sprintf("%%%d", 10+i)
		record := placed(fmt.Sprintf("agent-%02d", i), session.Working, socket, id)
		record.StateSince = testNow.Add(-time.Duration(i) * time.Minute)
		records = append(records, record)
		panes = append(panes, at(id, "@1"))
	}
	w := world(true, panes...)
	selected := Flatten(Sections(Agents(records, socket, w, "%1")))[11]
	got := drawn(records, w, selected.Pane, 40, 10)
	if lines := strings.Split(got, "\n"); len(lines) != 10 || !strings.Contains(got, "▌ "+selected.Record.Name) {
		t.Errorf("%d lines, and the last agent is selected:\n%s", len(lines), got)
	}
}

func TestAnEmptyListSaysSo(t *testing.T) {
	if got := drawn(nil, world(true, theList), "", 40, 10); !strings.Contains(got, "no agents") {
		t.Errorf("an empty list draws %q", got)
	}
}

func TestTheStandInSaysWhatItWasTold(t *testing.T) {
	frame := Frame(Showing{Title: "alpha\x07\x1b]0;evil\x07", Lines: []string{"alpha is on screen in home", "o goes there"}}, 60, 11)
	if !strings.Contains(frame, "\x1b]2;alpha") || strings.Contains(frame, "\x07\x1b]0;evil") {
		t.Errorf("the title is not the one it was told, cleaned: %q", frame)
	}
	// 26 cells, centred in 60, on the fifth of eleven rows
	if !strings.Contains(frame, "\x1b[5;18H") || !strings.Contains(ansi.Strip(frame), "alpha is on screen in home") {
		t.Errorf("the first line is not in the middle: %q", frame)
	}
	if !strings.Contains(frame, "\x1b[?25l") {
		t.Errorf("an empty pane shows a cursor unless it is told not to: %q", frame)
	}
	narrow := ansi.Strip(Frame(Showing{Lines: []string{"alpha is on screen in home"}}, 10, 3))
	if strings.Contains(narrow, "in home") {
		t.Errorf("a line wider than the pane was not cut: %q", narrow)
	}
}
