package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// realSessionFile is a copy of a live one, fields and all. [captured
// 2026-09-19, 2.1.236] The name is what an ordinary interactive session looks
// like: Claude stamped the directory and a random byte on it and nobody has
// renamed it.
const realSessionFile = `{"pid":38514,"sessionId":"4a152fd4-7280-4f7d-a172-3f5fef078783",
  "cwd":"/Users/x/projects/personal/agent-notify","startedAt":1789646635977,
  "version":"2.1.236","kind":"interactive","entrypoint":"cli",
  "messagingSocketPath":"/tmp/cc-socks/38514.sock",
  "name":"agent-notify-96","nameSource":"derived","nameSince":1789646635977,
  "status":"busy","updatedAt":1789847036765,"statusUpdatedAt":1789847036765}`

// sessionsDirectory points this program at a temporary ~/.claude and returns
// a hand to write files into it.
func sessionsDirectory(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	directory := filepath.Join(root, "sessions")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	t.Setenv(configDirectoryVariable, root)
	t.Setenv(pidVariable, "")
	return directory
}

func writeSession(t *testing.T, directory, pid string, session map[string]any) {
	t.Helper()
	content, err := json.Marshal(session)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, pid+".json"), content, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// TestARealSessionFileIsReadWhateverItsNameSource is the reader, which reads
