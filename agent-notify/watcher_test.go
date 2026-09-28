package main_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lassoColombo/agent-notify/internal/paths"
	"github.com/lassoColombo/agent-notify/internal/sessionwatcher"
	"github.com/lassoColombo/agent-notify/session"
)

// watching reports whether a session-watcher holds the lock under root, and
// which pid.
func watching(t *testing.T, root string) (int, bool) {
	t.Helper()
	layout, err := paths.Under(root)
	if err != nil {
		t.Fatal(err)
	}
	if !sessionwatcher.Running(layout) {
		return 0, false
	}
	held, err := sessionwatcher.WhoHolds(layout)
	if err != nil {
		t.Fatal(err)
	}
	return held.PID, true
}

// waitFor polls until the condition holds or the deadline passes, and reports
// how long it took — which is the number these tests are actually about.
func waitFor(t *testing.T, within time.Duration, what string, ready func() bool) time.Duration {
	t.Helper()
	start := time.Now()
	deadline := start.Add(within)
	for time.Now().Before(deadline) {
		if ready() {
			return time.Since(start)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("waited %s for %s and it never happened", within, what)
	return 0
}

// TestAKilledAgentIsEndedInMilliseconds is M8's first "done when".
//
// No hook fires — the agent is killed with SIGKILL, which runs no code at all —
// so the only thing that can notice is the session-watcher's exit watch.
func TestAKilledAgentIsEndedInMilliseconds(t *testing.T) {
	pretend := startWith(t, "")

	// The hooks the pretend agent fired will have started a session-watcher.
	waitFor(t, 5*time.Second, "a session-watcher to take the lock", func() bool {
		_, running := watching(t, pretend.root)
		return running
	})

	if got := pretend.records(t)[0].Kernel; got != session.BlockedOnYou {
		t.Fatalf("kernel = %q before the kill", got)
	}

	pretend.kill()
	took := waitFor(t, 5*time.Second, "the record to be ended", func() bool {
		records := pretend.recordsAll(t)
		return len(records) == 1 && records[0].Kernel == session.Ended
	})
	t.Logf("ended %s after the kill, with the sweep five seconds away", took)

	if took > 3*time.Second {
		t.Errorf("took %s, which is slow enough that the sweep could have done it", took)
	}
	record := pretend.recordsAll(t)[0]
	if record.Detail != "process-gone" {
		t.Errorf("detail = %q, want process-gone", record.Detail)
	}
	if record.EndedAt.IsZero() {
		t.Error("ended_at is empty, so retention has nothing to count from")
	}
}

// TestARebootLeavesNoGhosts is M8's second.
//
// After a reboot the pids in the store either do not exist or belong to
// something else entirely, so they are ended without probing anything. This
// fakes the reboot the only way a test can: by rewriting the boot identity the
// record was written under.
func TestARebootLeavesNoGhosts(t *testing.T) {
	pretend := startWith(t, "")
	waitFor(t, 5*time.Second, "a session-watcher", func() bool {
		_, running := watching(t, pretend.root)
		return running
	})
	pretend.run(t, "watcher", "stop")

	// The agent is still alive and its pid is still real: only the boot
	// identity says this record belongs to a machine that has since restarted.
	record := pretend.records(t)[0]
	path := filepath.Join(pretend.root, "state", "sessions", record.Key.String()+".json")
	rewriteBootID(t, path, "00000000-1111-2222-3333-444444444444")

	if _, _, code := pretend.run(t, "watcher", "start"); code != 0 {
		t.Fatalf("watcher start exited %d", code)
	}
	took := waitFor(t, 5*time.Second, "the ghost to be ended", func() bool {
		all := pretend.recordsAll(t)
		return len(all) == 1 && all[0].Kernel == session.Ended
	})
	t.Logf("a session from a previous boot was ended %s after startup, with its process still alive", took)

	if got := pretend.recordsAll(t)[0].Detail; got != "process-gone" {
		t.Errorf("detail = %q", got)
	}
}

func rewriteBootID(t *testing.T, path, boot string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(content, &fields); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	var proc map[string]json.RawMessage
	if err := json.Unmarshal(fields["process"], &proc); err != nil {
		t.Fatalf("parsing process: %v", err)
	}
	proc["boot_id"], _ = json.Marshal(boot)
	fields["process"], _ = json.Marshal(proc)
	rewritten, _ := json.Marshal(fields)
	if err := os.WriteFile(path, rewritten, 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// TestAHookDoesNotHangOnAnInheritedPipe is M8's third, and the failure that
// would otherwise be found by a user rather than by us.
//
// Claude hands a hook a pipe and waits for it to close as the signal that the
// hook has finished. The hook starts a session-watcher meant to outlive it. If
// that session-watcher inherits the pipe, it never closes, and Claude waits
// forever — with nothing in any log, because nothing failed. Something merely
// never ended.
//
// So: give a hook a pipe, let it start a session-watcher, and require the pipe
// to reach end-of-file when the hook exits.
func TestAHookDoesNotHangOnAnInheritedPipe(t *testing.T) {
	built := binary(t)
	root, err := os.MkdirTemp("/tmp", "an-pipe")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() {
		stop := exec.Command(built, "watcher", "stop")
		stop.Env = append(os.Environ(), "AGENT_NOTIFY_ROOT="+root)
		_ = stop.Run()
		os.RemoveAll(root)
	})

	reading, writing, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}

	command := exec.Command(built, "report-event",
		"--agent", "fake", "--session", "piped", "--event", "user-sent-prompt", "--no-message")
	command.Env = append(os.Environ(), "AGENT_NOTIFY_ROOT="+root)
	command.Stdout, command.Stderr = writing, writing

	if err := command.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Exactly what Claude does: hand over the write end and keep only the read
	// end, so that end-of-file means "everything holding it has let go".
	writing.Close()

	if err := command.Wait(); err != nil {
		t.Fatalf("the hook exited badly: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		buffer := make([]byte, 256)
		for {
			read, err := reading.Read(buffer)
			if err != nil {
				done <- err
				return
			}
			if read > 0 {
				done <- nil
				return
			}
		}
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("the hook wrote to the pipe, which Claude reads into the session (R2)")
		}
		// io.EOF: everybody let go.
	case <-time.After(10 * time.Second):
		t.Fatalf("the pipe never reached end-of-file: something the hook started is still " +
			"holding it, and a real agent would now wait forever")
	}

	// And the thing it started is genuinely running, or this test proved
	// nothing about detachment.
	waitFor(t, 5*time.Second, "the session-watcher the hook started", func() bool {
		_, running := watching(t, root)
		return running
	})
}

// TestOnlyOneSessionWatcherSurvives is the lock doing its job: anyone may start
// one, and the race is harmless.
func TestOnlyOneSessionWatcherSurvives(t *testing.T) {
	pretend := startWith(t, "")
	waitFor(t, 5*time.Second, "a session-watcher", func() bool {
		_, running := watching(t, pretend.root)
		return running
	})
	first, _ := watching(t, pretend.root)

	// Several more, at once, exactly as several hooks firing together would.
	for range 5 {
		if _, _, code := pretend.run(t, "watcher", "start"); code != 0 {
			t.Errorf("a second start exited %d, which a race must not do", code)
		}
	}

	if second, _ := watching(t, pretend.root); first != second {
		t.Errorf("the lock changed hands: pid %d, then %d", first, second)
	}

	// Running it in the foreground loses the race and says so, rather than
	// failing or starting a second one.
	output, code := runForeground(t, pretend)
	if code != 0 {
		t.Errorf("a foreground run that lost the race exited %d, want 0", code)
	}
	if !strings.Contains(output, "already running") {
		t.Errorf("it did not say why it stopped: %q", output)
	}
}

func runForeground(t *testing.T, pretend *pretendAgent) (string, int) {
	t.Helper()
	command := exec.Command(pretend.binary, "watcher", "run", "--foreground")
	command.Env = append(os.Environ(), "AGENT_NOTIFY_ROOT="+pretend.root)
	var out strings.Builder
	command.Stdout, command.Stderr = &out, &out
	code := 0
	if err := command.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			code = exit.ExitCode()
		} else {
			t.Fatalf("running in the foreground: %v", err)
		}
	}
	return out.String(), code
}

// TestStoppingIsAskingAndNeverKilling: nothing in this system kills another
// process unprompted, so stop is a SIGTERM and a wait.
func TestStoppingIsAskingAndNeverKilling(t *testing.T) {
	pretend := startWith(t, "")
	waitFor(t, 5*time.Second, "a session-watcher", func() bool {
		_, running := watching(t, pretend.root)
		return running
	})

	stdout, _, code := pretend.run(t, "watcher", "stop")
	if code != 0 || !strings.Contains(stdout, "stopped") {
		t.Fatalf("stop exited %d saying %q", code, stdout)
	}
	if _, running := watching(t, pretend.root); running {
		t.Error("after stopping, something still holds the lock")
	}

	// Stopping what is not running is not an error worth shouting about.
	if _, _, code := pretend.run(t, "watcher", "stop"); code != 3 {
		t.Errorf("stopping nothing exited %d, want the distinct 'not running' code", code)
	}
}
