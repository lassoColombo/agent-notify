package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A person's settings.json holds their model, their permissions and their own
// hooks. Losing any of it to an installer would be unforgivable, so these tests
// are mostly about what install leaves alone.

// theirSettings is shaped like a real one, including hooks that are not ours.
const theirSettings = `{
  "env": { "CLAUDE_CODE_DISABLE_TERMINAL_TITLE": "1" },
  "model": "opus[1m]",
  "hooks": {
    "SessionStart": [
      { "hooks": [ { "type": "command", "command": "/Users/x/.claude/hooks/log-events.sh SessionStart" } ] }
    ],
    "PreToolUse": [
      { "matcher": "Bash", "hooks": [ { "type": "command", "command": "/Users/x/bin/audit.sh" } ] }
    ]
  }
}`

func build(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	binary := filepath.Join(dir, "agent-notify-claude")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Env = append(os.Environ(), "GOROOT=", "GOPATH=")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building: %v\n%s", err, output)
	}
	return binary
}

func run(t *testing.T, binary string, arguments ...string) (string, string, int) {
	t.Helper()
	command := exec.Command(binary, arguments...)
	var out, errs strings.Builder
	command.Stdout, command.Stderr = &out, &errs
	code := 0
	if err := command.Run(); err != nil {
		exit, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("running %v: %v", arguments, err)
		}
		code = exit.ExitCode()
	}
	return out.String(), errs.String(), code
}

func settingsAt(t *testing.T, path string) map[string]any {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var settings map[string]any
	if err := json.Unmarshal(content, &settings); err != nil {
		t.Fatalf("%s is not JSON: %v\n%s", path, err, content)
	}
	return settings
}

func commandsFor(t *testing.T, settings map[string]any, event string) []string {
	t.Helper()
	hooks, _ := settings["hooks"].(map[string]any)
	groups, _ := hooks[event].([]any)
	var found []string
	for _, group := range groups {
		entries, _ := group.(map[string]any)["hooks"].([]any)
		for _, entry := range entries {
			if command, ok := entry.(map[string]any)["command"].(string); ok {
				found = append(found, command)
			}
		}
	}
	return found
}

func TestInstallAddsItselfAndKeepsEverythingElse(t *testing.T) {
	binary := build(t)
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(theirSettings), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	stdout, stderr, code := run(t, binary, "install", "--settings", path)
	if code != 0 {
		t.Fatalf("install exited %d: %s", code, stderr)
	}
	t.Logf("install said:\n%s", stdout)

	settings := settingsAt(t, path)
	if settings["model"] != "opus[1m]" {
		t.Errorf("the model was lost: %v", settings["model"])
	}
	if _, present := settings["env"]; !present {
		t.Error("the env block was lost")
	}

	// Their own hook is still there, beside ours.
	sessionStart := commandsFor(t, settings, "SessionStart")
	if len(sessionStart) != 2 {
		t.Fatalf("SessionStart has %d commands, want theirs and ours: %v", len(sessionStart), sessionStart)
	}
	if !strings.Contains(strings.Join(sessionStart, " "), "log-events.sh") {
		t.Errorf("their own hook was lost: %v", sessionStart)
	}

	// A hook with a matcher they set is untouched.
	preTool := commandsFor(t, settings, "PreToolUse")
	if len(preTool) != 1 || !strings.Contains(preTool[0], "audit.sh") {
		t.Errorf("a hook we do not subscribe to was changed: %v", preTool)
	}

	// Every hook we subscribe to now names us.
	for _, event := range SubscribedHooks {
		joined := strings.Join(commandsFor(t, settings, event), " ")
		if !strings.Contains(joined, "agent-notify-claude") || !strings.Contains(joined, event) {
			t.Errorf("%s does not name this program: %q", event, joined)
		}
	}

	// And the file it replaced was kept.
	if _, err := os.Stat(path + ".before-agent-notify"); err != nil {
		t.Errorf("no copy of the previous settings was kept: %v", err)
	}
}

