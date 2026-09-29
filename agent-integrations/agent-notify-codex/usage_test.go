package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// usageRecordLine and tokenCountLine are the two accounting lines as codex
// writes them — [verified 2026-09-20, 0.154.0] against a real rollout, down to
// the running totals this deliberately does not read.
//
// Only the first is read now. The second is kept in the fixtures because codex
// still writes it between the responses, so a reader that stopped skipping it
// cleanly would be found here rather than on a bar.
func usageRecordLine(responseID string, input, cached, output, threadTotal uint64) string {
	return fmt.Sprintf(`{"timestamp":"2026-09-19T21:13:17.544Z","ordinal":22,"type":"token_usage_record",`+
		`"payload":{"thread_id":"01a0bb7f","turn_id":"01a0bb84","response_id":%q,`+
		`"usage":{"input_tokens":%d,"cached_input_tokens":%d,"cache_write_input_tokens":0,`+
		`"output_tokens":%d,"reasoning_output_tokens":%d,"total_tokens":%d},`+
		`"thread_token_usage":{"input_tokens":%d,"output_tokens":0,"total_tokens":%d}}}`,
		responseID, input, cached, output, output/3, input+output, threadTotal, threadTotal)
}

func tokenCountLine(lastInput, window uint64) string {
	return fmt.Sprintf(`{"timestamp":"2026-09-19T21:13:17.545Z","ordinal":23,"type":"event_msg",`+
		`"payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":28622,"total_tokens":28681},`+
		`"last_token_usage":{"input_tokens":%d,"cached_input_tokens":14080,"output_tokens":30,"total_tokens":%d},`+
		`"model_context_window":%d},"rate_limits":{"limit_id":"codex",`+
		`"primary":{"used_percent":15.0,"window_minutes":43200,"resets_at":1792008501},`+
		`"secondary":null,"plan_type":"free"}}}`,
		lastInput, lastInput+30, window)
}

func rollout(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rollout-2026-09-19T23-07-58-01a0bb7f.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("writing the rollout: %v", err)
	}
	return path
}

// TestResponsesAreReportedOldestFirstWithTheirIds. The id is what lets core
// count a response once however many times it is handed over, and the order is
// the one core walks them in.
//
// The token_count lines between them are the ones this no longer reads: two
// responses out of four accounting lines is also the assertion that they are
// skipped rather than half-decoded into a third.
func TestResponsesAreReportedOldestFirstWithTheirIds(t *testing.T) {
	spending := WhatCodexHasSpent(rollout(t,
		usageRecordLine("resp-1", 14287, 9984, 29, 14316),
		tokenCountLine(14287, 258_400),
		usageRecordLine("resp-2", 14335, 14080, 30, 28681),
		tokenCountLine(14335, 258_400),
	))

	var order []string
	for _, response := range spending.Responses {
		order = append(order, response.Response)
	}
	if got, want := strings.Join(order, ","), "resp-1,resp-2"; got != want {
		t.Errorf("order = %s, want %s", got, want)
	}
}

// TestTheCachedShareIsNotCountedTwice is the one place codex's shape and core's
// disagree: what codex calls input includes what it read from the cache, and
// core's four counters do not overlap.
func TestTheCachedShareIsNotCountedTwice(t *testing.T) {
	spending := WhatCodexHasSpent(rollout(t, usageRecordLine("resp-1", 14335, 14080, 30, 14365)))

	if len(spending.Responses) != 1 {
		t.Fatalf("reported %d responses, want 1", len(spending.Responses))
	}
	got := spending.Responses[0]
	if want := uint64(14335 - 14080); got.Input != want {
		t.Errorf("input = %d, want %d — the fresh part only", got.Input, want)
	}
	if want := uint64(14080); got.CacheRead != want {
		t.Errorf("cache_read = %d, want %d", got.CacheRead, want)
	}
	if total := got.Input + got.CacheRead + got.CacheWrite + got.Output; total != 14335+30 {
		t.Errorf("the four counters sum to %d, want codex's own total of %d", total, 14335+30)
	}
}

// TestTheRunningTotalCodexKeepsIsNotWhatIsReported. codex adds up for itself,
// and reading its total instead would make one adapter's arithmetic different
// from every other's — and would have to decide, per agent, what a resumed
// thread's total means.
func TestTheRunningTotalCodexKeepsIsNotWhatIsReported(t *testing.T) {
	spending := WhatCodexHasSpent(rollout(t,
		usageRecordLine("resp-1", 100, 0, 10, 999_999),
	))

	if got := spending.Responses[0].Input; got != 100 {
		t.Errorf("input = %d, want the response's own 100 and not the thread's total", got)
	}
}

// TestALineBeingWrittenIsWorthNothingMoreThanSkipping. Codex is appending to
// this file while the hook reads it.
func TestALineBeingWrittenIsWorthNothingMoreThanSkipping(t *testing.T) {
	spending := WhatCodexHasSpent(rollout(t,
		usageRecordLine("resp-1", 100, 0, 10, 110),
		`{"type":"token_usage_record","payload":{"response_id":"resp-2","usa`,
	))

	if len(spending.Responses) != 1 || spending.Responses[0].Response != "resp-1" {
		t.Errorf("responses = %+v, want only the line that was finished", spending.Responses)
	}
}

// TestAResponseBehindAnEnormousTurnIsStillFound is why the read is a loop
// rather than one look at the end: the conversation is in this file too, and
// one pasted file is a line larger than every accounting line in the thread.
func TestAResponseBehindAnEnormousTurnIsStillFound(t *testing.T) {
	huge := `{"type":"response_item","payload":{"type":"message","content":"` +
		strings.Repeat("x", 300<<10) + `"}}`
	spending := WhatCodexHasSpent(rollout(t,
		usageRecordLine("resp-1", 100, 0, 10, 110),
		tokenCountLine(100, 258_400),
		huge,
	))

	if len(spending.Responses) != 1 || spending.Responses[0].Response != "resp-1" {
		t.Fatalf("responses = %+v, want resp-1 found behind a 300 KiB line", spending.Responses)
	}
}

// TestNoRolloutIsAnOrdinaryAnswer: a path the payload did not carry, a file
// that is not there. Neither is an error, and core reads an empty report as
// "leave the totals alone".
func TestNoRolloutIsAnOrdinaryAnswer(t *testing.T) {
	for _, path := range []string{"", "   ", filepath.Join(t.TempDir(), "never-written.jsonl")} {
		if spending := WhatCodexHasSpent(path); len(spending.Responses) != 0 {
			t.Errorf("WhatCodexHasSpent(%q) = %+v, want nothing", path, spending)
		}
	}
}

// TestTheWindowStopsAtEnoughResponses keeps the report bounded, and keeps the
// newest ones, which are the ones core has not counted.
func TestTheWindowStopsAtEnoughResponses(t *testing.T) {
	var lines []string
	for i := range 40 {
		lines = append(lines, usageRecordLine(fmt.Sprintf("resp-%02d", i), 100, 0, 10, 110))
	}
	spending := WhatCodexHasSpent(rollout(t, lines...))

	// Bounded by core at eight (hook.LastResponses).
	if len(spending.Responses) != 8 {
		t.Fatalf("reported %d responses, want 8", len(spending.Responses))
	}
	if last := spending.Responses[len(spending.Responses)-1].Response; last != "resp-39" {
		t.Errorf("the newest reported is %q, want resp-39", last)
	}
}