// the name whatever `nameSource` says. `agent-notify-96` is the placeholder and
// is never reported (D-75), but that is Translate's decision, made where it is
// tested beside the others.
func TestARealSessionFileIsReadWhateverItsNameSource(t *testing.T) {
	directory := sessionsDirectory(t)
	if err := os.WriteFile(filepath.Join(directory, "38514.json"),
		[]byte(realSessionFile), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	known := WhatClaudeKnowsAboutThisSession("4a152fd4-7280-4f7d-a172-3f5fef078783")
	if known.Name != "agent-notify-96" || known.NameSource != "derived" {
		t.Errorf("name = %q from %q, want the placeholder as Claude filed it",
			known.Name, known.NameSource)
	}
	if known.Cwd != "/Users/x/projects/personal/agent-notify" {
		t.Errorf("cwd = %q, want the one the session started in", known.Cwd)
	}
}

// TestANameSomebodyMeantIsReported is a `/rename` and Claude's own summary,
// which is what the file looks like once a session has earned a name.
func TestANameSomebodyMeantIsReported(t *testing.T) {
	directory := sessionsDirectory(t)
	writeSession(t, directory, "38514", map[string]any{
		"sessionId": "s1", "name": "remove-the-name-command",
	})
	if got := WhatClaudeKnowsAboutThisSession("s1").Name; got != "remove-the-name-command" {
		t.Errorf("reported %q, want the name Claude holds", got)
	}
}

// TestTheRightFileIsFoundWithoutThePid is the fallback: no CLAUDE_PID, several
// live sessions, and the session id is what picks one out.
func TestTheRightFileIsFoundWithoutThePid(t *testing.T) {
	directory := sessionsDirectory(t)
	writeSession(t, directory, "111", map[string]any{
		"sessionId": "somebody-else", "name": "another-session",
	})
	writeSession(t, directory, "222", map[string]any{
		"sessionId": "ours", "name": "the-store",
	})

	if got := WhatClaudeKnowsAboutThisSession("ours").Name; got != "the-store" {
		t.Errorf("reported %q, want the-store", got)
	}
}

// TestAStalePidFileIsNotTrusted is why the session id is checked even on the
// fast path. Pids are reused, and `/clear` replaces the session inside a
// process without the file being renamed: a file that disagrees about which
// session it describes is describing a different one.
func TestAStalePidFileIsNotTrusted(t *testing.T) {
	directory := sessionsDirectory(t)
	t.Setenv(pidVariable, "38514")
	writeSession(t, directory, "38514", map[string]any{
		"sessionId": "the-one-before-clear", "name": "stale",
	})
	writeSession(t, directory, "99999", map[string]any{
		"sessionId": "ours", "name": "after-the-clear",
	})

	if got := WhatClaudeKnowsAboutThisSession("ours").Name; got != "after-the-clear" {
		t.Errorf("reported %q, want the file that agrees about the session", got)
	}
}

// TestNothingToReadIsNotAProblem is the contract with core: a Claude too old to
// keep the file, a directory that was moved, a session id nobody has heard of.
// Every one of them is an empty session, which means "leave what is stored
// alone" for the name and the payload's own cwd for the directory.
func TestNothingToReadIsNotAProblem(t *testing.T) {
	directory := sessionsDirectory(t)
	writeSession(t, directory, "38514", map[string]any{
		"sessionId": "ours", "name": "fine",
	})

	for _, one := range []struct {
		what      string
		sessionID string
		setup     func()
	}{
		{what: "a session nobody has a file for", sessionID: "unknown"},
		{what: "no session id at all", sessionID: "  "},
		{
			what: "no sessions directory", sessionID: "ours",
			setup: func() { t.Setenv(configDirectoryVariable, filepath.Join(directory, "nowhere")) },
		},
	} {
		t.Run(one.what, func(t *testing.T) {
			if one.setup != nil {
				one.setup()
			}
			if got := WhatClaudeKnowsAboutThisSession(one.sessionID); got != (ClaudeSession{}) {
				t.Errorf("reported %+v, want nothing", got)
			}
		})
	}
}

// TestNonsenseInTheFileIsIgnored: Claude is writing this file while we read it,
// and a half-written one must cost nothing but the name.
func TestNonsenseInTheFileIsIgnored(t *testing.T) {
	directory := sessionsDirectory(t)
	if err := os.WriteFile(filepath.Join(directory, "38514.json"),
		[]byte(`{"sessionId":"ours","na`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if got := WhatClaudeKnowsAboutThisSession("ours"); got != (ClaudeSession{}) {
		t.Errorf("reported %+v from a truncated file", got)
	}
}

// TestEveryHookCarriesTheName is the policy half, and it is in Translate so it
// can be tested without a filesystem: whatever the hook is, if there is a name
// the report carries it.
func TestEveryHookCarriesTheName(t *testing.T) {
	for _, one := range []struct{ hook, payload string }{
		{"SessionStart", realStart},
		{"UserPromptSubmit", realPrompt},
		{"PostToolUse", realPostTool},
		{"Stop", realStop},
		{"Notification", realPermission},
		{"SessionEnd", realEnd},
	} {
		t.Run(one.hook, func(t *testing.T) {
			var payload Payload
			if err := json.Unmarshal([]byte(one.payload), &payload); err != nil {
				t.Fatalf("the fixture is not JSON: %v", err)
			}
			report, worth := Translate(one.hook, payload,
				ClaudeSession{Name: "remove-the-name-command", NameSource: "user"},
				ClaudeTranscript{}, "")
			if !worth {
				t.Fatalf("%s was ignored", one.hook)
			}
			if report.Name != "remove-the-name-command" {
				t.Errorf("Name = %q on %s", report.Name, one.hook)
			}
		})
	}
}

// TestTheBestNameClaudeHasIsTheOneReported is the order, which is the whole of
// this program's naming policy: a name a person or a hook chose first, then
// Claude's title for the session, then Claude's label — and nothing at all when
// Claude has none of the three, because `agent-notify-16` is a guess and core
// makes the guesses (D-75). The payload's title, which ranks above all of them,
// is TestATitleTheSessionWasGivenIsReadFromThePayload.
func TestTheBestNameClaudeHasIsTheOneReported(t *testing.T) {
	var payload Payload
	if err := json.Unmarshal([]byte(realPostTool), &payload); err != nil {
		t.Fatalf("the fixture is not JSON: %v", err)
	}

	renamed := ClaudeSession{Name: "an-picker", NameSource: "user"}
	// [verified 2026-10-09, 2.1.285] What the file says after a
	// UserPromptSubmit hook returned a `sessionTitle`, and after a session was
	// started with `CLAUDE_CODE_SESSION_NAME` (D-88).
	hooked := ClaudeSession{Name: "ups-named-probe", NameSource: "hook"}
	launched := ClaudeSession{Name: "clash-probe"}
	placeholder := ClaudeSession{Name: "agent-notify-16", NameSource: "derived"}
	labelled := ClaudeSession{Name: "session-naming", NameSource: "auto"}
	const titled = "Fix configuration resetting tab name"

	for _, one := range []struct {
		what   string
		claude ClaudeSession
		title  string
		want   string
	}{
		{"a rename beats everything", renamed, titled, "an-picker"},
		{"so does a hook's title", hooked, titled, "ups-named-probe"},
		{"and a name with no source", launched, titled, "clash-probe"},
		{"a title beats a placeholder", placeholder, titled, titled},
		{"a title beats a label", labelled, titled, titled},
		{"a label beats a placeholder", labelled, "", "session-naming"},
		{"a placeholder is not a name", placeholder, "", ""},
		{"and neither is nothing", ClaudeSession{}, "", ""},
	} {
		t.Run(one.what, func(t *testing.T) {
			report, worth := Translate("PostToolUse", payload, one.claude, ClaudeTranscript{}, one.title)
			if !worth {
				t.Fatal("PostToolUse was ignored")
			}
			if report.Name != one.want {
				t.Errorf("Name = %q, want %q", report.Name, one.want)
			}
		})
	}
}

// The two hooks that carry `session_title`, as Claude sent them. [captured
// 2026-09-20 and 2026-09-21] A resume is the SessionStart that carries it most
// often, and it runs before Claude has written the session file.
const (
	realTitledPrompt = `{"session_id":"6a77f40b-ad62-44f2-abe8-489f37e5e59c",
	  "transcript_path":"/Users/x/.claude/projects/p/6a77f40b.jsonl",
	  "cwd":"/Users/x/projects/personal/agent-notify","prompt_id":"6f5e1c9d",
	  "permission_mode":"auto","hook_event_name":"UserPromptSubmit",
	  "prompt":"this implementation seems stratificated. i'd rather have a single function",
	  "session_title":"cobra-completer"}`

	realTitledResume = `{"session_id":"438c513c-5772-41c4-a662-1b3b5dc28ea3",
	  "transcript_path":"/Users/x/.claude/projects/p/438c513c.jsonl",
	  "cwd":"/Users/x/projects/personal/agent-notify","hook_event_name":"SessionStart",
	  "source":"resume","session_title":"wholeness"}`
)

// TestATitleTheSessionWasGivenIsReadFromThePayload is the rung above the file
// (D-88). It is the only name there is on a SessionStart, which runs before
// Claude writes the file, and the only one at all for a title a SessionStart
// hook gave, which never reaches the file — so it wins over whatever the file
// holds, and is reported when the file holds nothing.
func TestATitleTheSessionWasGivenIsReadFromThePayload(t *testing.T) {
	placeholder := ClaudeSession{Name: "p1-hooktitle-4a", NameSource: "derived"}
	labelled := ClaudeSession{Name: "session-naming", NameSource: "auto"}

	for _, one := range []struct {
		what, hook, payload string
		claude              ClaudeSession
		title               string
		want                string
	}{
		{"over a placeholder", "UserPromptSubmit", realTitledPrompt, placeholder, "", "cobra-completer"},
		{"over Claude's title and label", "UserPromptSubmit", realTitledPrompt, labelled,
			"Fix configuration resetting tab name", "cobra-completer"},
		{"before the file exists", "SessionStart", realTitledResume, ClaudeSession{}, "", "wholeness"},
	} {
		t.Run(one.what, func(t *testing.T) {
			var payload Payload
			if err := json.Unmarshal([]byte(one.payload), &payload); err != nil {
				t.Fatalf("the fixture is not JSON: %v", err)
			}
			report, worth := Translate(one.hook, payload, one.claude, ClaudeTranscript{}, one.title)
			if !worth {
				t.Fatalf("%s was ignored", one.hook)
			}
			if report.Name != one.want {
				t.Errorf("Name = %q, want %q", report.Name, one.want)
			}
		})
	}
}
