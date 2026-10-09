// Command agent-notify-claude is Claude Code's agent-integration: it turns
// Claude's hooks into agent-notify's events and nothing else. It produces seven
// of the nine: never `turn-interrupted`, never `context-changed`.
//
// Everything peculiar to Claude lives in this file. Everything after it is
// hook.Record, which is identical for every agent.
//
// Core knows nothing about this program and never will (R10). Claude's own
// settings.json names it, so it works the moment it exists; `install` writes
// that block.
package main

import (
	"cmp"
	"strings"

	"github.com/lassoColombo/agent-notify/session"
)

// AgentName is what every record this writes says it is, and what the user's
// configuration matches on.
const AgentName = "claude"

// SubscribedHooks is every hook this program wants installed.
//
// PreToolUse is deliberately absent. PostToolUse already says the session is
// working, and subscribing to both would double the number of processes Claude
// waits for on every single tool call — [verified] 3,017 PreToolUse against
// 2,915 PostToolUse in two days of real use. A `working/running-<tool>` detail
// is what it would buy, and it is not worth that.
var SubscribedHooks = []string{
	"SessionStart",
	"UserPromptSubmit",
	"PostToolUse",
	"SubagentStop",
	"PreCompact",
	"Stop",
	"StopFailure",
	"Notification",
	"SessionEnd",
}

// Payload is the part of Claude's JSON this program reads.
//
// Everything is optional and unknown fields are ignored: Claude sends a great
// deal more than this, sends different subsets per hook, and adds fields
// between releases. A program that fell over when it did would break on an
// upgrade nobody told it about.
type Payload struct {
	HookEventName  string `json:"hook_event_name"`
	SessionID      string `json:"session_id"`
	Cwd            string `json:"cwd"`
	TranscriptPath string `json:"transcript_path"`

	// UserPromptSubmit and SessionStart: the title the session was given — a
	// `/rename`, `--name`, or a hook's `sessionTitle` — and absent when nobody
	// gave it one. [verified 2026-10-09, 2.1.285] It is on no other hook, and
	// it is never Claude's placeholder or its `ai-title` (sessionname.go).
	SessionTitle string `json:"session_title"`

	// SessionStart: startup, resume, clear or compact. [verified 2026-09-17]
	// against 38 real starts; all four occur.
	Source string `json:"source"`
	// SessionEnd: other, prompt_input_exit, clear or resume.
	Reason string `json:"reason"`

	// UserPromptSubmit: what was just typed.
	Prompt string `json:"prompt"`
	// Stop and SubagentStop: the answer the turn ended with.
	LastAssistantMessage string `json:"last_assistant_message"`
	// Notification: the text, and which kind it is.
	Message          string `json:"message"`
	NotificationType string `json:"notification_type"`
	// SubagentStop: which subagent, and of what kind. An empty kind is the
	// discriminator this program turns on — see Translate.
	AgentType string `json:"agent_type"`
	// StopFailure: why the turn died rather than finished. [verified
	// 2026-09-22, 2.1.267] against a real payload and against the binary, which
	// builds this hook's input as `error`, `error_details` and the same
	// `last_assistant_message` a Stop carries. The names this read until then —
	// `error_type` and `error_message` — came from the prior Rust adapter and
	// were never in a payload anybody had seen, so every turn-failed ever
	// written carried an empty detail and no message at all.
	//
	// `error` is the kind: `server_error`, and the `overloaded_error` the old
	// guess was modelled on. `error_details` is free text and sometimes a JSON
	// blob, and it is the message only when the turn died without the assistant
	// saying anything.
	Error        string `json:"error"`
	ErrorDetails string `json:"error_details"`
}

// NotificationsThatWantYou are the kinds that genuinely mean "the agent needs
// YOU". The others — idle nudges, auth notices, quota chatter — must never
// escalate, or the state stops meaning anything.
//
// [verified 2026-09-17] Of 151 real notifications, 64 were permission_prompt
// and 87 were idle_prompt, and every permission_prompt arrived directly after
// a PreToolUse. The type field is a reliable discriminator; matching on the
// message text is not needed and never was.
var NotificationsThatWantYou = map[string]bool{
	"permission_prompt":      true,
	"elicitation_dialog":     true,
	"elicitation_url_dialog": true,
	"agent_needs_input":      true,
}

