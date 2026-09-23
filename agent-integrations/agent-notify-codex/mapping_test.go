package main

import (
	"encoding/json"
	"testing"

	agentnotify "github.com/lassoColombo/agent-notify"
)

// The fixtures are real payloads, recorded from codex 0.154.0 and trimmed only
// of the long paths. A mapping tested against payloads somebody wrote by hand
// is a mapping tested against what the author believed the agent sends.

const sessionID = "01a0a6f4-96d4-7182-b91b-eff9ecf25adf"

func mapped(t *testing.T, raw string) (agentnotify.Report, bool) {
	t.Helper()
	var payload Payload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("the fixture is not valid JSON: %v", err)
	}
	return Translate(payload, "", Spending{})
}

func mustMap(t *testing.T, raw string) agentnotify.Report {
	t.Helper()
	report, worth := mapped(t, raw)
	if !worth {
		t.Fatalf("this payload was discarded: %s", raw)
	}
	return report
}

const recordedSessionStart = `{
  "session_id": "01a0a6f4-96d4-7182-b91b-eff9ecf25adf",
  "transcript_path": "/Users/x/.codex/sessions/2026/09/15/rollout.jsonl",
  "cwd": "/Users/x/projects/thing",
  "hook_event_name": "SessionStart",
  "model": "gpt-5.6-terra",
  "permission_mode": "default",
  "source": "startup"
}`

const recordedUserPrompt = `{
  "session_id": "01a0a6f4-96d4-7182-b91b-eff9ecf25adf",
  "turn_id": "01a0a6f5-4f35-7ea1-aa1e-03b93cf9c20b",
  "cwd": "/Users/x/projects/thing",
  "hook_event_name": "UserPromptSubmit",
  "model": "gpt-5.6-terra",
  "permission_mode": "default",
  "prompt": "Reply with exactly one word: ready"
}`

const recordedPostToolUse = `{
  "session_id": "01a0a6f4-96d4-7182-b91b-eff9ecf25adf",
  "turn_id": "01a0a6f9-7022-7283-8439-224ebbb4ee77",
  "cwd": "/Users/x/projects/thing",
  "hook_event_name": "PostToolUse",
  "model": "gpt-5.6-terra",
  "permission_mode": "default",
  "tool_name": "Bash",
  "tool_input": {"command": "pwd"},
  "tool_response": "/Users/x/projects/thing\n",
  "tool_use_id": "exec-e0248edf"
}`

const recordedPermissionRequest = `{
  "session_id": "01a0a6f4-96d4-7182-b91b-eff9ecf25adf",
  "turn_id": "01a0a703-7d0f-74a1-8fdf-d676f435446b",
  "cwd": "/Users/x/projects/thing",
  "hook_event_name": "PermissionRequest",
  "model": "gpt-5.6-terra",
  "permission_mode": "default",
  "tool_name": "Bash",
  "tool_input": {
    "command": "echo hello > /Users/x/codex-a4-outside.txt",
    "description": "Do you want to allow writing the requested file outside the workspace?"
  }
}`

const recordedStop = `{
  "session_id": "01a0a6f4-96d4-7182-b91b-eff9ecf25adf",
  "turn_id": "01a0a6f5-4f35-7ea1-aa1e-03b93cf9c20b",
  "cwd": "/Users/x/projects/thing",
  "hook_event_name": "Stop",
  "model": "gpt-5.6-terra",
  "permission_mode": "default",
  "stop_hook_active": false,
  "last_assistant_message": "ready"
}`

const recordedInterrupt = `{
  "session_id": "01a0a6f4-96d4-7182-b91b-eff9ecf25adf",
  "turn_id": "01a0a702-32ac-7c72-95a2-179f59605d60",
  "cwd": "/Users/x/projects/thing",
  "hook_event_name": "Interrupt",
  "model": "gpt-5.6-terra",
  "permission_mode": "default"
}`

const recordedSessionEnd = `{
  "session_id": "01a0a6f4-96d4-7182-b91b-eff9ecf25adf",
  "cwd": "/Users/x/projects/thing",
  "hook_event_name": "SessionEnd",
  "reason": "other"
}`

