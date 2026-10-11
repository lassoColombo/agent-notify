package handshake_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/lassoColombo/agent-notify/internal/config"
	"github.com/lassoColombo/agent-notify/internal/handshake"
	"github.com/lassoColombo/agent-notify/internal/paths"
	"github.com/lassoColombo/agent-notify/session"
)

// TestWithNoReportItAsksRatherThanSayingThereIsNoContainer is what keeps a
// focus working on a machine where nothing is running.
//
// Reading the report is the shortcut — a focus comes off a keypress and must
// not run every container to find out what a container is — but the runtime
// directory does not survive a reboot, so the first focus after one has no
// report to read. Answering "you have no containers" then would be a lie told
// at exactly the wrong moment.
func TestWithNoReportItAsksRatherThanSayingThereIsNoContainer(t *testing.T) {
	// Two seconds is right for a program that prints a constant and wrong for
	// a shell script doing the same while a dozen packages' tests link and
	// spawn around it, and this test is not about the timeout.
	was := handshake.Timeout
	handshake.Timeout = 30 * time.Second
	t.Cleanup(func() { handshake.Timeout = was })

	// Short, so that the shell script written into it has a short path.
	root, err := os.MkdirTemp("/tmp", "an-hs")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	placer := filepath.Join(root, "agent-notify-placer")
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = %q ]; then printf '%%s\\n' '%s'; exit 0; fi\nexit 1\n",
		session.CapabilitiesCommand,
		`{"version":"0.0.0-dev","methods":["interpret-environment","focus","focused"]}`)
	if err := os.WriteFile(placer, []byte(script), 0o700); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	table := fmt.Sprintf("[integration.placer]\nbinary = %q\n", placer)
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte(table), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Setenv(paths.TheVariableThatNamesTheRoot, root)
	layout, err := paths.FromEnvironment()
	if err != nil {
		t.Fatalf("FromEnvironment: %v", err)
	}
	settings, problems := config.Load(layout)
	if len(problems) > 0 {
		t.Fatalf("reading the config: %v", problems)
	}

	// No session-watcher has ever run here, so there is no report at all.
	methods := handshake.ByIntegration(layout, settings)
	if !slices.Contains(methods["placer"].Methods, session.MethodFocus) {
		t.Errorf("with no report it found %v, and the program on disk answers focus",
			methods["placer"].Methods)
	}
}
