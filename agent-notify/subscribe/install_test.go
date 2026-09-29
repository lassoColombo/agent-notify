package subscribe

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lassoColombo/agent-notify/internal/config"
	"github.com/lassoColombo/agent-notify/internal/paths"
	"github.com/pelletier/go-toml/v2"
)

var painter = Install{
	Tool: "sh",
	Table: func(program, tool string) string {
		return fmt.Sprintf("[integration.painter]\nbinary = %q\n\n[integration.painter.settings]\nsh = %q\n", program, tool)
	},
	Advice: "\nPut it outermost first.\n",
}

// TestInstallWritesTheDropInAndNotTheConfig is D-85 as a test: the drop-in is
// the integration's, the config file is the user's.
func TestInstallWritesTheDropInAndNotTheConfig(t *testing.T) {
	root := t.TempDir()
	me := Integration{Name: "painter", Root: root}
	layout, _ := paths.Under(root)

	var out, problems strings.Builder
	if code := me.Install(painter, &out, &problems, nil); code != 0 {
		t.Fatalf("install exited %d:\n%s", code, problems.String())
	}
	if !strings.Contains(out.String(), "wrote "+layout.DropIn("painter")) {
		t.Errorf("install did not say where it wrote:\n%s", out.String())
	}
	if _, err := os.Stat(layout.ConfigFile); err == nil {
		t.Error("install wrote the config file, which is the user's")
	}
	if !strings.Contains(problems.String(), "outermost first") {
		t.Errorf("the advice is not on stderr:\n%s", problems.String())
	}

	// And what it wrote is what core reads.
	settings, complaints := config.Load(layout)
	if len(complaints) > 0 {
		t.Fatalf("core complains about the drop-in: %v", complaints)
	}
	mine := settings.Integration["painter"]
	if !filepath.IsAbs(mine.Binary) {
		t.Errorf("binary = %q is not absolute, and a child's PATH is not yours", mine.Binary)
	}
	if sh, _ := mine.Settings["sh"].(string); sh == "" {
		t.Error("the tool was not resolved at install time")
	}

	if code := me.Uninstall(&out, &problems, nil); code != 0 {
		t.Fatalf("uninstall exited %d:\n%s", code, problems.String())
	}
	if _, err := os.Stat(layout.DropIn("painter")); err == nil {
		t.Error("uninstall left the drop-in behind")
	}
}

func TestInstallTakesNoOptions(t *testing.T) {
	me := Integration{Name: "painter", Root: t.TempDir()}
	if code := me.Install(painter, io.Discard, io.Discard, []string{"--print"}); code != 2 {
		t.Errorf("install --print exited %d, want 2 and a word about it", code)
	}
}

// TestAToolThatIsNotThereLeavesAVisibleHole: a table that looks complete and
// is not is worse than one that says what is missing.
func TestAToolThatIsNotThereLeavesAVisibleHole(t *testing.T) {
	root := t.TempDir()
	me := Integration{Name: "painter", Root: root}
	missing := painter
	missing.Tool = "no-such-tool-anywhere"
	var problems strings.Builder
	if code := me.Install(missing, io.Discard, &problems, nil); code != 1 {
		t.Errorf("install exited %d, want 1", code)
	}
	layout, _ := paths.Under(root)
	written, _ := os.ReadFile(layout.DropIn("painter"))
	if !strings.Contains(string(written), `sh = ""`) {
		t.Errorf("the hole is not visible:\n%s", written)
	}
	if !strings.Contains(problems.String(), "no-such-tool-anywhere is not on this PATH") {
		t.Errorf("nothing said which tool is missing:\n%s", problems.String())
	}
}

var bar = BundleInstall{
	Program: "agent-notify-test-bar", Identifier: "io.github.lassocolombo.agent-notify-test",
	Table: func(identity string) string {
		table := "[integration.bar]\nlaunch-agent = \"io.github.lassocolombo.agent-notify-test\"\n"
		if identity != "" {
			table += fmt.Sprintf("\n[integration.bar.settings]\nsign = %q\n", identity)
		}
		return table
	},
	Advice: "\nThere is a second display too.\n",
}

