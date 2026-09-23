package main

import (
	"encoding/json"
	"strings"
	"testing"

	agentnotify "github.com/lassoColombo/agent-notify"
)

// Every payload below is a real one, copied out of a capture of two days of
// ordinary use. Nothing here is invented, which matters: a hand-written fixture
// tests what the author believed the agent sends.

func translate(t *testing.T, hook, raw string) (agentnotify.Report, bool) {
	t.Helper()
	var payload Payload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("the fixture is not JSON: %v", err)
	}
	return Translate(hook, payload, ClaudeSession{}, ClaudeTranscript{}, "")
}

func mustTranslate(t *testing.T, hook, raw string) agentnotify.Report {
	t.Helper()
	report, worth := translate(t, hook, raw)
	if !worth {
		t.Fatalf("%s was ignored", hook)
	}
	return report
}

const (
	realStart = `{"session_id":"74e70404-9b2a-4b0e-b53e-cc5e20ba423a",
	  "transcript_path":"/Users/x/.claude/projects/p/74e70404.jsonl",
	  "cwd":"/Users/x/projects/work/lenny","hook_event_name":"SessionStart",
	  "source":"startup","model":"claude-opus-5[1m]"}`

	realCompact = `{"session_id":"8d95dac4-57c8-44ef-8046-0bad17e48e77",
	  "transcript_path":"/Users/x/.claude/projects/p/8d95dac4.jsonl",
	  "cwd":"/Users/x/projects/work/lenny","hook_event_name":"SessionStart","source":"compact"}`

	realPrompt = `{"session_id":"0e2804a7","transcript_path":"/t.jsonl",
	  "cwd":"/Users/x/projects/personal/agent-notify","prompt_id":"b8787160",
	  "permission_mode":"auto","hook_event_name":"UserPromptSubmit","prompt":"commit and proceed with m7"}`

	realStop = `{"session_id":"0e2804a7","transcript_path":"/t.jsonl",
	  "cwd":"/Users/x/projects/personal/agent-notify","prompt_id":"2e87792c","permission_mode":"auto",
	  "effort":{"level":"xhigh"},"hook_event_name":"Stop","stop_hook_active":false,
	  "last_assistant_message":"  Agreed, and the justification is withdrawn.  ",
	  "background_tasks":[],"session_crons":[]}`

	realPermission = `{"session_id":"0e2804a7","transcript_path":"/t.jsonl","cwd":"/Users/x/p",
	  "prompt_id":"09145818","hook_event_name":"Notification",
	  "message":"Claude needs your permission to use Bash","notification_type":"permission_prompt"}`

	realIdle = `{"session_id":"0e2804a7","transcript_path":"/t.jsonl","cwd":"/Users/x/p",
	  "prompt_id":"09145818","hook_event_name":"Notification",
	  "message":"Claude is waiting for your input","notification_type":"idle_prompt"}`

	realTurnWrapper = `{"session_id":"0e2804a7","transcript_path":"/t.jsonl","cwd":"/Users/x/p",
	  "prompt_id":"2e87792c","permission_mode":"auto","agent_id":"abd259791dc0921ef","agent_type":"",
	  "effort":{"level":"xhigh"},"hook_event_name":"SubagentStop","stop_hook_active":false,
	  "agent_transcript_path":"/sub.jsonl","last_assistant_message":"commit and proceed with m7",
	  "background_tasks":[],"session_crons":[]}`

	realSubagent = `{"session_id":"0e2804a7","transcript_path":"/t.jsonl","cwd":"/Users/x/p",
	  "prompt_id":"2e87792c","permission_mode":"auto","agent_id":"aa11","agent_type":"general-purpose",
	  "hook_event_name":"SubagentStop","last_assistant_message":"found it in store.go"}`

	realEnd = `{"session_id":"74e70404","transcript_path":"/t.jsonl","cwd":"/Users/x/p",
	  "prompt_id":"72638480","hook_event_name":"SessionEnd","reason":"other"}`

	realPreCompact = `{"session_id":"8d95dac4","transcript_path":"/t.jsonl","cwd":"/Users/x/p",
	  "prompt_id":"f8850d01","hook_event_name":"PreCompact","trigger":"manual","custom_instructions":null}`

	realPostTool = `{"session_id":"0e2804a7","transcript_path":"/t.jsonl","cwd":"/Users/x/p",
	  "prompt_id":"2e87792c","permission_mode":"auto","hook_event_name":"PostToolUse",
	  "tool_name":"Bash","tool_use_id":"tu1","tool_input":{},"tool_response":{},"duration_ms":240}`
)

