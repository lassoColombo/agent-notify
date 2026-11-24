// Claude hands a conversation over to a session of its own, and this is how a
// hook finds the session it was handed over from.
//
// It is in this program rather than in core because every word of it is
// Claude's: the file, the line type and the rule for reading it (R10). Core is
// handed a session id and never learns where it came from.
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/lassoColombo/agent-notify/hook"
)

// [verified 2026-11-23, 2.1.311] A `/compact` no longer compacts in place.
// The session's process dispatches the conversation to Claude's background
// daemon, which carries it on under a new session id, in a process of its
// own; the process that ran the old session stays, as a window onto the new
// one. The old session gets no SessionEnd and nothing reports on it again, so
// with its process alive it stayed `working` until the window was closed.
//
// What Claude leaves behind is one line, appended to the OLD transcript as
// the hand-over happens:
//
//	{"type":"continued-in","timestamp":"…","sessionId":"<old>","continuedInSessionId":"<new>"}
//
// Claude reads it back itself, from the end of the file, and a conversation
// that went on in the old session after it cancels it. Claude cancels it only
// for a whole turn; here any user or assistant line does. That can miss a
// hand-over, which leaves the old session as it always was, and can never end
// a session that is still being talked to.
//
// The new session's transcript does not name the old one, so the old one is
// found by looking: the end of every other transcript in the same directory.
// It is looked for on SessionStart, which in the hand-overs seen runs after the
// line is written, and again on UserPromptSubmit in case it did not: once a
// turn at most, never once a tool call (R1). [measured 2026-11-23] 0.2ms in a
// directory of ten transcripts and 2.3ms in the largest here, 137 entries,
// nearly all of it opening files rather than reading them: a window of 4 KiB
// took 1.5ms.
const (
	// theEndOfATranscript is how much of the end of each transcript is read,
	// newest line first. The line is the last thing written to the old session and
	// is under 200 bytes; the rest is room for bookkeeping that lands after it.
	theEndOfATranscript = 16 << 10
)

// WhatThisSessionContinues is the session Claude handed this one's
// conversation over from, or nothing at all, which is the ordinary answer and
// never an error.
func WhatThisSessionContinues(transcriptPath, sessionID string) string {
	transcriptPath, sessionID = strings.TrimSpace(transcriptPath), strings.TrimSpace(sessionID)
	if transcriptPath == "" || sessionID == "" {
		return ""
	}
	directory, own := filepath.Split(transcriptPath)
	entries, err := os.ReadDir(directory)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == own || !entry.Type().IsRegular() || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		if from := continuedIn(filepath.Join(directory, name), sessionID); from != "" {
			return from
		}
	}
	return ""
}

// The cheap tests that decide whether a line is worth decoding, beside
// usage.go's assistantLine. They match values rather than pairs, for its
// reason; the decode is what decides.
var (
	continuedInLine = []byte(`"continued-in"`)
	userLine        = []byte(`"user"`)
)

// tailLine is the part of one line this program reads.
type tailLine struct {
	Type                 string `json:"type"`
	SessionID            string `json:"sessionId"`
	ContinuedInSessionID string `json:"continuedInSessionId"`
}

// continuedIn is the session one transcript belongs to, when the last word at
// its end is that it was continued in sessionID.
func continuedIn(path, sessionID string) string {
	from := ""
	_ = hook.ReadBackwards(path, theEndOfATranscript, theEndOfATranscript, func(line []byte) bool {
		if !bytes.Contains(line, continuedInLine) && !bytes.Contains(line, userLine) &&
			!bytes.Contains(line, assistantLine) {
			return true
		}
		var written tailLine
		if err := json.Unmarshal(line, &written); err != nil {
			// Claude may be midway through appending it.
			return true
		}
		switch written.Type {
		case "continued-in":
			if claimed := strings.TrimSpace(written.SessionID); written.ContinuedInSessionID == sessionID &&
				claimed != sessionID {
				from = claimed
			}
			return false
		case "user", "assistant":
			return false
		}
		return true
	})
	return from
}
