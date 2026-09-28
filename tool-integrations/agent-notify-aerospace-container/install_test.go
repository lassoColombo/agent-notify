package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

// printed runs install and returns what went to stdout, which is the table and
// nothing else.
func printed(t *testing.T, arguments ...string) (string, int) {
	t.Helper()
	var out strings.Builder
	code := printTable(&out, io.Discard, arguments)
	return out.String(), code
}

// TestInstallPrintsTheTableAndWritesNothing is D-66 as a test.
//
// The point of the change it pins is that this program does not write to a file
// that belongs to somebody else. Asserting only on the output would not notice
// the day somebody adds "and also append it".
func TestInstallPrintsTheTableAndWritesNothing(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGENT_NOTIFY_ROOT", root)

	table, code := printed(t)
	if code != 0 {
		t.Fatalf("install exited %d", code)
	}
	if !strings.Contains(table, "[integration."+Name+"]") {
		t.Errorf("install printed no table:\n%s", table)
	}

	left, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, entry := range left {
		t.Errorf("install created %s, and that file is the user's", entry.Name())
	}
}

// TestOnlyTheTableGoesToStdout, so that redirecting it into a config file
// produces a config file rather than a config file with an apology in it.
func TestOnlyTheTableGoesToStdout(t *testing.T) {
	t.Setenv("AGENT_NOTIFY_ROOT", t.TempDir())

	var out, problems strings.Builder
	if code := printTable(&out, &problems, nil); code != 0 {
		t.Fatalf("install exited %d", code)
	}
	if problems.Len() == 0 {
		t.Error("nothing was said about what to do with it")
	}
	var parsed map[string]any
	if err := toml.Unmarshal([]byte(out.String()), &parsed); err != nil {
		t.Errorf("stdout is not TOML on its own: %v\n%s", err, out.String())
	}
}

// TestInstallTakesNoOptions: there is nothing left to vary. `--print` described
// the only thing it does, and `--config` named a file it no longer opens.
func TestInstallTakesNoOptions(t *testing.T) {
	if _, code := printed(t, "--print"); code != 2 {
		t.Errorf("install --print exited %d, want 2 and a word about it", code)
	}
}

// TestWhatInstallPrintsIsWhatCoreReads closes the loop the rest of this leaves
// open: a table that prints beautifully and parses into keys core does not
// recognise is a table that helps nobody.
func TestWhatInstallPrintsIsWhatCoreReads(t *testing.T) {
	t.Setenv("AGENT_NOTIFY_ROOT", t.TempDir())
	table, code := printed(t)
	if code != 0 {
		t.Fatalf("install exited %d", code)
	}

	var parsed struct {
		Integration map[string]struct {
			Binary string `toml:"binary"`
		} `toml:"integration"`
	}
	if err := toml.Unmarshal([]byte(table), &parsed); err != nil {
		t.Fatalf("what install printed is not TOML: %v\n%s", err, table)
	}

	mine, present := parsed.Integration[Name]
	if !present {
		t.Fatalf("no [integration.%s] table in:\n%s", Name, table)
	}
	if !filepath.IsAbs(mine.Binary) {
		t.Errorf("binary = %q is not absolute, and a supervised child's PATH is not yours",
			mine.Binary)
	}
}

// TestTheContainerOrderIsOfferedAndNeverDecided: which shell is outside which
// is something only the person running them knows (§A11.2). The old install
// said so in a comment and then wrote the order anyway whenever there was no
// [container] table to collide with.
func TestTheContainerOrderIsOfferedAndNeverDecided(t *testing.T) {
	t.Setenv("AGENT_NOTIFY_ROOT", t.TempDir())
	table, _ := printed(t)
	if !strings.Contains(table, "[container]") {
		t.Errorf("the order was not offered at all:\n%s", table)
	}
	var parsed struct {
		Container struct {
			Order []string `toml:"order"`
		} `toml:"container"`
	}
	if err := toml.Unmarshal([]byte(table), &parsed); err != nil {
		t.Fatalf("what install printed is not TOML: %v", err)
	}
	if len(parsed.Container.Order) != 1 || parsed.Container.Order[0] != Name {
		t.Errorf("order = %v, want just this container for a person to place",
			parsed.Container.Order)
	}
}

// TestTheToolIsResolvedAtInstallTime is D-67.
//
// A supervised child's PATH is not your shell's, so looking aerospace up at
// runtime is looking it up in the one context that cannot answer. Install runs
// in your shell, where the answer is simply there.
func TestTheToolIsResolvedAtInstallTime(t *testing.T) {
	if _, err := exec.LookPath("aerospace"); err != nil {
		t.Skip("no aerospace on this PATH to resolve")
	}
	t.Setenv("AGENT_NOTIFY_ROOT", t.TempDir())

	table, code := printed(t)
	if code != 0 {
		t.Fatalf("install exited %d", code)
	}
	var parsed struct {
		Integration map[string]struct {
			Settings struct {
				Tool string `toml:"aerospace"`
			} `toml:"settings"`
		} `toml:"integration"`
	}
	if err := toml.Unmarshal([]byte(table), &parsed); err != nil {
		t.Fatalf("what install printed is not TOML: %v\n%s", err, table)
	}
	where := parsed.Integration[Name].Settings.Tool
	if !filepath.IsAbs(where) {
		t.Errorf("aerospace = %q is not an absolute path", where)
	}
	if _, err := os.Stat(where); err != nil {
		t.Errorf("aerospace = %q does not exist: %v", where, err)
	}
	// And what it printed is what the program will accept, which is the loop
	// worth closing: a table that reads well and is then refused helps nobody.
	if _, err := TheAerospaceToRun(where); err != nil {
		t.Errorf("install printed a path its own program refuses: %v", err)
	}
}

// TestTheToolMustBeNamedAndAbsolute: there is no search left, so the only
// answers are a path somebody has seen, or a refusal that says so.
func TestTheToolMustBeNamedAndAbsolute(t *testing.T) {
	for _, one := range []struct{ given, mention string }{
		{"", "is not set"},
		{"aerospace", "absolute"},
		{"./aerospace", "absolute"},
	} {
		_, err := TheAerospaceToRun(one.given)
		if err == nil {
			t.Errorf("TheAerospaceToRun(%q) was accepted", one.given)
			continue
		}
		if !strings.Contains(err.Error(), one.mention) {
			t.Errorf("TheAerospaceToRun(%q) said %q, want it to mention %q", one.given, err, one.mention)
		}
	}
	if _, err := TheAerospaceToRun("/nowhere/at/all/aerospace"); err == nil {
		t.Error("a path that is not there was accepted")
	}
}
