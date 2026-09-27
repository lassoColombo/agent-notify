// Claude keeps its own token accounting in the transcript it writes for each
// session, and this is how a hook reads the part of it that is new.
//
// It is in this program rather than in core because every word of it is
// Claude's: the file, its lines, and the names of the four numbers on each one
// (R10). Core is handed responses and adds up the ones it has not seen.
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"slices"
	"strings"

	"github.com/lassoColombo/agent-notify/session"
)

// The transcript is the only place the numbers are. [verified 2026-09-20,
// 2.1.236] Nothing else Claude writes carries them: every hook input is built
// from the same six fields and none of them is a count,
// ~/.claude/sessions/<pid>.json holds the name and the status and no usage at
// all, and the dollars and the context percentage a person watches on the
// status line are handed to the statusLine command alone — a slot the user owns
// and this program will not take (§A7.4.2).
//
//	~/.claude/projects/<slug>/<session-id>.jsonl
//	{"type":"assistant","requestId":"req_011Cf…","message":{"usage":{
//	  "input_tokens":2,"cache_read_input_tokens":75042,
//	  "cache_creation_input_tokens":1572,"output_tokens":366,
//	  "output_tokens_details":{"thinking_tokens":91}}}}
//
// **There is no running total in it.** [verified 2026-09-20] a whole transcript
// grepped for a cost, a context size or a rate limit finds none of the three:
// the per-response usage above is all there is, and what a session has spent is
// the sum of it. That is why core adds rather than this (§A7.4.3) — the sum
// lives in the record, and a hook is a process that exits before the record it
// is about to write exists.
//
// **One response is written as several lines.** Claude writes one line per
// content block — the text, then each tool_use — and repeats the response's
// entire usage on every one of them. [verified 2026-09-20] 3401 assistant lines
// carrying 1869 distinct requestIds in one 51 MB transcript, so adding up lines
// overcounts output by 2.11× and cache reads by 1.77×. The requestId rides
// along with every response for exactly that reason.
const (
	// oneReadWorthOfTranscript is how much is read at a time, working backwards
	// from the end.
	oneReadWorthOfTranscript = 64 << 10
	// enoughResponses is how many the search is content to stop at. A hook
	// fires on every tool call, so one new response since the last read is the
	// ordinary case; eight is the margin for the hooks that do not fire.
	enoughResponses = 8
	// asFarBackAsItIsWorthGoing stops a search that is not finding any.
	//
	// A block can hold no responses at all, and not rarely: a tool result is a
	// line too, and reading a file writes a megabyte of one. So the search
	// cannot be a single read of the end — the newest response is often behind
	// a result far larger than the window — and it cannot be unbounded either,
	// on a path the agent is waiting on.
	asFarBackAsItIsWorthGoing = 4 << 20
)

// ClaudeTranscript is what the transcript has to say about this session, or
// nothing at all.
//
// Nothing at all is an ordinary answer: a transcript that has not been written
// yet, a path the payload did not carry, a hook firing before the first
// response. Core treats an empty answer as "leave what is stored alone".
type ClaudeTranscript struct {
	// Responses are the ones this read could see, oldest first. Core counts the
	// ones it has not counted before, so handing it the same response on twenty
	// hooks in a row costs nothing and misses nothing.
	Responses []session.Spend
	// Model is what produced the newest response, and the transcript is where it
	// has to come from: [verified 2026-09-20, 2.1.236] the hook payload does not
	// carry it. Claude builds every hook input from the same six fields and the
	// model is not among them, so this program's `model` field had been putting
	// an empty string into the record on every hook since the day it was added.
	// It is on every assistant line instead, beside the usage already read here.
	Model string
}

