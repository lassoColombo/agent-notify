package watcher_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/lassoColombo/agent-notify/internal/paths"
	"github.com/lassoColombo/agent-notify/internal/watcher"
	"github.com/lassoColombo/agent-notify/subscribe"
)

// The supervisor is tested against REAL children, because everything it is for
// happens between processes: a binary that is not there, one that starts and
// never connects, one that connects and stays. A mock would test the shape of
// the code rather than the behaviour of the system.
//
// The children are shell scripts that exec this test binary in a mode that
// connects and sits there — a fake integration written the way a person in
// another language would write one, and the same handshake either way.

const beAnIntegration = "TestBeAnIntegration"

// TestBeAnIntegration is not a test. It is the child, and it does nothing at
// all unless the script below runs it by name.
func TestBeAnIntegration(t *testing.T) {
	if os.Getenv("AGENT_NOTIFY_TEST_INTEGRATION") == "" {
		t.Skip("spawned only by the supervisor tests")
	}
	name := os.Getenv("AGENT_NOTIFY_TEST_INTEGRATION")

	ctx, stop := context.WithTimeout(context.Background(), 60*time.Second)
	defer stop()
	_ = subscribe.Run(ctx, subscribe.Integration{
		Name:     name,
		Roles:    []string{"display"},
		OnChange: func(subscribe.View) error { return nil },
	})
}

func shortRoot(t *testing.T) string {
	t.Helper()
	// Not t.TempDir(): its path on macOS is long enough on its own that adding
	// a socket name exceeds what a unix socket may be.
	root, err := os.MkdirTemp("/tmp", "an-sup")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	return root
}

// fakeIntegration writes a script that becomes one.
func fakeIntegration(t *testing.T, directory, name string) string {
	t.Helper()
	path := filepath.Join(directory, "agent-notify-"+name)
	script := fmt.Sprintf("#!/bin/sh\nAGENT_NOTIFY_TEST_INTEGRATION=%s exec %s -test.run=%s\n",
		name, os.Args[0], beAnIntegration)
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// running starts a session-watcher against root and stops it with the test.
// atATestablePace shortens the loop these tests watch go round.
//
// The sweep is five seconds in production, which is right for a safety net and
// absurd for a test that has to see it fire three times. It is not
// configuration — nobody has an opinion about how often a safety net checks —
// so it is moved here, in the one place that needs it moved, and put back.
func atATestablePace(t *testing.T, sweep, grace time.Duration) {
	t.Helper()
	wasSweep, wasGrace := watcher.SweepInterval, watcher.IntegrationGrace
	watcher.SweepInterval, watcher.IntegrationGrace = sweep, grace
	t.Cleanup(func() { watcher.SweepInterval, watcher.IntegrationGrace = wasSweep, wasGrace })
}

func running(t *testing.T, root string) paths.Layout {
	t.Helper()
	t.Setenv(paths.TheVariableThatNamesTheRoot, root)
	layout, err := paths.FromEnvironment()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if err := layout.Create(); err != nil {
		t.Fatalf("Create: %v", err)
	}

	started, err := watcher.Start(layout, false)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = started.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		stop()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("the session-watcher did not stop")
		}
	})
	return layout
}

func waitFor(t *testing.T, what string, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("waited for %s and it never happened", what)
}

func reported(
	t *testing.T, layout paths.Layout, name string,
) (watcher.Integration, bool) {
	t.Helper()
	report, found := watcher.ReadReport(layout)
	if !found {
		return watcher.Integration{}, false
	}
	for _, one := range report.Integrations {
		if one.Name == name {
			return one, true
		}
	}
	return watcher.Integration{}, false
}

// TestAnIntegrationIsStartedAndConnects is the reconciler doing its one job.
func TestAnIntegrationIsStartedAndConnects(t *testing.T) {
	atATestablePace(t, 200*time.Millisecond, watcher.IntegrationGrace)
	root := shortRoot(t)
	binary := fakeIntegration(t, root, "steady")
	write(t, root+"/config.toml", fmt.Sprintf(
		"[integration.steady]\nbinary = %q\n", binary))

	layout := running(t, root)
	waitFor(t, "the integration to connect", func() bool {
		one, found := reported(t, layout, "steady")
		return found && one.State == "connected"
	})

	one, _ := reported(t, layout, "steady")
	if one.PID == 0 {
		t.Error("the report carries no pid; the kernel was asked and should have answered")
	}
	if len(one.Roles) == 0 {
		t.Errorf("the report carries no roles: %+v — roles are declared on connect", one)
	}
}

// TestDeletingTheBinaryGivesUpWithThePathNamed is the first half of M13's
// "done when".
func TestDeletingTheBinaryGivesUpWithThePathNamed(t *testing.T) {
	atATestablePace(t, 100*time.Millisecond, watcher.IntegrationGrace)
	root := shortRoot(t)
	missing := filepath.Join(root, "agent-notify-ghost")
	write(t, root+"/config.toml", fmt.Sprintf(
		"integration-tries = 3\n\n[integration.ghost]\nbinary = %q\n",
		missing))

	layout := running(t, root)
	waitFor(t, "the supervisor to give up", func() bool {
		one, found := reported(t, layout, "ghost")
		return found && one.State == "gave up"
	})

	one, _ := reported(t, layout, "ghost")
	if !strings.Contains(one.Reason, "ghost") {
		t.Errorf("the reason does not name the integration: %q", one.Reason)
	}
	if !strings.Contains(one.Binary, missing) {
		t.Errorf("the report does not name the path that was looked at: %q", one.Binary)
	}

	// And it stays given up: the whole point of counting is that it stops.
	time.Sleep(700 * time.Millisecond)
	if again, _ := reported(t, layout, "ghost"); again.Failures != one.Failures {
		t.Errorf("it is still being retried: %d attempts, then %d", one.Failures, again.Failures)
	}
}

