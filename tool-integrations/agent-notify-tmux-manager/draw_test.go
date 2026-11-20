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
	testGlyphs = session.NewPalette(DefaultGlyphs, nil)
	testNow    = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
)

// drawn is the list for these records, as plain text.
func drawn(records []session.Record, w World, selected string, width, height int) string {
	sections := Sections(Agents(records, socket, w, "%1"))
	return ansi.Strip(Draw(sections, selected, testGlyphs, width, height, testNow))
}

// TestEveryFieldTheListReadsIsOneItWakesFor asks the renderer what it reads
// rather than trusting the list of names (D-72, D-73).
func TestEveryFieldTheListReadsIsOneItWakesFor(t *testing.T) {
	w := world(true, theList, theStandIn, at("%5", "@1"))
	base := placed("alpha", session.Working, socket, "%5")
	base.Detail = "running-bash"
	base.Cwd = "/Users/somebody/projects/agent-notify"

	missing := session.FieldsRenderedButNotWokenFor(base, WhatToWakeFor(), func(record session.Record) string {
		return drawn([]session.Record{record}, w, "%5", 40, 20)
	})
	if len(missing) > 0 {
		t.Errorf("the list comes out different when %v move, and does not ask to be woken for them", missing)
	}
}

func TestARowIsItsNameItsAgeAndWhereItLives(t *testing.T) {
	record := placed("alpha", session.BlockedOnYou, socket, "%5")
	record.Cwd = "/Users/somebody/projects/agent-notify"
	record.StateSince = testNow.Add(-90 * time.Second)
	got := drawn([]session.Record{record}, world(true, theList, at("%5", "@1")), "%5", 40, 20)
	for _, part := range []string{"needs you 1", "▌ alpha", "1m", "home:1  agent-notify", "q close"} {
		if !strings.Contains(got, part) {
			t.Errorf("the list does not say %q:\n%s", part, got)
		}
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
	got := Draw(sections, "%5", testGlyphs, 40, 20, testNow)
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
	frame := Frame(Showing{Title: "alpha\x07\x1b]0;evil\x07", Lines: []string{"alpha is in the agents tab", "Enter brings it back here"}}, 60, 11)
	if !strings.Contains(frame, "\x1b]2;alpha") || strings.Contains(frame, "\x07\x1b]0;evil") {
		t.Errorf("the title is not the one it was told, cleaned: %q", frame)
	}
	// 26 cells, centred in 60, on the fifth of eleven rows
	if !strings.Contains(frame, "\x1b[5;18H") || !strings.Contains(ansi.Strip(frame), "alpha is in the agents tab") {
		t.Errorf("the first line is not in the middle: %q", frame)
	}
	narrow := ansi.Strip(Frame(Showing{Lines: []string{"alpha is in the agents tab"}}, 10, 3))
	if strings.Contains(narrow, "agents tab") {
		t.Errorf("a line wider than the pane was not cut: %q", narrow)
	}
}
