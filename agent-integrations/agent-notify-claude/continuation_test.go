package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The lines below are copies of real ones. [captured 2026-11-23, 2.1.311]
const (
	oldSession = "c84cb845-ab87-48f6-b7e5-4d94d124820d"
	newSession = "aab2c500-9729-4eaa-864f-d205d5404664"

	realCostState   = `{"type":"cost-state","sessionId":"c84cb845-ab87-48f6-b7e5-4d94d124820d","totalCostUSD":3.47}`
	realContinuedIn = `{"type":"continued-in","timestamp":"2026-11-23T08:25:07.077Z","sessionId":"c84cb845-ab87-48f6-b7e5-4d94d124820d","continuedInSessionId":"aab2c500-9729-4eaa-864f-d205d5404664"}`
)

// projectDirectory writes transcripts, named for their sessions, into one
// directory the way Claude keeps them, and returns the path of the one named
// first: the session whose hook is asking.
func projectDirectory(t *testing.T, transcripts map[string][]string, asking string) string {
	t.Helper()
	directory := t.TempDir()
	for session, lines := range transcripts {
		path := filepath.Join(directory, session+".jsonl")
		if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
	}
	return filepath.Join(directory, asking+".jsonl")
}

// TestTheSessionHandedOverFromIsFound is the hand-over as Claude wrote it: the
// old transcript ends with the line, and the new one, which may not exist yet
// on its SessionStart, says nothing about the old one.
func TestTheSessionHandedOverFromIsFound(t *testing.T) {
	for _, one := range []struct {
		what  string
		yours []string
	}{
		{"before the new transcript is written", nil},
		{"after", []string{realTypedPrompt, realAssistant}},
	} {
		t.Run(one.what, func(t *testing.T) {
			transcripts := map[string][]string{
				oldSession:  {realTypedPrompt, realAssistant, realCostState, realContinuedIn},
				"unrelated": {realTypedPrompt, realAssistant},
			}
			if one.yours != nil {
				transcripts[newSession] = one.yours
			}
			path := projectDirectory(t, transcripts, newSession)
			if got := WhatThisSessionContinues(path, newSession); got != oldSession {
				t.Errorf("continues %q, want %q", got, oldSession)
			}
		})
	}
}

// TestNothingIsContinuedWithoutTheLastWord is every way a transcript can
// mention the line and still not be the session this one carries on from.
func TestNothingIsContinuedWithoutTheLastWord(t *testing.T) {
	for _, one := range []struct {
		what string
		old  []string
	}{
		{"no hand-over at all", []string{realTypedPrompt, realAssistant}},
		{"the conversation went on in the old session after it",
			[]string{realContinuedIn, realTypedPrompt, realAssistant}},
		{"a reply after it", []string{realContinuedIn, realAssistant}},
		{"handed over to somebody else",
			[]string{strings.Replace(realContinuedIn, newSession, "0af2cd8f-c261-4f22-9b62-e7fcfcbd9fe6", 1)}},
		{"a line Claude was still writing", []string{realAssistant, realContinuedIn[:60]}},
		{"the words in a prompt", []string{`{"type":"user","message":{"role":"user","content":"\"continued-in\" ` + newSession + `"}}`}},
	} {
		t.Run(one.what, func(t *testing.T) {
			path := projectDirectory(t, map[string][]string{oldSession: one.old}, newSession)
			if got := WhatThisSessionContinues(path, newSession); got != "" {
				t.Errorf("continues %q", got)
			}
		})
	}
}

// TestASessionDoesNotContinueItself: its own transcript is never asked, and a
// line naming itself on both sides is nobody's hand-over.
func TestASessionDoesNotContinueItself(t *testing.T) {
	itself := strings.Replace(realContinuedIn, oldSession, newSession, 1)
	path := projectDirectory(t, map[string][]string{
		newSession: {realContinuedIn},
		"other":    {itself},
	}, newSession)
	if got := WhatThisSessionContinues(path, newSession); got != "" {
		t.Errorf("continues %q", got)
	}
}

// TestOnlyTheEndIsRead is the bound: a line further from the end than one read
// reaches is not found, and a read that begins mid-line does not trip on it.
func TestOnlyTheEndIsRead(t *testing.T) {
	padding := `{"type":"attachment","attachment":"` + strings.Repeat("x", theEndOfATranscript) + `"}`
	near := projectDirectory(t, map[string][]string{
		oldSession: {padding, realContinuedIn},
	}, newSession)
	if got := WhatThisSessionContinues(near, newSession); got != oldSession {
		t.Errorf("continues %q at the end of a long transcript, want %q", got, oldSession)
	}
	far := projectDirectory(t, map[string][]string{
		oldSession: {realContinuedIn, padding},
	}, newSession)
	if got := WhatThisSessionContinues(far, newSession); got != "" {
		t.Errorf("continues %q from beyond the end it reads", got)
	}
}

// TestNoPathNoAnswer: a payload without a transcript or a session, and a
// directory that is not there, are nothing and never an error.
func TestNoPathNoAnswer(t *testing.T) {
	for _, one := range []struct{ path, session string }{
		{"", newSession},
		{"/nowhere/at/all/" + newSession + ".jsonl", newSession},
		{projectDirectory(t, map[string][]string{oldSession: {realContinuedIn}}, newSession), ""},
	} {
		if got := WhatThisSessionContinues(one.path, one.session); got != "" {
			t.Errorf("WhatThisSessionContinues(%q, %q) = %q", one.path, one.session, got)
		}
	}
}

// TestTranslateCarriesIt: whatever hook was handed it, the report says it.
func TestTranslateCarriesIt(t *testing.T) {
	report, worth := Translate("SessionStart", Payload{SessionID: newSession, Source: "startup"},
		ClaudeSession{}, ClaudeTranscript{}, "", oldSession)
	if !worth || report.ContinuedFrom != oldSession {
		t.Errorf("report = %+v, want it to carry %q", report, oldSession)
	}
}
