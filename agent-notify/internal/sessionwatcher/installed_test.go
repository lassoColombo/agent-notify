package sessionwatcher_test

import (
	"testing"
	"time"

	"github.com/lassoColombo/agent-notify/internal/handshake"
)

// TestAnIntegrationInstalledWhileTheWatcherRunsIsAskedOnTheSweep is D-102's
// "on PATH is installed" holding for a watcher that is already running: the
// sweep sees a new program on PATH and asks it, with nobody sending a signal
// (D-105).
func TestAnIntegrationInstalledWhileTheWatcherRunsIsAskedOnTheSweep(t *testing.T) {
	atATestablePace(t, 100*time.Millisecond)
	root := shortRoot(t)
	layout := running(t, root)
	waitFor(t, "the first report", func() bool {
		_, found := handshake.Read(layout)
		return found
	})

	answersCapabilities(t, root, "placer",
		`{"version":"0.0.0-dev","methods":["interpret-environment","focus","focused"]}`)

	waitFor(t, "the new program to be reported without a reload", func() bool {
		one, found := reported(t, layout, "placer")
		return found && len(one.Answers.Methods) == 3
	})
}