func TestTheMapping(t *testing.T) {
	for _, want := range []struct {
		raw     string
		event   agentnotify.Event
		detail  string
		message string
		why     string
	}{
		{recordedSessionStart, agentnotify.SessionStarted, "", "",
			"a session announcing itself, with no detail: claude sends none either"},
		{recordedUserPrompt, agentnotify.UserSentPrompt, "", "Reply with exactly one word: ready",
			"what you typed is what a bar shows"},
		{recordedPostToolUse, agentnotify.AgentProgressed, "", "",
			"a tool ran; it is working, and it says nothing new"},
		{recordedPermissionRequest, agentnotify.BlockedOnHuman, "permission-prompt",
			"needs approval: echo hello > /Users/x/codex-a4-outside.txt",
			"the command, not codex's summary of it"},
		{recordedStop, agentnotify.TurnFinished, "", "ready", "the answer"},
		{recordedInterrupt, agentnotify.TurnInterrupted, "interrupted", "",
			"you stopped it; nothing was produced and nothing is said"},
		{recordedSessionEnd, agentnotify.SessionEnded, "exited", "", "gone"},
	} {
		report := mustMap(t, want.raw)
		if report.Event != want.event {
			t.Errorf("%s: event = %q, want %q", want.why, report.Event, want.event)
		}
		if report.Detail != want.detail {
			t.Errorf("%s: detail = %q, want %q", want.why, report.Detail, want.detail)
		}
		got := ""
		if report.Message != nil {
			got = *report.Message
		}
		if got != want.message {
			t.Errorf("%s: message = %q, want %q", want.why, got, want.message)
		}
		if report.Key.Agent != AgentName || report.Key.SessionID != sessionID {
			t.Errorf("%s: key = %v", want.why, report.Key)
		}
		if report.Cwd != "/Users/x/projects/thing" {
			t.Errorf("%s: cwd = %q — codex marks it required on every hook, so it rides on "+
				"every report and a session that moved corrects itself", want.why, report.Cwd)
		}
	}
}

// TestTheEventComesFromTheBody is the one structural difference from claude:
// codex writes the hook's name into every payload, so one command string serves
// all seven subscriptions and no argument can disagree with the key it was
// registered under.
func TestTheEventComesFromTheBody(t *testing.T) {
	if _, worth := mapped(t, `{"session_id":"x"}`); worth {
		t.Error("a payload with no hook_event_name was mapped to something")
	}
	report := mustMap(t, `{"session_id":"x","hook_event_name":"Stop"}`)
	if report.Event != agentnotify.TurnFinished {
		t.Errorf("event = %q", report.Event)
	}
}

func TestAPayloadNobodyCanNameIsDiscarded(t *testing.T) {
	// Nothing can be written about a session with no id, and inventing one
	// would put a record in the store that nothing will ever update again.
	for _, raw := range []string{
		`{"hook_event_name":"Stop"}`,
		`{"hook_event_name":"Stop","session_id":""}`,
	} {
		if _, worth := mapped(t, raw); worth {
			t.Errorf("mapped a payload with no session: %s", raw)
		}
	}
}

// TestTheHooksWeDoNotWant is the list in SubscribedHooks, asserted rather than
// described: subscribing to one of these would be a change that has to be
// argued for.
func TestTheHooksWeDoNotWant(t *testing.T) {
	for _, hook := range []string{"PreToolUse", "PreCompact", "PostCompact", "SubagentStart", "SubagentStop"} {
		raw := `{"session_id":"x","hook_event_name":"` + hook + `"}`
		if _, worth := mapped(t, raw); worth {
			t.Errorf("%s was mapped to something; see SubscribedHooks for why it is not", hook)
		}
	}

	// And the subscription list and the mapping cannot drift apart: every hook
	// asked for must translate, or codex is being made to run a program that
	// does nothing.
	for _, hook := range SubscribedHooks {
		raw := `{"session_id":"x","hook_event_name":"` + hook + `"}`
		if _, worth := mapped(t, raw); !worth {
			t.Errorf("%s is subscribed but maps to nothing", hook)
		}
	}
}

// TestAnInterruptedTurnDoesNotLeaveASessionWorking is the reason this
// integration subscribes to Interrupt at all. Stop and Interrupt are mutually
// exclusive paths in codex: an interrupted turn never reaches the Stop call.
func TestAnInterruptedTurnDoesNotLeaveASessionWorking(t *testing.T) {
	kernel := agentnotify.Reduce("", mustMap(t, recordedSessionStart).Event)
	kernel = agentnotify.Reduce(kernel, mustMap(t, recordedUserPrompt).Event)
	if kernel != agentnotify.Working {
		t.Fatalf("after a prompt the session is %q", kernel)
	}
	kernel = agentnotify.Reduce(kernel, mustMap(t, recordedInterrupt).Event)
	if kernel != agentnotify.Idle {
		t.Errorf("after an interrupt the session is %q, want idle — not finished-a-turn, which "+
			"promises an answer to read, and not broke, which is loud about something you chose",
			kernel)
	}
}

