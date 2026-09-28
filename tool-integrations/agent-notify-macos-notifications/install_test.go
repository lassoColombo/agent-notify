package main

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

// printed runs install into a temporary place for the bundle and returns what
// went to stdout, which is the table and nothing else.
func printed(t *testing.T, arguments ...string) (string, int) {
	t.Helper()
	var out strings.Builder
	code := setUp(&out, io.Discard, append([]string{"--app", t.TempDir()}, arguments...))
	return out.String(), code
}

// TestInstallBuildsTheBundleAndOnlyPrintsTheTable is D-66 as a test, and the
// line it draws.
//
// The bundle is this program's own artifact and macOS gives it no choice, so
// install writes that. Agent-notify's config file belongs to the person running
// all this, so install does not write that — and a test that only looked at the
// output would not notice the day somebody adds "and also append it".
func TestInstallBuildsTheBundleAndOnlyPrintsTheTable(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGENT_NOTIFY_ROOT", root)
	app := t.TempDir()

	var out strings.Builder
	if code := setUp(&out, io.Discard, []string{"--app", app}); code != 0 {
		t.Fatalf("install exited %d", code)
	}
	if !strings.Contains(out.String(), "[integration."+Name+"]") {
		t.Errorf("install printed no table:\n%s", out.String())
	}

	// The bundle IS written: that half is mechanical.
	bundles, err := os.ReadDir(app)
	if err != nil || len(bundles) == 0 {
		t.Errorf("no bundle was built in %s (%v)", app, err)
	}

	// The config is not.
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
	if code := setUp(&out, &problems, []string{"--app", t.TempDir()}); code != 0 {
		t.Fatalf("install exited %d", code)
	}
	if problems.Len() == 0 {
		t.Error("nothing was said about what to do with it")
	}
	var parsed map[string]any
	if err := toml.Unmarshal([]byte(out.String()), &parsed); err != nil {
		t.Errorf("stdout is not TOML on its own: %v\n%s", err, out.String())
	}
	// And the bundle's own path, which is the other thing a person needs, is
	// said rather than printed into their config file.
	if !strings.Contains(problems.String(), ".app") {
		t.Errorf("the bundle's path was never mentioned:\n%s", problems.String())
	}
}

// TestTheTableNamesNoBinary: `binary` means "core may run this", and nothing
// core runs can answer a tap on a banner — that is answered on this process's
// main thread, so there has to be a process.
//
// The path it used to carry is in the launch agent now — see the bundle tests,
// which is the whole reason there is a bundle. What is left here is settings.
func TestTheTableNamesNoBinary(t *testing.T) {
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
	if mine.Binary != "" {
		t.Errorf("binary = %q, so core would try to run a display it cannot run",
			mine.Binary)
	}
}

// TestAnIdentityThisKeychainDoesNotHaveIsRefused before anything is built,
// because a bundle signed with nothing is the failure that is hardest to see.
func TestAnIdentityThisKeychainDoesNotHaveIsRefused(t *testing.T) {
	t.Setenv("AGENT_NOTIFY_ROOT", t.TempDir())
	var out, problems strings.Builder
	code := setUp(&out, &problems, []string{"--app", t.TempDir(), "--sign", "nobody has this"})
	if code == 0 {
		t.Error("an identity that is not in the keychain was accepted")
	}
	if !strings.Contains(problems.String(), "nobody has this") {
		t.Errorf("the refusal does not name it:\n%s", problems.String())
	}
}
