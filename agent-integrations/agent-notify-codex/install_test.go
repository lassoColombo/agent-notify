package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func configAt(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if body != "" {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	return path
}

func read(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	return string(body)
}

// hooksIn decodes the file the way codex would, which is the only assertion
// that matters: a block can be perfectly formatted and still register nothing.
func hooksIn(t *testing.T, path string) map[string][]string {
	t.Helper()
	var parsed struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Type    string `toml:"type"`
				Command string `toml:"command"`
			} `toml:"hooks"`
		} `toml:"hooks"`
	}
	if err := toml.Unmarshal([]byte(read(t, path)), &parsed); err != nil {
		t.Fatalf("what install wrote does not parse as codex would read it: %v", err)
	}
	commands := map[string][]string{}
	for hook, groups := range parsed.Hooks {
		for _, group := range groups {
			for _, one := range group.Hooks {
				if one.Type != "command" {
					t.Errorf("%s has a hook of type %q", hook, one.Type)
				}
				commands[hook] = append(commands[hook], one.Command)
			}
		}
	}
	return commands
}

func TestInstallRegistersEverySubscribedHook(t *testing.T) {
	path := configAt(t, "")
	if code := install([]string{"--config", path}); code != 0 {
		t.Fatalf("install exited %d", code)
	}

	registered := hooksIn(t, path)
	for _, hook := range SubscribedHooks {
		commands := registered[hook]
		if len(commands) != 1 {
			t.Errorf("%s is registered %d times, want once", hook, len(commands))
			continue
		}
		if !filepath.IsAbs(commands[0]) {
			t.Errorf("%s runs %q, which is not an absolute path — a hook's PATH is not your "+
				"shell's PATH", hook, commands[0])
		}
	}
	if len(registered) != len(SubscribedHooks) {
		t.Errorf("registered %d hooks, want the %d in SubscribedHooks: %v",
			len(registered), len(SubscribedHooks), registered)
	}
}

// TestInstallKeepsEverythingElse is why this appends rather than re-encodes: a
// codex config holds a person's model, their sandbox policy and their comments.
func TestInstallKeepsEverythingElse(t *testing.T) {
	before := `# my settings, do not lose these
model = "gpt-5.6-terra"

[mcp_servers.thing]
command = "thing-server"   # the one I actually use
`
	path := configAt(t, before)
	if code := install([]string{"--config", path}); code != 0 {
		t.Fatalf("install exited %d", code)
	}

	after := read(t, path)
	if !strings.HasPrefix(after, before) {
		t.Errorf("the file was rewritten rather than added to:\n%s", after)
	}
	if !strings.Contains(after, "# my settings, do not lose these") ||
		!strings.Contains(after, "# the one I actually use") {
		t.Errorf("comments did not survive:\n%s", after)
	}
	if len(hooksIn(t, path)) != len(SubscribedHooks) {
		t.Errorf("the hooks were not added:\n%s", after)
	}
}

func TestInstallTwiceChangesNothingTheSecondTime(t *testing.T) {
	path := configAt(t, "model = \"gpt-5.6-terra\"\n")
	if code := install([]string{"--config", path}); code != 0 {
		t.Fatalf("first install exited %d", code)
	}
	once := read(t, path)
	if code := install([]string{"--config", path}); code != 0 {
		t.Fatalf("second install exited %d", code)
	}
	if twice := read(t, path); twice != once {
		t.Errorf("a second install changed the file:\n%s", twice)
	}
}

// TestInstallLeavesSomebodyElsesHooksAlone: the hooks table is where people put
// their own commands, and codex allows several per event.
func TestInstallLeavesSomebodyElsesHooksAlone(t *testing.T) {
	mine := `[[hooks.Stop]]
[[hooks.Stop.hooks]]
type = "command"
command = "/Users/me/bin/say-done"
`
	path := configAt(t, mine)
	if code := install([]string{"--config", path}); code != 0 {
		t.Fatalf("install exited %d", code)
	}

	registered := hooksIn(t, path)
	if len(registered["Stop"]) != 2 {
		t.Errorf("Stop now runs %v, want theirs and ours", registered["Stop"])
	}
	if !strings.Contains(read(t, path), "/Users/me/bin/say-done") {
		t.Error("their hook is gone")
	}
}

func TestInstallWillNotTouchAFileItCannotParse(t *testing.T) {
	broken := "model = \nthis is not toml\n"
	path := configAt(t, broken)
	if code := install([]string{"--config", path}); code == 0 {
		t.Error("install reported success against a file that does not parse")
	}
	if after := read(t, path); after != broken {
		t.Errorf("the file was changed anyway:\n%s", after)
	}
}

func TestInstallPrintChangesNothing(t *testing.T) {
	path := configAt(t, "model = \"x\"\n")
	if code := install([]string{"--print", "--config", path}); code != 0 {
		t.Fatalf("install --print exited %d", code)
	}
	if after := read(t, path); after != "model = \"x\"\n" {
		t.Errorf("--print wrote to the file:\n%s", after)
	}
}

// TestInstallWritesNoTrust is a deliberate omission, asserted so that nobody
// adds it as a convenience. Codex records that a person trusted a hook; writing
// that hash here would be forging their consent to run a program on every event.
func TestInstallWritesNoTrust(t *testing.T) {
	path := configAt(t, "")
	if code := install([]string{"--config", path}); code != 0 {
		t.Fatalf("install exited %d", code)
	}
	if strings.Contains(read(t, path), "trusted_hash") {
		t.Error("install wrote a trust entry, which is the user's to give and not ours to take")
	}
}

func TestInstallAddsANewlineWhenTheFileLacksOne(t *testing.T) {
	path := configAt(t, "model = \"x\"")
	if code := install([]string{"--config", path}); code != 0 {
		t.Fatalf("install exited %d", code)
	}
	if len(hooksIn(t, path)) != len(SubscribedHooks) {
		t.Errorf("the block was glued onto the previous line:\n%s", read(t, path))
	}
}
