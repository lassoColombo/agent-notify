package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The lines below are copies of real ones. [captured 2026-09-22, 2.1.267]
const (
	realTitle = `{"type":"ai-title","aiTitle":"Fix configuration resetting tab name","sessionId":"c1dd7433-1750-4655-88e7-67c5d039b974"}`
	// realTypedPrompt is what a person asked for, which is not a name and is
	// deliberately not read (D-75).
	realTypedPrompt = `{"type":"user","isSidechain":false,"userType":"external","message":{"role":"user","content":"the zellij display integration is not working correctly: tabs and panes are not getting renamed"}}`
	realToolResult  = `{"type":"user","message":{"role":"user","content":[{"tool_use_id":"toolu_01","type":"tool_result","content":"ok"}]}}`
	realAssistant   = `{"type":"assistant","requestId":"req_011","message":{"model":"claude-opus-5","usage":{"input_tokens":2,"output_tokens":366}}}`
)

// TestTheTitleIsReadOutOfTheHead is the ordinary head of a titled session.
func TestTheTitleIsReadOutOfTheHead(t *testing.T) {
	title := WhatClaudeTitlesThisSession(transcript(t,
		realTypedPrompt, realAssistant, realTitle, realToolResult))
	if title != "Fix configuration resetting tab name" {
		t.Errorf("title = %q, want Claude's own", title)
	}
}

// TestASessionClaudeHasNotTitledReportsNothing is every session on this machine
// today, and the whole point of the change that produced it: an empty name is
// how core learns that nobody has named this one, and core names it after its
// directory rather than this program inventing something (D-75).
func TestASessionClaudeHasNotTitledReportsNothing(t *testing.T) {
	for _, one := range []struct{ what, line string }{
		{"the prompt that says exactly what it is about", realTypedPrompt},
		{"a tool result", realToolResult},
		{"an assistant turn", realAssistant},
		{"a line Claude was still writing", `{"type":"ai-ti`},
		{"a title with nothing in it", `{"type":"ai-title","aiTitle":"   "}`},
		{"something else carrying the words", `{"type":"user","message":{"role":"user","content":"grep ai-title"}}`},
	} {
		t.Run(one.what, func(t *testing.T) {
			if got := WhatClaudeTitlesThisSession(transcript(t, one.line)); got != "" {
				t.Errorf("read %q as a title", got)
			}
			if got := WhatClaudeTitlesThisSession(transcript(t, one.line, realTitle)); got == "" {
				t.Error("skipped it and then found nothing after it")
			}
		})
	}
}

// TestTheWindowEndsWhereItEnds is the bound. A transcript is read once, from
// the front, and a line that begins inside the window and ends outside it is a
// fragment nothing can be read from.
func TestTheWindowEndsWhereItEnds(t *testing.T) {
	padding, err := json.Marshal(map[string]any{
		"type": "user", "message": map[string]any{"content": strings.Repeat("x", theHeadOfTheTranscript)},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if got := WhatClaudeTitlesThisSession(transcript(t, string(padding), realTitle)); got != "" {
		t.Errorf("read %q from beyond the window", got)
	}
}

// TestNothingToReadIsNotAProblemEither is the contract with core: an empty
// answer means "leave the stored name alone", and every one of these is
// ordinary rather than an error.
func TestNothingToReadIsNotAProblemEither(t *testing.T) {
	empty := filepath.Join(t.TempDir(), "empty.jsonl")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	for _, one := range []struct{ what, path string }{
		{"no path at all", "  "},
		{"a transcript that has not been written yet", filepath.Join(t.TempDir(), "gone.jsonl")},
		{"a transcript with nothing in it", empty},
		{"a transcript that is not JSON", transcript(t, "}{ not json", "")},
	} {
		t.Run(one.what, func(t *testing.T) {
			if got := WhatClaudeTitlesThisSession(one.path); got != "" {
				t.Errorf("read %q", got)
			}
		})
	}
}

// TestEveryTranscriptOnThisMachine runs the reader over the real corpus and
// reports what it found. It is skipped everywhere but a machine that has one,
// and it is not a substitute for the fixtures above — it is how they were
// chosen, and how the numbers in sessiontitle.go's comments were measured.
func TestEveryTranscriptOnThisMachine(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	found, err := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", "*.jsonl"))
	if err != nil || len(found) == 0 {
		t.Skip("no transcripts on this machine")
	}

	var titled int
	for _, path := range found {
		title := WhatClaudeTitlesThisSession(path)
		if title == "" {
			continue
		}
		titled++
		if strings.ContainsAny(title, "\n\r") {
			t.Errorf("%s: a name with a newline in it: %q", filepath.Base(path), title)
		}
	}
	t.Logf("%d transcripts, %d titled", len(found), titled)
}