func TestATurnMovesThroughWorkingIntoFinished(t *testing.T) {
	prompt := mustTranslate(t, "UserPromptSubmit", realPrompt)
	if prompt.Event != agentnotify.UserSentPrompt {
		t.Errorf("a prompt is %q", prompt.Event)
	}
	if prompt.Message == nil || *prompt.Message != "commit and proceed with m7" {
		t.Errorf("the prompt text was not kept: %v", prompt.Message)
	}

	tool := mustTranslate(t, "PostToolUse", realPostTool)
	if tool.Event != agentnotify.AgentProgressed || tool.Message != nil {
		t.Errorf("a tool call is %q saying %v; hundreds fire a turn and saying nothing is what lets them coalesce",
			tool.Event, tool.Message)
	}

	stop := mustTranslate(t, "Stop", realStop)
	if stop.Event != agentnotify.TurnFinished {
		t.Errorf("a stop is %q", stop.Event)
	}
	if stop.Message == nil || *stop.Message != "Agreed, and the justification is withdrawn." {
		t.Errorf("the answer was not kept or not trimmed: %v", stop.Message)
	}

	// Reducing them in order is the M7 "done when".
	kernel := agentnotify.Kernel("")
	for _, report := range []agentnotify.Report{prompt, tool, stop} {
		kernel = agentnotify.Reduce(kernel, report.Event)
	}
	if kernel != agentnotify.FinishedATurn {
		t.Errorf("a turn ended in %q, want finished-a-turn", kernel)
	}
}

func TestAPermissionPromptBlocksOnYouAndAnIdleNudgeDoesNothing(t *testing.T) {
	asking := mustTranslate(t, "Notification", realPermission)
	if asking.Event != agentnotify.BlockedOnHuman {
		t.Errorf("a permission prompt is %q", asking.Event)
	}
	if asking.Detail != "permission-prompt" {
		t.Errorf("detail = %q, want the kind in the shared kebab namespace", asking.Detail)
	}
	if asking.Message == nil || *asking.Message != "Claude needs your permission to use Bash" {
		t.Errorf("the ask was not kept: %v", asking.Message)
	}
	if got := agentnotify.Reduce(agentnotify.Working, asking.Event); got != agentnotify.BlockedOnYou {
		t.Errorf("a permission prompt left a working session at %q", got)
	}

	if _, worth := translate(t, "Notification", realIdle); worth {
		t.Error("an idle nudge escalated; 87 of 151 real notifications are these")
	}
}

// TestTheTurnWrapperIsNotASubagent is the mapping the replay caught, and the
// one worth the most.
//
// 413 of 416 real SubagentStop payloads carry an empty agent_type, and 245 of
// those fire within two seconds of a Stop. Treating them as progress holds a
// finished turn at `working` while the human is the one being waited on.
func TestTheTurnWrapperIsNotASubagent(t *testing.T) {
	if _, worth := translate(t, "SubagentStop", realTurnWrapper); worth {
		t.Error("a SubagentStop with no agent_type was treated as a subagent finishing")
	}

	real := mustTranslate(t, "SubagentStop", realSubagent)
	if real.Event != agentnotify.AgentProgressed || real.Detail != "subagent-finished" {
		t.Errorf("a real subagent finishing is %q/%q", real.Event, real.Detail)
	}
	if real.Message != nil {
		t.Errorf("a subagent's last words were stored as the session's: %v", real.Message)
	}

	// The thing this buys: a finished turn survives its own wrapper.
	kernel := agentnotify.FinishedATurn
	if _, worth := translate(t, "SubagentStop", realTurnWrapper); worth {
		kernel = agentnotify.Reduce(kernel, agentnotify.AgentProgressed)
	}
	if kernel != agentnotify.FinishedATurn {
		t.Errorf("a finished turn was knocked back to %q by its own wrapper", kernel)
	}
}