// WhatTheTranscriptSays reads backwards from the end of the transcript until it
// has enough responses or has looked far enough.
//
// Backwards, because the newest are the ones core has not counted, and a
// transcript only grows: a session an hour in is fifty megabytes, and reading
// it in full to find the last few hundred bytes that changed is a cost that
// grows with every turn taken.
func WhatTheTranscriptSays(transcriptPath string) ClaudeTranscript {
	transcriptPath = strings.TrimSpace(transcriptPath)
	if transcriptPath == "" {
		return ClaudeTranscript{}
	}
	file, err := os.Open(transcriptPath)
	if err != nil {
		return ClaudeTranscript{}
	}
	defer file.Close()
	facts, err := file.Stat()
	if err != nil {
		return ClaudeTranscript{}
	}

	var said ClaudeTranscript
	// carried is the head of a line the previous read landed in the middle of.
	// It belongs to a line that begins earlier in the file, so it waits here for
	// the read that brings the rest of it.
	var carried []byte
	var readSoFar int64

	for end := facts.Size(); end > 0 && readSoFar < asFarBackAsItIsWorthGoing; {
		start := max(end-oneReadWorthOfTranscript, 0)
		block := make([]byte, end-start)
		if _, err := file.ReadAt(block, start); err != nil {
			break
		}
		readSoFar += int64(len(block))

		lines := bytes.Split(slices.Concat(block, carried), []byte("\n"))
		if start > 0 {
			carried, lines = lines[0], lines[1:]
		}
		for i := len(lines) - 1; i >= 0 && len(said.Responses) < enoughResponses; i-- {
			response, model, itCost := whatThisLineCost(lines[i])
			if !itCost {
				continue
			}
			if newest := len(said.Responses) - 1; newest >= 0 &&
				said.Responses[newest].Response == response.Response {
				// The same response written across another content block. Core
				// would discard it anyway; dropping it here is what makes the
				// count above eight responses rather than eight lines.
				continue
			}
			if len(said.Responses) == 0 {
				// The first one found reading backwards is the newest in the
				// file, and the only one whose model is still true: a `/model`
				// halfway through a session leaves the older lines saying what
				// used to be the case.
				said.Model = model
			}
			said.Responses = append(said.Responses, response)
		}
		if len(said.Responses) >= enoughResponses {
			break
		}
		end = start
	}

	// Oldest first, which is the order core walks them in and the order they
	// were written in.
	slices.Reverse(said.Responses)
	return said
}

// transcriptLine is the part of one line of the transcript this program reads.
// Claude writes a dozen more fields on every one of them and will write more.
type transcriptLine struct {
	Type      string `json:"type"`
	RequestID string `json:"requestId"`
	// IsSidechain marks a subagent's own work. [verified 2026-09-20] not one
	// transcript on this machine holds a line with it set — Claude gives a
	// subagent its own file and names it in SubagentStop's
	// `agent_transcript_path` — so this is a guard rather than a filter, and it
	// guards the context level: what a subagent filled is not what the session
	// you are looking at has filled.
	IsSidechain bool `json:"isSidechain"`
	Message     struct {
		Model string `json:"model"`
		Usage struct {
			Input      uint64 `json:"input_tokens"`
			Output     uint64 `json:"output_tokens"`
			CacheRead  uint64 `json:"cache_read_input_tokens"`
			CacheWrite uint64 `json:"cache_creation_input_tokens"`
			Details    struct {
				Thinking uint64 `json:"thinking_tokens"`
			} `json:"output_tokens_details"`
		} `json:"usage"`
	} `json:"message"`
}

// assistantLine is the cheap test that decides whether a line is worth
// decoding. Most of a transcript is prompts, tool results and attachments, and
// a file read can put a megabyte of one of them in front of this.
//
// It matches the value rather than the pair, because a filter that depends on
// how the writer spaces its JSON is a filter that stops matching on a day
// nobody is watching, and what it costs to be wrong here is silence. The decode
// below is what actually decides.
var assistantLine = []byte(`"assistant"`)

// whatThisLineCost reads one line of the transcript, and says whether it is a
// response with a price on it — and which model was charged for it.
//
// Claude is appending to this file while this runs and the read may have landed
// mid-line, so a line that does not parse is ordinary and worth nothing more
// than moving on to the next one.
func whatThisLineCost(line []byte) (session.Spend, string, bool) {
	if !bytes.Contains(line, assistantLine) {
		return session.Spend{}, "", false
	}
	var written transcriptLine
	if err := json.Unmarshal(line, &written); err != nil {
		return session.Spend{}, "", false
	}
	if written.Type != "assistant" || written.IsSidechain || written.RequestID == "" {
		return session.Spend{}, "", false
	}

	usage := written.Message.Usage
	spend := session.Spend{
		Response:   written.RequestID,
		Input:      usage.Input,
		Output:     usage.Output,
		CacheRead:  usage.CacheRead,
		CacheWrite: usage.CacheWrite,
		// Thinking tokens are part of what was charged as output, which is what
		// core's `reasoning` means: a share of it, never an addend beside it.
		Reasoning: usage.Details.Thinking,
	}
	if spend.Input == 0 && spend.Output == 0 && spend.CacheRead == 0 && spend.CacheWrite == 0 {
		// An assistant line with no usage at all is not a response that was
		// charged for, whatever else it is.
		return session.Spend{}, "", false
	}
	return spend, strings.TrimSpace(written.Message.Model), true
}
