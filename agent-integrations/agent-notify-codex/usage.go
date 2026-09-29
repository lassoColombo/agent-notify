// Codex keeps its own token accounting in the rollout it writes for each
// thread, and this is how a hook reads the part of it that is new.
//
// It is in this program rather than in core because every word of it is
// codex's: the file, the two kinds of line that carry numbers, and the names of
// those numbers (R10). Core is handed responses and adds up the ones it has not
// seen.
package main

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/lassoColombo/agent-notify/hook"
	"github.com/lassoColombo/agent-notify/session"
)

// Codex writes one line per response into the rollout it keeps for each thread,
// and that line is the whole of what is read here:
//
//	~/.codex/sessions/2026/09/19/rollout-<timestamp>-<thread-id>.jsonl
//	{"type":"token_usage_record","payload":{"response_id":"resp_0d6a…",
//	  "usage":{"input_tokens":14335,"cached_input_tokens":14080,
//	    "cache_write_input_tokens":0,"output_tokens":30,
//	    "reasoning_output_tokens":0,"total_tokens":14365},
//	  "thread_token_usage":{…}}}
//
// **codex does the adding itself**, in `thread_token_usage` and
// `total_token_usage`, and this reads neither. What is reported is the
// per-response `usage` beside them, because core adds for every agent or for
// none: a second rule that trusts one agent's running total would be a rule
// that has to decide, per agent, whether a resumed thread's total starts again.
// Reading the same per-response lines Claude's adapter reads costs nothing.
//
// The `event_msg`/`token_count` lines beside these carried two more things
// codex alone could answer — `model_context_window` and the account's
// `rate_limits` — and both were read here until the record stopped having
// anywhere to put them (D-76).
// Spending is what to report about this thread's tokens, or nothing at all.
//
// Nothing at all is an ordinary answer: a rollout that has not been written
// yet, a path the payload did not carry, a hook firing before the first
// response. Core treats no responses as "leave the totals alone".
type Spending struct {
	// Responses are the ones this read could see, oldest first. Core counts the
	// ones it has not counted before, so handing it the same response on twenty
	// hooks in a row costs nothing and misses nothing.
	Responses []session.Spend
}

// WhatCodexHasSpent reads the newest responses off the end of the rollout.
func WhatCodexHasSpent(rolloutPath string) Spending {
	return Spending{Responses: hook.LastResponses(strings.TrimSpace(rolloutPath), whatThisLineCost)}
}

// tokenUsage is codex's per-response accounting, on both kinds of line.
//
// `cached_input_tokens` is a share of `input_tokens` rather than an addend
// beside it — [verified 2026-09-20, 0.154.0] `total_tokens` is `input_tokens` +
// `output_tokens` exactly, with the cached figure smaller than the input it is
// part of. Core's four counters are disjoint, so the cached half is subtracted
// out rather than reported twice.
type tokenUsage struct {
	Input      uint64 `json:"input_tokens"`
	Cached     uint64 `json:"cached_input_tokens"`
	CacheWrite uint64 `json:"cache_write_input_tokens"`
	Output     uint64 `json:"output_tokens"`
	Reasoning  uint64 `json:"reasoning_output_tokens"`
}

// spend turns codex's shape into core's, which differ in one place: what codex
// calls input includes what it read from the cache, and core's do not overlap.
func (u tokenUsage) spend(responseID string) session.Spend {
	cached := min(u.Cached, u.Input)
	return session.Spend{
		Response:   responseID,
		Input:      u.Input - cached,
		CacheRead:  cached,
		CacheWrite: u.CacheWrite,
		Output:     u.Output,
		// Reasoning tokens are part of what was charged as output, which is
		// what core's `reasoning` means: a share of it, never an addend.
		Reasoning: u.Reasoning,
	}
}

// rolloutLine is the part of one line of the rollout this program reads. Codex
// writes a dozen kinds of line and will write more.
type rolloutLine struct {
	Type    string `json:"type"`
	Payload struct {
		ResponseID string     `json:"response_id"`
		Usage      tokenUsage `json:"usage"`
	} `json:"payload"`
}

// usageRecord is the cheap test that decides whether a line is worth decoding.
// It matches the value rather than the pair, because a filter that depends on
// how the writer spaces its JSON stops matching on a day nobody is watching.
// The decode below is what actually decides.
var usageRecord = []byte(`"token_usage_record"`)

// whatThisLineCost reads one line of the rollout, and says whether it is a
// response with a price on it.
//
// Codex is appending to this file while this runs and the read may have landed
// mid-line, so a line that does not parse is ordinary and worth nothing more
// than moving on to the next one.
func whatThisLineCost(line []byte) (session.Spend, bool) {
	if !bytes.Contains(line, usageRecord) {
		return session.Spend{}, false
	}
	var written rolloutLine
	if err := json.Unmarshal(line, &written); err != nil {
		return session.Spend{}, false
	}
	if written.Type != "token_usage_record" || written.Payload.ResponseID == "" {
		return session.Spend{}, false
	}
	usage := written.Payload.Usage
	if usage.Input == 0 && usage.Output == 0 && usage.CacheWrite == 0 {
		return session.Spend{}, false
	}
	return usage.spend(written.Payload.ResponseID), true
}