// TestCompactionIsWorkingAndNotABeginning is D-31 in the adapter: splitting
// SessionStart on its source is this program's job, not core's.
func TestCompactionIsWorkingAndNotABeginning(t *testing.T) {
	compact := mustTranslate(t, "SessionStart", realCompact)
	if compact.Event != agentnotify.AgentProgressed {
		t.Errorf("a compaction is %q, want agent-progressed: it keeps the session id and is the "+
			"agent working mid-turn", compact.Event)
	}
	if got := agentnotify.Reduce(agentnotify.Working, compact.Event); got != agentnotify.Working {
		t.Errorf("a compaction left a working session at %q", got)
	}

	for _, source := range []string{"startup", "resume", "clear"} {
		raw := `{"session_id":"s","hook_event_name":"SessionStart","source":"` + source + `"}`
		report := mustTranslate(t, "SessionStart", raw)
		if report.Event != agentnotify.SessionStarted {
			t.Errorf("source %q is %q, want session-started", source, report.Event)
		}
	}

	precompact := mustTranslate(t, "PreCompact", realPreCompact)
	if precompact.Event != agentnotify.AgentProgressed || precompact.Detail != "compacting" {
		t.Errorf("a compaction beginning is %q/%q", precompact.Event, precompact.Detail)
	}
}

// TestEndingSaysWhy maps Claude's reasons onto the shared detail vocabulary,
// including the decoy session that resuming produces.
func TestEndingSaysWhy(t *testing.T) {
	cases := map[string]string{
		"other":             "exited",
		"prompt_input_exit": "exited",
		"clear":             "superseded",
		"resume":            "superseded",
		"something_new":     "something-new",
		"":                  "exited",
	}
	for reason, want := range cases {
		raw := `{"session_id":"s","hook_event_name":"SessionEnd","reason":"` + reason + `"}`
		report := mustTranslate(t, "SessionEnd", raw)
		if report.Event != agentnotify.SessionEnded {
			t.Errorf("reason %q is %q", reason, report.Event)
		}
		if report.Detail != want {
			t.Errorf("reason %q became detail %q, want %q", reason, report.Detail, want)
		}
	}
	if got := mustTranslate(t, "SessionEnd", realEnd); got.Detail != "exited" {
		t.Errorf("a real SessionEnd became %q", got.Detail)
	}
}

// TestTheSessionsDirectoryBeatsTheHooks is the whole of why CwdChanged is not
// subscribed to any more. The payload's cwd is wherever the tool happened to
// run, so a display falling back to the last component of it renamed the
// session on every `cd`; Claude's own file holds the one the session started
// in.
func TestTheSessionsDirectoryBeatsTheHooks(t *testing.T) {
	var payload Payload
	if err := json.Unmarshal([]byte(realPostTool), &payload); err != nil {
		t.Fatalf("the fixture is not JSON: %v", err)
	}

	moved, _ := Translate("PostToolUse", payload,
		ClaudeSession{Cwd: "/Users/x/projects/personal/agent-notify"}, ClaudeTranscript{}, "")
	if moved.Cwd != "/Users/x/projects/personal/agent-notify" {
		t.Errorf("cwd = %q, want the session's own", moved.Cwd)
	}

	// And the payload's when Claude is too old to keep a file, because no cwd
	// at all leaves a display with the session id to paint.
	old, _ := Translate("PostToolUse", payload, ClaudeSession{}, ClaudeTranscript{}, "")
	if old.Cwd != payload.Cwd {
		t.Errorf("cwd = %q, want the payload's as the fallback", old.Cwd)
	}
}

