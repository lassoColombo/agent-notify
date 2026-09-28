// Package main is codex's agent-integration.
//
// Everything peculiar to codex lives in this file: which hook means what, and
// where in the payload the text is. Everything after it is hook.Record, which
// is identical for every agent, and core knows nothing about this program.
package main

import (
	"encoding/json"
	"strings"

	"github.com/lassoColombo/agent-notify/session"
)

// AgentName is what every record this writes says it is, and what the user's
// `[agent.codex]` table is keyed on.
const AgentName = "codex"

// SubscribedHooks is every hook this asks for, spelled as codex spells it.
//
// Seven of the twelve. What is deliberately absent, and why:
//
//   - **PreToolUse.** PostToolUse already says the session is working, and
//     subscribing to both would put a "working" write in flight at the moment a
//     PermissionRequest is writing "blocked on you" — measured 49ms apart on a
//     real run, which is well inside the window where two short-lived processes
//     can reach the lock out of order. The one that arrives second wins, and
//     losing that race paints over a permission prompt.
//   - **SubagentStart and SubagentStop.** Neither fired once in the recorded
//     traffic, so there is nothing to map them against — and the one agent
//     where this hook *was* observed turned out to fire it for its own turn
//     wrapper, which had to be discarded (agent-notify-claude, M7). An
//     unverified mapping of a hook that can knock a finished turn back to
//     working is not worth the guess: a subagent's own tool calls already mark
//     the parent working.
//   - **PreCompact and PostCompact.** A compaction that happens mid-turn is
//     already working, so they add nothing; a compaction you asked for from an
//     idle session would leave PostCompact's "working" sitting there with
//     nothing running behind it, because codex fires nothing afterwards.
//     Measured: PreCompact, PostCompact 19s later, then silence for two
//     minutes. A state that is right for ten seconds and wrong for ten minutes
//     is worse than no state at all.
var SubscribedHooks = []string{
	"SessionStart",
	"UserPromptSubmit",
	"PostToolUse",
	"PermissionRequest",
	"Stop",
	"Interrupt",
	"SessionEnd",
}

// Payload is the part of codex's hook input this reads.
//
// Everything is optional. Codex sends a different subset per hook and adds
// fields between releases, so nothing here may be required and nothing may fail
// to decode.
type Payload struct {
	// HookEventName is on every payload, which is why this program takes the
	// event from the body rather than from its arguments: one command string
	// serves all seven subscriptions, and no argument can disagree with the key
	// it was registered under.
	HookEventName string `json:"hook_event_name"`

	SessionID string `json:"session_id"`
	Cwd       string `json:"cwd"`

	// Reason is SessionEnd's. Only "other" has ever been seen.
	Reason string `json:"reason"`

	Prompt               string `json:"prompt"`
	LastAssistantMessage string `json:"last_assistant_message"`

	ToolName  string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input"`

	Model string `json:"model"`
	// TranscriptPath is the rollout this thread's accounting is written to,
	// which usage.go reads. Codex calls it a transcript; it is the same file.
	TranscriptPath string `json:"transcript_path"`
}

