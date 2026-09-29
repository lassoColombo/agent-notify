package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// assistant writes one transcript line the way Claude writes it: the numbers
// are the shape of a real one, and the requestId is repeated across content
// blocks exactly as it is in the file.
func assistant(requestID string, output, cacheRead uint64) string {
	return fmt.Sprintf(`{"type":"assistant","requestId":%q,"isSidechain":false,"message":{"model":"claude-opus-5",`+
		`"usage":{"input_tokens":2,"cache_creation_input_tokens":1572,"cache_read_input_tokens":%d,`+
		`"output_tokens":%d,"output_tokens_details":{"thinking_tokens":%d}}}}`,
		requestID, cacheRead, output, output/2)
}

func transcript(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("writing the transcript: %v", err)
	}
	return path
}

// TestOneResponseWrittenAsSeveralLinesIsReportedOnce is the measurement this
// file exists for: Claude writes a line per content block and repeats the whole
// usage on each, and a reader that hands every line to core as a response would
// be handing it the same response three times.
func TestOneResponseWrittenAsSeveralLinesIsReportedOnce(t *testing.T) {
	said := WhatTheTranscriptSays(transcript(t,
		assistant("req-1", 100, 40_000),
		assistant("req-1", 100, 40_000),
		assistant("req-1", 100, 40_000),
	))

	if len(said.Responses) != 1 {
		t.Fatalf("reported %d responses for one response written three times: %+v",
			len(said.Responses), said.Responses)
	}
	if got := said.Responses[0]; got.Output != 100 || got.Reasoning != 50 {
		t.Errorf("response = %+v, want output 100 of which 50 thinking", got)
	}
}

// TestResponsesAreReportedOldestFirst pins the order core walks them in. The
// read runs backwards and the report must not.
func TestResponsesAreReportedOldestFirst(t *testing.T) {
	said := WhatTheTranscriptSays(transcript(t,
		assistant("req-1", 10, 1_000),
		assistant("req-2", 20, 2_000),
		assistant("req-3", 30, 3_000),
	))

	var order []string
	for _, response := range said.Responses {
		order = append(order, response.Response)
	}
	if want := []string{"req-1", "req-2", "req-3"}; strings.Join(order, ",") != strings.Join(want, ",") {
		t.Errorf("order = %v, want %v", order, want)
	}
}

// TestAResponseBehindAnEnormousToolResultIsStillFound is why the read is a loop
// rather than one look at the end of the file. A tool result is a line too, and
// reading a file writes a megabyte of one — so the newest response is regularly
// further back than a single window reaches.
func TestAResponseBehindAnEnormousToolResultIsStillFound(t *testing.T) {
	huge := `{"type":"user","message":{"content":[{"type":"tool_result","content":"` +
		strings.Repeat("x", 300<<10) + `"}]}}`
	said := WhatTheTranscriptSays(transcript(t,
		assistant("req-1", 10, 40_000),
		huge,
	))

	if len(said.Responses) != 1 {
		t.Fatalf("reported %d responses behind a 300 KiB tool result, want 1", len(said.Responses))
	}
	if said.Responses[0].Response != "req-1" {
		t.Errorf("found %q, want req-1", said.Responses[0].Response)
	}
}

// TestASearchThatFindsNothingGivesUp: a transcript of nothing but enormous
// results must not be read in full on the path the agent is waiting on.
func TestASearchThatFindsNothingGivesUp(t *testing.T) {
	var lines []string
	for range 12 {
		lines = append(lines, `{"type":"user","message":{"content":"`+strings.Repeat("x", 512<<10)+`"}}`)
	}
	lines = append([]string{assistant("req-1", 10, 40_000)}, lines...)

	if said := WhatTheTranscriptSays(transcript(t, lines...)); len(said.Responses) != 0 {
		t.Errorf("found %d responses past the backstop, want the search to have stopped",
			len(said.Responses))
	}
}