func TestInstallIsIdempotent(t *testing.T) {
	binary := build(t)
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(theirSettings), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	run(t, binary, "install", "--settings", path)
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	stdout, _, code := run(t, binary, "install", "--settings", path)
	if code != 0 {
		t.Fatalf("the second install exited %d", code)
	}
	if !strings.Contains(stdout, "already installed") {
		t.Errorf("the second install did not say it had nothing to do: %q", stdout)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(first) != string(second) {
		t.Errorf("installing twice changed the file:\n%s\n---\n%s", first, second)
	}
}

func TestInstallUpdatesAStalePathRatherThanAddingASecond(t *testing.T) {
	binary := build(t)
	path := filepath.Join(t.TempDir(), "settings.json")
	stale := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"/old/place/agent-notify-claude Stop"}]}]}}`
	if err := os.WriteFile(path, []byte(stale), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	run(t, binary, "install", "--settings", path)

	commands := commandsFor(t, settingsAt(t, path), "Stop")
	if len(commands) != 1 {
		t.Fatalf("Stop has %d commands, want the one, updated: %v", len(commands), commands)
	}
	if strings.Contains(commands[0], "/old/place/") {
		t.Errorf("the stale path survived: %q", commands[0])
	}
}

func TestInstallCreatesASettingsFileThatIsNotThere(t *testing.T) {
	binary := build(t)
	path := filepath.Join(t.TempDir(), "never", "settings.json")

	if _, stderr, code := run(t, binary, "install", "--settings", path); code != 0 {
		t.Fatalf("install exited %d: %s", code, stderr)
	}
	if got := commandsFor(t, settingsAt(t, path), "Stop"); len(got) != 1 {
		t.Errorf("Stop has %v", got)
	}
}

func TestInstallRefusesToOverwriteSomethingItCannotRead(t *testing.T) {
	binary := build(t)
	path := filepath.Join(t.TempDir(), "settings.json")
	broken := `{"model": "opus", oops`
	if err := os.WriteFile(path, []byte(broken), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, stderr, code := run(t, binary, "install", "--settings", path)
	if code == 0 {
		t.Error("install claimed success on a file it could not parse")
	}
	if !strings.Contains(stderr, "will not overwrite") {
		t.Errorf("the refusal does not say what it did: %q", stderr)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != broken {
		t.Errorf("the unreadable file was changed: %q", content)
	}
}

func TestPrintWritesNothing(t *testing.T) {
	binary := build(t)
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(theirSettings), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	stdout, _, code := run(t, binary, "install", "--print", "--settings", path)
	if code != 0 {
		t.Fatalf("install --print exited %d", code)
	}
	if !strings.Contains(stdout, "agent-notify-claude") {
		t.Errorf("--print did not show the block:\n%s", stdout)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != theirSettings {
		t.Error("--print wrote to the settings file")
	}
}

// TestTheHookNeverBlocks is R2 against the real binary. Claude reads exit 2 as
// "block this", stderr back into the session, and stdout into the conversation.
func TestTheHookNeverBlocks(t *testing.T) {
	binary := build(t)
	root := t.TempDir()

	attempts := []struct {
		hook    string
		payload string
	}{
		{"Stop", realStop},
		{"Stop", "not json at all"},
		{"Stop", ""},
		{"SomethingClaudeAddedLater", `{"session_id":"s"}`},
		{"Notification", realIdle},
		{"SessionStart", realStart},
	}
	for _, attempt := range attempts {
		command := exec.Command(binary, attempt.hook)
		command.Stdin = strings.NewReader(attempt.payload)
		command.Env = append(os.Environ(), "AGENT_NOTIFY_ROOT="+root)
		var out, errs strings.Builder
		command.Stdout, command.Stderr = &out, &errs
		err := command.Run()

		if err != nil {
			t.Errorf("%s exited non-zero: %v", attempt.hook, err)
		}
		if out.String() != "" {
			t.Errorf("%s wrote to stdout, which Claude reads into the session: %q", attempt.hook, out.String())
		}
		if errs.String() != "" {
			t.Errorf("%s wrote to stderr: %q", attempt.hook, errs.String())
		}
	}
}