// TestAFailedTurnBreaks reads a real StopFailure, which is the whole point of
// it. [captured 2026-09-18] The field names this used to read — `error_type`
// and `error_message` — came from the prior Rust adapter and were marked
// [assumed] because no payload carrying them had ever been seen. None ever
// was: Claude sends `error` and `last_assistant_message`, so every turn-failed
// this program wrote arrived with an empty detail and no message at all.
func TestAFailedTurnBreaks(t *testing.T) {
	const real = `{"session_id":"0e2804a7-a0e2-4a57-90ee-f7cc5f0c9cc6",
	  "transcript_path":"/Users/x/.claude/projects/-Users-x-p/0e2804a7.jsonl",
	  "cwd":"/Users/x/p","prompt_id":"af5bfcae","effort":{"level":"xhigh"},
	  "hook_event_name":"StopFailure","error":"server_error",
	  "last_assistant_message":"API Error: Your computer went to sleep mid-response. The response above may be incomplete."}`
	report := mustTranslate(t, "StopFailure", real)
	if report.Event != agentnotify.TurnFailed {
		t.Errorf("a failed turn is %q", report.Event)
	}
	if report.Detail != "server-error" {
		t.Errorf("detail = %q, want the kind of failure it was", report.Detail)
	}
	if report.Message == nil || !strings.HasPrefix(*report.Message, "API Error: Your computer went to sleep") {
		t.Errorf("message = %v, want what the turn died partway through saying", report.Message)
	}
	if got := agentnotify.Reduce(agentnotify.Working, report.Event); got != agentnotify.Broke {
		t.Errorf("a failed turn left the session at %q", got)
	}
}

// TestAFailedTurnWithNothingSaidFallsBackToTheDetails is the other shape
// Claude sends: it omits `last_assistant_message` when the turn died before the
// assistant said anything, and the raw detail is better than silence.
func TestAFailedTurnWithNothingSaidFallsBackToTheDetails(t *testing.T) {
	raw := `{"session_id":"s","hook_event_name":"StopFailure",
	         "error":"request_too_large","error_details":"request_too_large: prompt is too long"}`
	report := mustTranslate(t, "StopFailure", raw)
	if report.Detail != "request-too-large" {
		t.Errorf("detail = %q", report.Detail)
	}
	if report.Message == nil || *report.Message != "request_too_large: prompt is too long" {
		t.Errorf("message = %v, want the details Claude did send", report.Message)
	}
}

func TestWhatIsIgnoredAndWhy(t *testing.T) {
	// A session nobody can name cannot be written about.
	for _, raw := range []string{`{}`, `{"session_id":""}`, `{"session_id":"  "}`} {
		if _, worth := translate(t, "Stop", raw); worth {
			t.Errorf("%s was recorded", raw)
		}
	}
	// A hook this program was not written for, which includes every hook
	// Claude adds after today.
	for _, hook := range []string{"PreToolUse", "SomethingNew", ""} {
		if _, worth := translate(t, hook, realStop); worth {
			t.Errorf("%q was mapped", hook)
		}
	}
	// A notification kind nobody has heard of: the kinds that do not want you
	// outnumber the kinds that do.
	raw := `{"session_id":"s","notification_type":"quota_warning"}`
	if _, worth := translate(t, "Notification", raw); worth {
		t.Error("an unknown notification kind escalated")
	}
	// ...but one with no kind at all is let through: that is what a permission
	// prompt looked like before the field existed.
	for _, raw := range []string{`{"session_id":"s"}`, `{"session_id":"s","notification_type":""}`} {
		if report, worth := translate(t, "Notification", raw); !worth ||
			report.Event != agentnotify.BlockedOnHuman {
			t.Errorf("%s was ignored", raw)
		}
	}
}

func TestEverySubscribedHookIsMapped(t *testing.T) {
	// A hook installed but never mapped is a process Claude waits for that
	// does nothing at all.
	unmapped := []string{}
	for _, hook := range SubscribedHooks {
		raw := `{"session_id":"s","agent_type":"general-purpose","source":"startup","reason":"other"}`
		if _, worth := translate(t, hook, raw); !worth {
			unmapped = append(unmapped, hook)
		}
	}
	if len(unmapped) != 0 {
		t.Errorf("subscribed but unmapped: %v", unmapped)
	}
}