// TestInterruptingAPermissionPromptAnswersIt: the recorded traffic has exactly
// this — a prompt nobody wanted to answer, ended with Esc.
func TestInterruptingAPermissionPromptAnswersIt(t *testing.T) {
	kernel := agentnotify.Reduce(agentnotify.Working, mustMap(t, recordedPermissionRequest).Event)
	if kernel != agentnotify.BlockedOnYou {
		t.Fatalf("a permission request left the session %q", kernel)
	}
	if kernel = agentnotify.Reduce(kernel, mustMap(t, recordedInterrupt).Event); kernel != agentnotify.Idle {
		t.Errorf("interrupting a permission prompt left the session %q", kernel)
	}
}

// TestAPermissionRequestClearsItself: approving runs the tool, and the
// PostToolUse behind it says working again. Nothing has to remember that a
// prompt was outstanding.
func TestAPermissionRequestClearsItself(t *testing.T) {
	kernel := agentnotify.Reduce(agentnotify.Working, mustMap(t, recordedPermissionRequest).Event)
	kernel = agentnotify.Reduce(kernel, mustMap(t, recordedPostToolUse).Event)
	if kernel != agentnotify.Working {
		t.Errorf("after the approved tool ran the session is %q, want working", kernel)
	}
}

func TestWhatNeedsApproving(t *testing.T) {
	for _, want := range []struct {
		input string
		tool  string
		text  string
		why   string
	}{
		{`{"command":"rm -rf /tmp/x","description":"Do you want to allow this?"}`, "Bash",
			"needs approval: rm -rf /tmp/x",
			"the literal command beats codex's summary: all three recorded descriptions were " +
				"the same sentence whichever file it was"},
		{`{"description":"Do you want to allow writing outside the workspace?"}`, "Write",
			"needs approval: Do you want to allow writing outside the workspace?",
			"the description when there is no command"},
		{`{"patch":{"a":1}}`, "ApplyPatch", "needs approval for ApplyPatch",
			"the bare tool name when its arguments are a shape nobody can summarise"},
		{`{}`, "", "needs approval for a tool", "and something even with no tool name"},
		{`{"command":"   "}`, "Bash", "needs approval for Bash", "blank is not a command"},
	} {
		got := whatNeedsApproving(Payload{ToolName: want.tool, ToolInput: json.RawMessage(want.input)})
		if got != want.text {
			t.Errorf("%s:\n got %q\nwant %q", want.why, got, want.text)
		}
	}
}

// TestAStopWithNothingSaidLeavesThePreviousMessage: two of the fourteen real
// Stop payloads carried a null last_assistant_message. Writing "" would erase
// what the session last said; nil leaves it alone.
func TestAStopWithNothingSaidLeavesThePreviousMessage(t *testing.T) {
	for _, raw := range []string{
		`{"session_id":"x","hook_event_name":"Stop","last_assistant_message":null}`,
		`{"session_id":"x","hook_event_name":"Stop"}`,
		`{"session_id":"x","hook_event_name":"Stop","last_assistant_message":"   "}`,
	} {
		report := mustMap(t, raw)
		if report.Message != nil {
			t.Errorf("%s produced a message of %q, want nothing at all", raw, *report.Message)
		}
	}
}

// TestTheModelIsReportedAsAFieldAndNowhereElse. It is the one thing codex says
// about itself that core keeps, and it comes off the payload, which carries it
// on every hook (§A7.4.4).
func TestTheModelIsReportedAsAFieldAndNowhereElse(t *testing.T) {
	report := mustMap(t, recordedStop)
	if report.Model == "" {
		t.Error("the report carries no model, and the payload it was built from has one")
	}
}

func TestEndedBecauseUsesTheSharedWords(t *testing.T) {
	for reason, want := range map[string]string{
		"other": "exited", "": "exited",
		"clear": "superseded", "resume": "superseded",
		"prompt_input_exit": "prompt-input-exit",
	} {
		if got := endedBecause(reason); got != want {
			t.Errorf("endedBecause(%q) = %q, want %q", reason, got, want)
		}
	}
}