// askedOfLaunchd replaces launchctl for one test and records what it was asked.
func askedOfLaunchd(t *testing.T) *[]string {
	t.Helper()
	was := launchctl
	var asked []string
	launchctl = func(arguments ...string) error {
		if arguments[0] == "print" {
			// Nothing is loaded in a test, which is what makes the wait for a
			// job to go away finish immediately.
			return fmt.Errorf("no such service")
		}
		asked = append(asked, strings.Join(arguments, " "))
		return nil
	}
	t.Cleanup(func() { launchctl = was })
	return &asked
}

// TestInstallBundleDoesTheWholeJob: the bundle is built, the drop-in and the
// launch agent are written, launchd is asked, and the config file is not
// touched. Re-running it is the upgrade.
func TestInstallBundleDoesTheWholeJob(t *testing.T) {
	root, app, home := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	asked := askedOfLaunchd(t)
	me := Integration{Name: "bar", Root: root}
	layout, _ := paths.Under(root)

	var out, problems strings.Builder
	if code := me.InstallBundle(bar, &out, &problems, []string{"--app", app}); code != 0 {
		t.Fatalf("install exited %d:\n%s", code, problems.String())
	}
	if bundles, err := os.ReadDir(app); err != nil || len(bundles) == 0 {
		t.Errorf("no bundle was built in %s (%v)", app, err)
	}
	plist := filepath.Join(home, "Library", "LaunchAgents", bar.Identifier+".plist")
	if content, err := os.ReadFile(plist); err != nil || !strings.Contains(string(content), ".app/Contents/MacOS/"+bar.Program) {
		t.Errorf("the launch agent was not written pointing inside the bundle: %v", err)
	}
	if len(*asked) != 1 || !strings.HasPrefix((*asked)[0], "bootstrap ") {
		t.Errorf("launchd was asked %v, want a bootstrap of a job that was not loaded", *asked)
	}
	settings, complaints := config.Load(layout)
	if len(complaints) > 0 {
		t.Fatalf("core complains about the drop-in: %v", complaints)
	}
	if mine := settings.Integration["bar"]; mine.Binary != "" || mine.LaunchAgent != bar.Identifier {
		t.Errorf("the drop-in reads %+v: want no binary and the launch agent's label", mine)
	}
	if _, err := os.Stat(layout.ConfigFile); err == nil {
		t.Error("install wrote the config file, which is the user's")
	}
	if !strings.Contains(problems.String(), "second display") {
		t.Errorf("the advice is not on stderr:\n%s", problems.String())
	}

	if code := me.UninstallBundle(bar, &out, &problems, []string{"--app", app}); code != 0 {
		t.Fatalf("uninstall exited %d:\n%s", code, problems.String())
	}
	for _, gone := range []string{plist, layout.DropIn("bar"), filepath.Join(app, bar.Program+".app")} {
		if _, err := os.Stat(gone); err == nil {
			t.Errorf("uninstall left %s behind", gone)
		}
	}
	if last := (*asked)[len(*asked)-1]; !strings.HasPrefix(last, "bootout ") {
		t.Errorf("uninstall did not stop the job: launchd was asked %v", *asked)
	}
}

// TestAnIdentityThisKeychainDoesNotHaveIsRefused before anything is built,
// because a bundle signed with nothing is the failure that is hardest to see.
func TestAnIdentityThisKeychainDoesNotHaveIsRefused(t *testing.T) {
	askedOfLaunchd(t)
	me := Integration{Name: "bar", Root: t.TempDir()}
	var problems strings.Builder
	code := me.InstallBundle(bar, io.Discard, &problems, []string{"--app", t.TempDir(), "--sign", "nobody has this"})
	if code == 0 {
		t.Error("an identity that is not in the keychain was accepted")
	}
	if !strings.Contains(problems.String(), "nobody has this") {
		t.Errorf("the refusal does not name it:\n%s", problems.String())
	}
}

// The table a bundle install files is TOML core reads.
func TestTheBundleTableIsToml(t *testing.T) {
	var parsed map[string]any
	if err := toml.Unmarshal([]byte(bar.Table("me")), &parsed); err != nil {
		t.Fatal(err)
	}
}
