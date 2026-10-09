package subscribe_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lassoColombo/agent-notify/subscribe"
)

// TestAnIntegrationLogsThroughTheSDK is D-95: the logger core opened for the
// process is the one an integration writes with, tagged with its name, on the
// one file every agent-notify process appends to.
func TestAnIntegrationLogsThroughTheSDK(t *testing.T) {
	root := t.TempDir()
	me := subscribe.Integration{Name: "painter", Root: root}

	me.Logger().Info("drew something", "panes", 3)

	content, err := os.ReadFile(filepath.Join(root, "state", "agent-notify.log"))
	if err != nil {
		t.Fatalf("nothing was logged under the root the integration was given: %v", err)
	}
	line := string(content)
	for _, want := range []string{"component=painter", `msg="drew something"`, "panes=3"} {
		if !strings.Contains(line, want) {
			t.Errorf("the log says %q, want it to carry %s", strings.TrimSpace(line), want)
		}
	}
}

// TestAnIntegrationWithNoFilesLogsNowhere rather than failing: the log is a
// diagnostic channel, and one that can fail is one that eventually fails the
// thing it diagnoses (R2).
func TestAnIntegrationWithNoFilesLogsNowhere(t *testing.T) {
	me := subscribe.Integration{Name: "painter", Root: string([]byte{0})}
	me.Logger().Warn("into the void")
}