// Translate is what this hook says about the session, or false to say nothing.
//
// PURE, and where all the thinking is: no store, no hook, no agent needed to
// test it. That is why what Claude knows about the session and what it has
// spent are parameters rather than lookups — each is a file read (sessionname.go
// and usage.go), and deciding which hooks carry them is policy, which belongs
// where policy is tested.
//
// Every hook carries it. The report is being written anyway, so the name costs
// no extra write, and a `/rename` halfway through a session lands on the next
// tool call instead of waiting for one that happens to be special.
func Translate(
	hookName string, payload Payload, claude ClaudeSession,
	said ClaudeTranscript, title string,
) (session.Report, bool) {
	if strings.TrimSpace(payload.SessionID) == "" {
		// Nothing can be written about a session nobody can name.
		return session.Report{}, false
	}

	report := session.Report{
		Key: session.Key{Agent: AgentName, SessionID: payload.SessionID},
		// The best name Claude has for this session, and nothing at all when it
		// has none (sessionname.go, sessiontitle.go): the title the payload
		// says it was given, then the name a person or a hook chose in the
		// session file, then Claude's own title for it, then Claude's own label.
		// The payload's comes first because it is the only one there on a
		// SessionStart, which runs before Claude writes the file, and the only
		// one there at all for a title a SessionStart hook gave (D-88). It rides
		// on two hooks of the nine, and that is enough: an empty name leaves the
		// stored one alone.
		//
		// What is not here is the placeholder Claude stamps on an unnamed
		// session — `agent-notify-16`. Reporting it meant no display
		// could tell it from a name somebody chose, and it is a guess core can
		// make for every agent rather than one this program makes for one of
		// them. An agent-integration that has a name reports it; one that has
		// none reports nothing, and the guessing belongs to the SDK so that
		// every display does it identically (§A7.4.2, D-75, R24).
		Name: cmp.Or(
			strings.TrimSpace(payload.SessionTitle),
			claude.NameSomebodyChose(),
			title,
			claude.NameClaudeGenerated(),
		),
		// The session's own directory, and the payload's only when Claude is
		// too old to keep one. The payload's is the directory the tool ran in,
		// which moves with every `cd` the agent makes (sessionname.go).
		Cwd: cmp.Or(claude.Cwd, payload.Cwd),
		// What the session has spent and what is spending it, read out of
		// Claude's own transcript (usage.go). Every hook carries both for the
		// reason the name is carried: the record is being written anyway, so it
		// costs no extra write, and core counts only the responses it has not
		// counted before (§A7.4.3, §A7.4.4).
		Spent: said.Responses,
		Model: said.Model,
	}

	switch hookName {
	case "SessionStart":
		// [verified 2026-09-17] All four sources occur, and they are not the
		// same event. A compaction keeps the session id and is the agent
		// working mid-turn, not a session beginning — splitting on `source` is
		// this program's job, not core's (D-31).
		if payload.Source == "compact" {
			report.Event = session.AgentProgressed
			return report, true
		}
		report.Event = session.SessionStarted
		return report, true

	case "UserPromptSubmit":
		report.Event = session.UserSentPrompt
		report.Message = text(payload.Prompt)
		return report, true

	case "PostToolUse":
		// Fires inside subagents too, carrying the PARENT's session id, which
		// is exactly right: the pane is busy either way.
		report.Event = session.AgentProgressed
		return report, true

	case "SubagentStop":
		// Only when a subagent actually stopped.
		//
		// [verified 2026-09-17] Of 416 real SubagentStop payloads, 413 carried
		// an EMPTY agent_type and 245 of those fired within two seconds of a
		// Stop — it is Claude's own turn wrapper, not a subagent. Treating
		// those as progress knocks a correctly finished turn back to `working`
		// and holds it there while the human is the one being waited on, which
		// is the exact failure this whole system exists to prevent.
		//
		// The three that named a kind are the real ones, and for those this is
		// the event both previous implementations had to discard: an adapter
		// emitting states could not say "still working" without risking
		// overwriting something worse. An event vocabulary can.
		//
		// If the discriminator is ever wrong, it is wrong in the safe
		// direction: a missed `agent-progressed` costs nothing, because the
		// parent's next PostToolUse says working anyway.
		if strings.TrimSpace(payload.AgentType) == "" {
			return session.Report{}, false
		}
		report.Event = session.AgentProgressed
		report.Detail = "subagent-finished"
		return report, true

	case "PreCompact":
		report.Event = session.AgentProgressed
		report.Detail = "compacting"
		return report, true

	case "Stop":
		report.Event = session.TurnFinished
		report.Message = text(payload.LastAssistantMessage)
		return report, true

	case "StopFailure":
		// The answer the turn died partway through — "API Error: Your computer
		// went to sleep mid-response. The response above may be incomplete." —
		// which is what a person wants to read. Claude omits it when the turn
		// died before the assistant said anything, and then the raw detail is
		// better than silence.
		report.Event = session.TurnFailed
		report.Detail = kebab(payload.Error)
		report.Message = text(cmp.Or(payload.LastAssistantMessage, payload.ErrorDetails))
		return report, true

	case "Notification":
		// A kind nobody has heard of is ignored, because the kinds that do not
		// want you outnumber the kinds that do. A payload with no kind at all
		// is let through: that is what a permission prompt looked like before
		// the field existed.
		kind := payload.NotificationType
		if kind != "" && !NotificationsThatWantYou[kind] {
			return session.Report{}, false
		}
		report.Event = session.BlockedOnHuman
		report.Detail = kebab(kind)
		report.Message = text(payload.Message)
		return report, true

	case "SessionEnd":
		report.Event = session.SessionEnded
		report.Detail = endedBecause(payload.Reason)
		return report, true
	}

	// A hook this program was not written for, which includes every hook
	// Claude adds after this was written.
	return session.Report{}, false
}

// endedBecause turns Claude's reason into the shared detail vocabulary of
// §A5.6, so that a display keyed on `ended/superseded` works across agents.
//
// [verified 2026-09-17] `clear` and `resume` both mean a session was replaced,
// and the second is the one worth knowing about: resuming produces a decoy
// session with a fresh id that lives for about three seconds and then ends
// with reason `resume`. Filing it as superseded is what lets a picker leave it
// out of the sessions it offers.
func endedBecause(reason string) string {
	switch reason {
	case "clear", "resume":
		return "superseded"
	case "other", "prompt_input_exit", "":
		return "exited"
	default:
		return kebab(reason)
	}
}

// text is a message worth keeping, or nothing at all. Blank is not a message,
// and a nil pointer means "leave what is stored alone" rather than "clear it".
func text(message string) *string {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// kebab puts an agent's own word into the shared detail namespace, which is
// lowercase-kebab by convention (§A5.8). The convention is not enforced by
// core, so it is enforced here.
func kebab(word string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(word)), "_", "-")
}
