package main

import (
	"strings"
	"testing"

	"github.com/lassoColombo/agent-notify/session"
)

func TestWrapBreaksOnSpaces(t *testing.T) {
	got := wrap("one two three four five six seven", 12)
	want := []string{"one two", "three four", "five six", "seven"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("wrap = %v, want %v", got, want)
	}
}

// TestWrapKeepsTheAgentsOwnLines: a newline is the difference between a list
// and a paragraph, and a blank line is the only paragraph break there is room
// for.
func TestWrapKeepsTheAgentsOwnLines(t *testing.T) {
	got := wrap("first\n\n- one\n- two", 40)
	want := []string{"first", "", "- one", "- two"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("wrap = %v, want %v", got, want)
	}
}

// TestAWordLongerThanTheLineIsCutRatherThanPushing. A path, a hash or a URL is
// what this is really about, and every one of them turns up in what an agent
// says.
func TestAWordLongerThanTheLineIsCutRatherThanPushing(t *testing.T) {
	for _, line := range wrap("/Users/somebody/projects/personal/agent-notify/tool-integrations", 20) {
		if len([]rune(line)) > 20 {
			t.Errorf("%q is %d wide, want at most 20", line, len([]rune(line)))
		}
	}
}

func TestWrapCountsRunesNotBytes(t *testing.T) {
	for _, line := range wrap(strings.Repeat("é", 50), 10) {
		if len([]rune(line)) > 10 {
			t.Errorf("%q is %d runes wide", line, len([]rune(line)))
		}
	}
}

func TestPreviewSettingsThatCannotWork(t *testing.T) {
	for _, each := range []struct {
		preview Preview
		says    string
	}{
		{Preview{Lines: 0, Width: 40}, "lines"},
		{Preview{Lines: 4, Width: 4}, "width"},
	} {
		if problem := each.preview.sane(); !strings.Contains(problem, each.says) {
			t.Errorf("%+v was accepted, or complained about the wrong thing: %q", each.preview, problem)
		}
	}
}

// TestTheNotificationBodyIsWhatTheAgentSaid, wrapped and capped, with the cut
// marked — it is the only place an agent's words are shown now.
func TestTheNotificationBodyIsWhatTheAgentSaid(t *testing.T) {
	record := aSession("alpha", session.Working, nine)
	record.Message = strings.Repeat("word ", 200)

	body := strings.Split(Preview{Lines: 3, Width: 20}.Said(record), "\n")
	if len(body) != 3 {
		t.Fatalf("the body is %d lines, want 3", len(body))
	}
	if !strings.HasSuffix(body[2], "…") {
		t.Errorf("the last line does not say it was cut: %q", body[2])
	}
}

func TestAnAgentThatHasSaidNothingHasAnEmptyBody(t *testing.T) {
	if got := (Preview{Lines: 4, Width: 30}).Said(aSession("alpha", session.Working, nine)); got != "" {
		t.Errorf("Said = %q", got)
	}
}

// TestStateIsHowAPersonWouldSayIt, which is what a notification's subtitle uses.
func TestStateIsHowAPersonWouldSayIt(t *testing.T) {
	record := aSession("alpha", session.BlockedOnYou, nine)
	record.Detail = "permission-prompt"
	if got := State(record); got != "blocked on you/permission prompt" {
		t.Errorf("State = %q", got)
	}
}