// TestALineBeingWrittenIsWorthNothingMoreThanSkipping. Claude is appending to
// this file while the hook reads it, so a half-written last line is ordinary.
func TestALineBeingWrittenIsWorthNothingMoreThanSkipping(t *testing.T) {
	said := WhatTheTranscriptSays(transcript(t,
		assistant("req-1", 10, 40_000),
		`{"type":"assistant","requestId":"req-2","message":{"usa`,
	))

	if len(said.Responses) != 1 || said.Responses[0].Response != "req-1" {
		t.Errorf("responses = %+v, want only the line that was finished", said.Responses)
	}
}

// TestASubagentsSpendingIsNotThisSessionsSpending. Claude gives a subagent its
// own transcript today, so this is a guard rather than a filter — but what a
// subagent spent is not what the session on the bar spent, and counting it as
// one would be worse than counting nothing.
func TestASubagentsSpendingIsNotThisSessionsSpending(t *testing.T) {
	sidechain := strings.Replace(assistant("req-sub", 999, 99_000), `"isSidechain":false`, `"isSidechain":true`, 1)
	said := WhatTheTranscriptSays(transcript(t, assistant("req-1", 10, 40_000), sidechain))

	if len(said.Responses) != 1 || said.Responses[0].Response != "req-1" {
		t.Errorf("responses = %+v, want the session's own", said.Responses)
	}
}

// TestNoTranscriptIsAnOrdinaryAnswer: a path the payload did not carry, a file
// that is not there yet, a session that has not had a response. None of them is
// an error, and core reads an empty report as "leave the totals alone".
func TestNoTranscriptIsAnOrdinaryAnswer(t *testing.T) {
	for _, path := range []string{"", "   ", filepath.Join(t.TempDir(), "never-written.jsonl")} {
		if said := WhatTheTranscriptSays(path); len(said.Responses) != 0 || said.Model != "" {
			t.Errorf("WhatTheTranscriptSays(%q) = %+v, want nothing", path, said)
		}
	}
}

// TestTheWindowStopsAtEnoughResponses keeps the report bounded. Core only needs
// what it has not counted, and a hook fires often enough that eight is already
// a margin.
func TestTheWindowStopsAtEnoughResponses(t *testing.T) {
	var lines []string
	for i := range 40 {
		lines = append(lines, assistant(fmt.Sprintf("req-%02d", i), 10, 1_000))
	}
	said := WhatTheTranscriptSays(transcript(t, lines...))

	// Bounded by core at eight (hook.LastResponses).
	if len(said.Responses) != 8 {
		t.Fatalf("reported %d responses, want 8", len(said.Responses))
	}
	// And they are the newest ones, because those are the ones core has not
	// counted.
	if last := said.Responses[len(said.Responses)-1].Response; last != "req-39" {
		t.Errorf("the newest reported is %q, want req-39", last)
	}
}

// TestTheModelComesOffTheNewestResponse. The hook payload does not carry one —
// [verified 2026-09-20, 2.1.236] Claude builds every hook input from the same
// six fields and the model is not among them — so the only honest source is a
// line that was actually charged, and the newest of those is the only one still
// true after a `/model` halfway through a session.
func TestTheModelComesOffTheNewestResponse(t *testing.T) {
	older := assistant("req-1", 10, 1_000)
	newer := strings.Replace(assistant("req-2", 20, 2_000), "claude-opus-5", "claude-haiku-4-5", 1)

	said := WhatTheTranscriptSays(transcript(t, older, newer))
	if said.Model != "claude-haiku-4-5" {
		t.Errorf("model = %q, want the one that produced the newest response", said.Model)
	}
}

// TestNoResponsesMeansNoModel, which core reads as "leave the stored one alone"
// rather than as a session that has stopped having a model.
func TestNoResponsesMeansNoModel(t *testing.T) {
	said := WhatTheTranscriptSays(transcript(t, `{"type":"user","message":{"content":"hello"}}`))
	if said.Model != "" {
		t.Errorf("model = %q, want nothing said", said.Model)
	}
}
