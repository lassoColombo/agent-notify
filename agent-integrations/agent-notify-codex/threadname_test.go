package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// recordedIndex is a copy of a real one, in the order codex wrote it.
// [captured 2026-09-15, 0.154.0] Nine threads, no duplicate ids, ascending by
// updated_at — the properties the tail-first read depends on.
const recordedIndex = `{"id":"01a0a6f4-96d4-7182-b91b-eff9ecf25adf","thread_name":"Reply with ready","updated_at":"2026-09-15T21:24:52.74141Z"}
{"id":"01a0a709-2f5b-76c3-8043-1519d7ba19d1","thread_name":"Reply with fresh","updated_at":"2026-09-15T21:46:59.760977Z"}
{"id":"01a0a712-b528-7a51-9d64-6d0f66b0f0a1","thread_name":"Reply hello","updated_at":"2026-09-15T21:58:22.983419Z"}
{"id":"01a0a715-17cb-70c1-ada9-1e43b2591d48","thread_name":"Run sleep command","updated_at":"2026-09-15T21:59:47.85139Z"}
{"id":"01a0a716-148b-7c31-8b8e-4a2d3f1c9e70","thread_name":"Run exact shell command","updated_at":"2026-09-15T22:00:52.888979Z"}
{"id":"01a0a71a-5e71-7980-885a-1dbdf72d24ca","thread_name":"Reply with x","updated_at":"2026-09-15T22:05:33.009492Z"}
`