// TestReloadIsHowYouRetry is the second half: reinstalling plus a reload brings
// it back without restarting the session-watcher.
func TestReloadIsHowYouRetry(t *testing.T) {
	atATestablePace(t, 100*time.Millisecond, watcher.IntegrationGrace)
	root := shortRoot(t)
	binary := filepath.Join(root, "agent-notify-late")
	write(t, root+"/config.toml", fmt.Sprintf(
		"integration-tries = 2\n\n[integration.late]\nbinary = %q\n",
		binary))

	layout := running(t, root)
	waitFor(t, "the supervisor to give up on a binary that is not there", func() bool {
		one, found := reported(t, layout, "late")
		return found && one.State == "gave up"
	})

	// Install it, for the first time, while the session-watcher keeps running.
	fakeIntegration(t, root, "late")

	// Nothing happens until asked: giving up is a decision, not a pause.
	time.Sleep(500 * time.Millisecond)
	if one, _ := reported(t, layout, "late"); one.State != "gave up" {
		t.Errorf("it restarted itself without being asked: %+v", one)
	}

	if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatalf("SIGHUP: %v", err)
	}
	waitFor(t, "the reload to bring it back", func() bool {
		one, found := reported(t, layout, "late")
		return found && one.State == "connected"
	})
}

// TestReloadRestartsWhatIsAlreadyRunning is the other half of what a reload
// means, and the half that is easy to forget: an integration reads its settings
// once, when it starts. Re-reading the file in core alone would leave every
// display painting yesterday's glyphs with no way to tell.
func TestReloadRestartsWhatIsAlreadyRunning(t *testing.T) {
	atATestablePace(t, 100*time.Millisecond, watcher.IntegrationGrace)
	root := shortRoot(t)
	binary := fakeIntegration(t, root, "steady")
	write(t, root+"/config.toml", fmt.Sprintf(
		"[integration.steady]\nbinary = %q\n", binary))

	layout := running(t, root)
	waitFor(t, "the integration to connect", func() bool {
		one, found := reported(t, layout, "steady")
		return found && one.State == "connected"
	})
	before, _ := reported(t, layout, "steady")

	if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatalf("SIGHUP: %v", err)
	}
	waitFor(t, "the child to be replaced by a new one", func() bool {
		one, found := reported(t, layout, "steady")
		return found && one.State == "connected" && one.PID != before.PID
	})
}

// TestAChildThatNeverConnectsIsKilled: the handshake is the health check, and
// without this rule a child that hangs before connecting accumulates for ever.
func TestAChildThatNeverConnectsIsKilled(t *testing.T) {
	atATestablePace(t, 100*time.Millisecond, 300*time.Millisecond)
	root := shortRoot(t)
	binary := filepath.Join(root, "agent-notify-mute")
	write(t, binary, "#!/bin/sh\nsleep 300\n")
	if err := os.Chmod(binary, 0o700); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	write(t, root+"/config.toml", fmt.Sprintf(
		"integration-tries = 2\n\n[integration.mute]\nbinary = %q\n", binary))

	layout := running(t, root)
	waitFor(t, "the mute child to be given up on", func() bool {
		one, found := reported(t, layout, "mute")
		return found && one.State == "gave up"
	})

	// And it really was killed, rather than left lying about.
	out, _ := exec.Command("pgrep", "-f", binary).Output()
	if strings.TrimSpace(string(out)) != "" {
		t.Errorf("a child that never connected is still running: %s", out)
	}
}

// TestAContainerIsNotSupervised: a container is a program that gets run when
// somebody needs an answer, not a daemon (D-38). Starting one would be starting
// a program that immediately exits, for ever.
func TestAContainerIsNotSupervised(t *testing.T) {
	atATestablePace(t, 100*time.Millisecond, watcher.IntegrationGrace)
	root := shortRoot(t)
	binary := fakeIntegration(t, root, "placer")
	write(t, root+"/config.toml", fmt.Sprintf(
		"[integration.placer]\nbinary = %q\ncapture-environment = true\n\n"+
			"[container]\norder = [\"placer\"]\n", binary))

	layout := running(t, root)
	waitFor(t, "a report to be written", func() bool {
		_, found := watcher.ReadReport(layout)
		return found
	})
	time.Sleep(400 * time.Millisecond)

	if one, found := reported(t, layout, "placer"); found && one.Supervise {
		t.Errorf("a container was supervised: %+v", one)
	}
}

// TestShutdownTakesTheChildrenWithIt.
func TestShutdownTakesTheChildrenWithIt(t *testing.T) {
	atATestablePace(t, 100*time.Millisecond, watcher.IntegrationGrace)
	root := shortRoot(t)
	binary := fakeIntegration(t, root, "doomed")
	write(t, root+"/config.toml", fmt.Sprintf(
		"[integration.doomed]\nbinary = %q\n", binary))

	t.Setenv(paths.TheVariableThatNamesTheRoot, root)
	layout, err := paths.FromEnvironment()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if err := layout.Create(); err != nil {
		t.Fatalf("Create: %v", err)
	}
	started, err := watcher.Start(layout, false)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = started.Run(ctx)
		close(done)
	}()

	waitFor(t, "the child to connect", func() bool {
		one, found := reported(t, layout, "doomed")
		return found && one.State == "connected"
	})
	one, _ := reported(t, layout, "doomed")

	stop()
	<-done

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(one.PID, 0); err != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("pid %d outlived the session-watcher that started it", one.PID)
}