// Translate is the whole mapping, and it is pure: no store, no filesystem, no
// codex needed to test it. That is why the name and the spending are parameters
// rather than lookups — each is a file read (threadname.go and usage.go), and
// deciding which hooks carry them is policy, which belongs where policy is
// tested.
//
// Every hook carries it. The report is being written anyway, so the name costs
// no extra write, and a thread codex renames mid-session lands on the next tool
// call instead of waiting for one that happens to be special.
//
// The bool is false for a hook that should be reported as nothing at all.
func Translate(payload Payload, whatCodexCallsIt string, spent Spending) (session.Report, bool) {
	hook := payload.HookEventName
	if payload.SessionID == "" {
		// Nothing can be written about a session nobody can name.
		return session.Report{}, false
	}

	report := session.Report{
		Key:  session.Key{Agent: AgentName, SessionID: payload.SessionID},
		Name: whatCodexCallsIt,
		Cwd:  payload.Cwd,
		// The model comes off the payload, which carries it on every hook, and
		// what the thread has spent off codex's own rollout (usage.go). Both ride
		// along for the reason the name does: the record is being written anyway,
		// so they cost no extra write, and core counts only the responses it has
		// not counted before (§A7.4.3, §A7.4.4).
		Model: payload.Model,
		Spent: spent.Responses,
	}

	switch hook {
	case "SessionStart":
		// Identity only. The reducer leaves a live session where it is, so a
		// codex release that starts firing this for a compaction or a fork
		// costs nothing here and needs no case of its own (D-31): asserting
		// that a session exists is idempotent.
		//
		// Codex's `source` — startup or resume in everything observed — is
		// deliberately not read. Claude's SessionStart carries one too and
		// sets no detail from it, and M11's whole point is that two agents are
		// indistinguishable in kind on one bar: a codex session reading
		// `idle/startup` beside a claude one reading `idle` would be a
		// difference with nothing behind it.
		report.Event = session.SessionStarted

	case "UserPromptSubmit":
		report.Event = session.UserSentPrompt
		report.Message = said(payload.Prompt)

	case "PostToolUse":
		// Hundreds a turn. Saying the same thing every time is what lets the
		// session-watcher coalesce them into one render.
		report.Event = session.AgentProgressed

	case "PermissionRequest":
		// The one moment codex genuinely needs you, and the only hook here that
		// is *synchronous*: codex reads a handler's stdout as a verdict on the
		// tool and waits for it. That is why main prints nothing on stdout —
		// saying nothing is how a handler declines to decide — and why the hook
		// path may not block on anything (R1).
		//
		// It clears itself: approving runs the tool, and the PostToolUse behind
		// it says working again.
		report.Event = session.BlockedOnHuman
		report.Detail = "permission-prompt"
		report.Message = said(whatNeedsApproving(payload))

	case "Stop":
		report.Event = session.TurnFinished
		report.Message = said(payload.LastAssistantMessage)

	case "Interrupt":
		// The hook this integration cannot do without. Stop and Interrupt are
		// mutually exclusive paths in codex — an interrupted turn never reaches
		// the Stop call — so ignoring this leaves a session reading "working"
		// for as long as nobody touches it. Measured: an interrupt, then
		// silence, then SessionEnd two minutes later.
		//
		// It carries no message, because codex sends none and the person who
		// pressed Esc knows why they pressed it.
		report.Event = session.TurnInterrupted
		report.Detail = "interrupted"

	case "SessionEnd":
		report.Event = session.SessionEnded
		report.Detail = endedBecause(payload.Reason)

	default:
		return session.Report{}, false
	}
	return report, true
}

// said is text worth keeping, or nothing at all. A blank message is not an
// empty message: nil leaves whatever the session last said in place, and "" is
// a deliberate erasure nothing here ever means.
func said(text string) *string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// approvalFields are the parts of a tool's arguments worth showing a person,
// best first.
//
// `command` beats `description` because a shell call's literal command says
// more than codex's summary of it: the three real permission prompts recorded
// here all carried "Do you want to allow writing the requested file outside the
// workspace?" as their description, which is the same sentence whichever file
// it was, while the command named the file.
var approvalFields = []string{"command", "description"}

// whatNeedsApproving is the sentence to put on a bar for a tool waiting on a
// human. Claude hands over a finished sentence; codex hands over a tool name
// and that tool's own arguments, so this is that sentence.
func whatNeedsApproving(payload Payload) string {
	var input map[string]json.RawMessage
	_ = json.Unmarshal(payload.ToolInput, &input)

	for _, field := range approvalFields {
		raw, present := input[field]
		if !present {
			continue
		}
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			continue
		}
		if text = strings.TrimSpace(text); text != "" {
			return "needs approval: " + text
		}
	}
	// The bare tool name is still worth having when its arguments are a shape
	// nobody can summarise.
	tool := payload.ToolName
	if tool == "" {
		tool = "a tool"
	}
	return "needs approval for " + tool
}

// endedBecause turns codex's reason into the shared detail vocabulary of
// §A5.8, so that a display keyed on `ended/superseded` works across agents
// rather than once per agent.
//
// Every SessionEnd recorded here carried `other`, which says nothing and is
// what claude sends for an ordinary exit too. `clear` and `resume` have never
// been observed from codex; they are here because if it ever sends them they
// mean what they mean everywhere, and the shared word is the point.
func endedBecause(reason string) string {
	switch reason {
	case "clear", "resume":
		return "superseded"
	case "other", "":
		return "exited"
	default:
		return kebab(reason)
	}
}

// kebab makes a detail out of whatever word the agent used. Details share one
// unprefixed namespace across agents (§A5.8), so `permission-prompt` means the
// same thing whoever wrote it; the spelling convention is enforced here, in the
// adapter, rather than by core.
func kebab(word string) string {
	var out strings.Builder
	for i, letter := range word {
		switch {
		case letter >= 'A' && letter <= 'Z':
			if i > 0 {
				out.WriteByte('-')
			}
			out.WriteRune(letter + 32)
		case letter == '_' || letter == ' ':
			out.WriteByte('-')
		default:
			out.WriteRune(letter)
		}
	}
	return out.String()
}
