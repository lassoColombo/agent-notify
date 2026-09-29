package subscribe_test

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lassoColombo/agent-notify/subscribe"
	"github.com/pelletier/go-toml/v2"
)

var painter = subscribe.Install{
	Tool: "sh",
	Table: func(program, tool string) string {
		return fmt.Sprintf("[integration.painter]\nbinary = %q\n\n[integration.painter.settings]\nsh = %q\n", program, tool)
	},
	Advice: "\nPut it outermost first.\n",
}

// TestInstallPrintsTheTableAndWritesNothing is D-66 as a test: asserting only
// on the output would not notice the day somebody adds "and also append it".
func TestInstallPrintsTheTableAndWritesNothing(t *testing.T) {
	root := t.TempDir()
	me := subscribe.Integration{Name: "painter", Root: root}

	var out, problems strings.Builder
	if code := me.Install(painter, &out, &problems, nil); code != 0 {
		t.Fatalf("install exited %d:\n%s", code, problems.String())
	}
	var parsed struct {
		Integration map[string]struct {
			Binary   string `toml:"binary"`
			Settings struct {
				Sh string `toml:"sh"`
			} `toml:"settings"`
		} `toml:"integration"`
	}
	if err := toml.Unmarshal([]byte(out.String()), &parsed); err != nil {
		t.Fatalf("stdout is not TOML on its own: %v\n%s", err, out.String())
	}
	mine := parsed.Integration["painter"]
	if !filepath.IsAbs(mine.Binary) {
		t.Errorf("binary = %q is not absolute, and a child's PATH is not yours", mine.Binary)
	}
	if _, err := os.Stat(mine.Settings.Sh); err != nil {
		t.Errorf("the tool was not resolved at install time: sh = %q", mine.Settings.Sh)
	}
	if !strings.Contains(problems.String(), "outermost first") || !strings.Contains(problems.String(), "Nothing was written") {
		t.Errorf("the advice is not all on stderr:\n%s", problems.String())
	}
	if left, _ := os.ReadDir(root); len(left) > 0 {
		t.Errorf("install created %s, and that file is the user's", left[0].Name())
	}
}

func TestInstallTakesNoOptions(t *testing.T) {
	me := subscribe.Integration{Name: "painter", Root: t.TempDir()}
	if code := me.Install(painter, io.Discard, io.Discard, []string{"--print"}); code != 2 {
		t.Errorf("install --print exited %d, want 2 and a word about it", code)
	}
}

// TestAToolThatIsNotThereLeavesAVisibleHole: a table that looks complete and
// is not is worse than one that says what is missing.
func TestAToolThatIsNotThereLeavesAVisibleHole(t *testing.T) {
	me := subscribe.Integration{Name: "painter", Root: t.TempDir()}
	missing := painter
	missing.Tool = "no-such-tool-anywhere"
	var out, problems strings.Builder
	if code := me.Install(missing, &out, &problems, nil); code != 1 {
		t.Errorf("install exited %d, want 1", code)
	}
	if !strings.Contains(out.String(), `sh = ""`) {
		t.Errorf("the hole is not visible:\n%s", out.String())
	}
	if !strings.Contains(problems.String(), "no-such-tool-anywhere is not on this PATH") {
		t.Errorf("nothing said which tool is missing:\n%s", problems.String())
	}
}

var bar = subscribe.BundleInstall{
	Program: "agent-notify-test-bar", Identifier: "io.github.lassocolombo.agent-notify-test",
	Table: func(identity string) string {
		table := "[integration.bar]\n"
		if identity != "" {
			table += fmt.Sprintf("\n[integration.bar.settings]\nsign = %q\n", identity)
		}
		return table
	},
	Advice: "\nThere is a second display too.\n",
}

// TestInstallBundleBuildsTheBundleAndOnlyPrintsTheTable draws D-66's line:
// the bundle is this program's own artifact and is written; the config file
// is the user's and is not.
func TestInstallBundleBuildsTheBundleAndOnlyPrintsTheTable(t *testing.T) {
	root, app := t.TempDir(), t.TempDir()
	me := subscribe.Integration{Name: "bar", Root: root}

	var out, problems strings.Builder
	if code := me.InstallBundle(bar, &out, &problems, []string{"--app", app}); code != 0 {
		t.Fatalf("install exited %d:\n%s", code, problems.String())
	}
	var parsed struct {
		Integration map[string]struct {
			Binary string `toml:"binary"`
		} `toml:"integration"`
	}
	if err := toml.Unmarshal([]byte(out.String()), &parsed); err != nil {
		t.Fatalf("stdout is not TOML on its own: %v\n%s", err, out.String())
	}
	if parsed.Integration["bar"].Binary != "" {
		t.Error("the table names a binary, so core would try to run a display it cannot run")
	}
	if bundles, err := os.ReadDir(app); err != nil || len(bundles) == 0 {
		t.Errorf("no bundle was built in %s (%v)", app, err)
	}
	if !strings.Contains(problems.String(), ".app") || !strings.Contains(problems.String(), "second display") {
		t.Errorf("the bundle's path and the advice are not both on stderr:\n%s", problems.String())
	}
	// The config directory holds only what core makes for itself.
	for _, entry := range mustReadDir(t, root) {
		if entry.Name() == "config.toml" {
			t.Error("install wrote the config file, which is the user's")
		}
	}
}

// TestAnIdentityThisKeychainDoesNotHaveIsRefused before anything is built,
// because a bundle signed with nothing is the failure that is hardest to see.
func TestAnIdentityThisKeychainDoesNotHaveIsRefused(t *testing.T) {
	me := subscribe.Integration{Name: "bar", Root: t.TempDir()}
	var problems strings.Builder
	code := me.InstallBundle(bar, io.Discard, &problems, []string{"--app", t.TempDir(), "--sign", "nobody has this"})
	if code == 0 {
		t.Error("an identity that is not in the keychain was accepted")
	}
	if !strings.Contains(problems.String(), "nobody has this") {
		t.Errorf("the refusal does not name it:\n%s", problems.String())
	}
}

func mustReadDir(t *testing.T, dir string) []os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	return entries
}
