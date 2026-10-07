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