// codexHome points this program at a temporary ~/.codex holding the given
// index, and returns the path to it.
func codexHome(t *testing.T, index string) string {
	t.Helper()
	home := t.TempDir()
	path := filepath.Join(home, indexFile)
	if index != "" {
		if err := os.WriteFile(path, []byte(index), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	t.Setenv(codexHomeVariable, home)
	return path
}

func TestTheThreadNameIsReadOutOfTheIndex(t *testing.T) {
	codexHome(t, recordedIndex)

	for id, want := range map[string]string{
		"01a0a71a-5e71-7980-885a-1dbdf72d24ca": "Reply with x",     // the last line
		"01a0a712-b528-7a51-9d64-6d0f66b0f0a1": "Reply hello",      // the middle
		"01a0a6f4-96d4-7182-b91b-eff9ecf25adf": "Reply with ready", // the first
		"01a0a716-148b-7c31-8b8e-4a2d3f1c9e70": "Run exact shell command",
	} {
		if got := WhatCodexCallsThisThread(id); got != want {
			t.Errorf("thread %s reported %q, want %q", id[:8], got, want)
		}
	}
}

// fillerLine is one entry for a thread nobody is asking about. Everything that
// builds a large index is made of these.
const fillerLine = `{"id":"filler","thread_name":"one of very many","updated_at":"2026-01-01T00:00:00Z"}`

// fillerOfExactly is that many bytes of index, to the byte, so that a test can
// put a chunk boundary where it wants one. The last line is padded with the
// leading whitespace JSON allows rather than by inventing a different shape.
func fillerOfExactly(t *testing.T, want int) string {
	t.Helper()
	line := len(fillerLine) + 1
	if want < 2*line {
		t.Fatalf("fillerOfExactly(%d): too small to pad; a line is %d bytes", want, line)
	}
	var index strings.Builder
	for index.Len()+line <= want-line {
		index.WriteString(fillerLine + "\n")
	}
	index.WriteString(strings.Repeat(" ", want-index.Len()-line) + fillerLine + "\n")
	if index.Len() != want {
		t.Fatalf("fillerOfExactly(%d) produced %d bytes", want, index.Len())
	}
	return index.String()
}

// TestAThreadNamedLongAgoIsStillFound is the reason the search does not stop at
// the first read.
//
// Entries are ordered by when a thread was *named*, and a name is never
// rewritten afterwards — so a session you leave open all week is pushed further
// and further from the end by every thread named since, while remaining the one
// running. Stopping at one read would lose exactly the long-lived session whose
// name is most worth having.
func TestAThreadNamedLongAgoIsStillFound(t *testing.T) {
	index := `{"id":"named-on-monday","thread_name":"still going","updated_at":"2026-09-14T09:00:04Z"}` + "\n" +
		fillerOfExactly(t, oneReadWorthOfIndex*5)
	codexHome(t, index)

	if got := WhatCodexCallsThisThread("named-on-monday"); got != "still going" {
		t.Errorf("reported %q five reads from the end, want it found", got)
	}
}

// TestTheSearchStopsSomewhere is the bound it runs under: a hook is on the path
// the agent waits on, and an index past anything a person could have produced
// must not be read in full.
func TestTheSearchStopsSomewhere(t *testing.T) {
	index := `{"id":"beyond-reason","thread_name":"the first thread there ever was",` +
		`"updated_at":"2026-01-01T00:00:00Z"}` + "\n" +
		fillerOfExactly(t, asFarBackAsItIsWorthGoing+oneReadWorthOfIndex)
	codexHome(t, index)

	if got := WhatCodexCallsThisThread("beyond-reason"); got != "" {
		t.Errorf("reported %q from beyond the cap, want the search to have stopped", got)
	}
}

// TestALineAcrossAReadIsPutBackTogether is the half-line problem, aimed rather
// than hoped for: the index is built so that a read boundary falls in the
// middle of the line being looked for. Reading backwards, that line arrives as
// a tail and then a head, and only the read that brings the head may parse it.
func TestALineAcrossAReadIsPutBackTogether(t *testing.T) {
	straddler := `{"id":"across-the-boundary","thread_name":"in two halves",` +
		`"updated_at":"2026-09-19T00:00:00Z"}` + "\n"

	// The first read starts at size-oneReadWorthOfIndex. Make what follows the
	// straddler short by half its length, and that offset lands inside it.
	index := fillerOfExactly(t, oneReadWorthOfIndex*2) + straddler +
		fillerOfExactly(t, oneReadWorthOfIndex-len(straddler)/2)
	path := codexHome(t, index)

	facts, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	boundary := facts.Size() - oneReadWorthOfIndex
	begins := int64(oneReadWorthOfIndex * 2)
	if boundary <= begins || boundary >= begins+int64(len(straddler)) {
		t.Fatalf("the test did not put a read boundary inside the line: boundary %d, line %d..%d",
			boundary, begins, begins+int64(len(straddler)))
	}

	if got := WhatCodexCallsThisThread("across-the-boundary"); got != "in two halves" {
		t.Errorf("reported %q for a line split across two reads", got)
	}
}

// TestNothingToReadIsNotAProblem is the contract with core: every way this can
// come up empty is an empty name, which means "leave the stored one alone".
func TestNothingToReadIsNotAProblem(t *testing.T) {
	for _, one := range []struct {
		what      string
		index     string
		sessionID string
	}{
		{what: "no index at all", index: "", sessionID: "01a0a71a-5e71-7980-885a-1dbdf72d24ca"},
		{what: "a thread it has never heard of", index: recordedIndex, sessionID: "nobody"},
		{what: "no session id", index: recordedIndex, sessionID: "  "},
		{what: "a line codex was halfway through writing", index: `{"id":"x","thre`, sessionID: "x"},
		{what: "a thread with no name yet", index: `{"id":"x","thread_name":"  "}`, sessionID: "x"},
	} {
		t.Run(one.what, func(t *testing.T) {
			codexHome(t, one.index)
			if got := WhatCodexCallsThisThread(one.sessionID); got != "" {
				t.Errorf("reported %q, want nothing", got)
			}
		})
	}
}

// TestEveryHookCarriesTheName is the policy half, and it is in Translate so it
// can be tested without a filesystem.
func TestEveryHookCarriesTheName(t *testing.T) {
	for _, raw := range []string{
		recordedSessionStart, recordedUserPrompt, recordedPostToolUse,
		recordedPermissionRequest, recordedStop, recordedInterrupt, recordedSessionEnd,
	} {
		var payload Payload
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			t.Fatalf("the fixture is not valid JSON: %v", err)
		}
		report, worth := Translate(payload, "Run exact shell command", Spending{})
		if !worth {
			t.Fatalf("%s was discarded", payload.HookEventName)
		}
		if report.Name != "Run exact shell command" {
			t.Errorf("Name = %q on %s", report.Name, payload.HookEventName)
		}
	}
}
