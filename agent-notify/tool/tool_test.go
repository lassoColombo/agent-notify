package tool_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lassoColombo/agent-notify/tool"
)

// program writes a shell script and hands back its path, which is how every
// test here gets a tool that behaves exactly as the test needs.
func program(t *testing.T, body string) string {
	t.Helper()
	where := filepath.Join(t.TempDir(), "pretend-tool")
	if err := os.WriteFile(where, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
		t.Fatalf("writing the script: %v", err)
	}
	return where
}

// TestATimeoutIsSaidToBeATimeout, and not an exit code.
//
// This is the check-order mistake as a test. A process the context kills comes
// back from Run as an ordinary *exec.ExitError, so a caller that looks at the
// error before ctx.Err() calls every timeout "signal: killed" — which names
// the symptom and hides the cause.
func TestATimeoutIsSaidToBeATimeout(t *testing.T) {
	_, err := tool.Run(program(t, "sleep 30"), 200*time.Millisecond, "whatever")

	var late *tool.TookTooLong
	if !errors.As(err, &late) {
		t.Fatalf("err = %v (%T), want *tool.TookTooLong", err, err)
	}
	var refused *tool.SaidNo
	if errors.As(err, &refused) {
		t.Error("a program that was killed is reported as one that decided")
	}
	if !strings.Contains(err.Error(), "took longer than 200ms") {
		t.Errorf("err = %q, want it to say what the bound was", err)
	}
}

// TestAHangingGrandchildDoesNotOutliveTheTimeout is WaitDelay earning its line.
//
// exec kills the process it started, but Run waits for the pipes it handed
// that process to close, and a grandchild inherits them. Without WaitDelay this
// takes as long as the grandchild does: measured in this project as a 200ms
// timeout that took 30 seconds.
func TestAHangingGrandchildDoesNotOutliveTheTimeout(t *testing.T) {
	// The child exits at once. The grandchild holds stdout for 30 seconds.
	hangs := program(t, "sleep 30 & exit 0")

	started := time.Now()
	_, _ = tool.Run(hangs, 200*time.Millisecond)

	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Errorf("a 200ms timeout took %s: the pipes were waited on", elapsed)
	}
}

// TestAProgramThatIsNotThereIsNotAProgramThatRefused.
//
// The two arrive as one non-nil error from exec and mean opposite things. A
// container owes container.NotRunning for the first and container.Refused for
// the second, and before this package two of them could not tell.
func TestAProgramThatIsNotThereIsNotAProgramThatRefused(t *testing.T) {
	_, err := tool.Run(filepath.Join(t.TempDir(), "no-such-tool"), time.Second, "focus")

	var missing *tool.DidNotRun
	if !errors.As(err, &missing) {
		t.Fatalf("err = %v (%T), want *tool.DidNotRun", err, err)
	}
	var refused *tool.SaidNo
	if errors.As(err, &refused) {
		t.Error("a binary that is not there is reported as a refusal")
	}
	if !strings.Contains(err.Error(), "no-such-tool") {
		t.Errorf("err = %q, want it to name the path that is not a program", err)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("err = %v, want the reason to survive unwrapping", err)
	}
}

// TestARefusalCarriesTheCodeAndTheToolsOwnWords.
func TestARefusalCarriesTheCodeAndTheToolsOwnWords(t *testing.T) {
	_, err := tool.Run(program(t, "echo 'window id 34 is not known' >&2; exit 3"),
		10*time.Second, "focus", "--window-id", "34")

	var refused *tool.SaidNo
	if !errors.As(err, &refused) {
		t.Fatalf("err = %v (%T), want *tool.SaidNo", err, err)
	}
	if refused.Code != 3 {
		t.Errorf("code = %d, want 3", refused.Code)
	}
	if !strings.Contains(err.Error(), "window id 34 is not known") {
		t.Errorf("err = %q, want the tool's own words in it", err)
	}
	if !strings.Contains(err.Error(), "focus --window-id 34") {
		t.Errorf("err = %q, want it to say what was asked", err)
	}
	// The directory is not news once the program has run.
	if strings.Contains(err.Error(), string(os.PathSeparator)+"pretend-tool") {
		t.Errorf("err = %q, want the tool's name rather than its path", err)
	}
}

// TestWhatItPrintedComesBackEvenWhenItFailed, because for at least one tool the
// complaint IS the answer: sketchybar reports a bad message on stderr and exits
// non-zero, and a render has to be able to read it rather than propagate it.
func TestWhatItPrintedComesBackEvenWhenItFailed(t *testing.T) {
	// Ten seconds for a script that echoes twice: the bound is not what this
	// test is about, and a second of it is a coin toss on a machine running
	// the rest of this suite alongside.
	out, err := tool.Run(program(t, "echo out; echo problem >&2; exit 1"), 10*time.Second)
	if err == nil {
		t.Fatal("a non-zero exit was not an error")
	}
	if got := strings.TrimSpace(string(out.Stdout)); got != "out" {
		t.Errorf("stdout = %q, want what it printed", got)
	}
	if got := strings.TrimSpace(string(out.Stderr)); got != "problem" {
		t.Errorf("stderr = %q, want what it complained", got)
	}
}

// TestNoTimeoutIsRefusedRatherThanDefaulted.
//
// A zero timeout is a caller that forgot, and it is the unbounded wait R18
// exists to prevent. Supplying one quietly would hide the bug rather than show
// it the first time the code runs.
func TestNoTimeoutIsRefusedRatherThanDefaulted(t *testing.T) {
	ran := filepath.Join(t.TempDir(), "marker")
	_, err := tool.Run(program(t, "touch "+ran), 0)
	if err == nil {
		t.Fatal("a program with no timeout was run")
	}
	if _, stat := os.Stat(ran); stat == nil {
		t.Error("it was run anyway")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Errorf("err = %q, want it to say what is missing", err)
	}
}

// TestSummariseTakesTheFirstLineAndStripsWhatATerminalWouldObey.
//
// The OSC case is the one the hand-rolled copies got wrong: they skipped `ESC [
// … final` and nothing else, so `ESC ] 0 ; … BEL` — the sequence that retitles
// a window — went through all three untouched on its way to a log and, for
// sketchybar, back out to a terminal (§A15).
func TestSummariseTakesTheFirstLineAndStripsWhatATerminalWouldObey(t *testing.T) {
	for _, one := range []struct{ name, printed, want string }{
		{"a plain complaint", "no session named home\n", "no session named home"},
		{"colour around it", "\x1b[31mno such session\x1b[0m\n", "no such session"},
		{"a window retitle", "\x1b]0;pwned\x07real message", "real message"},
		{"the rest of the list", "not found\nhome\nwork\n", "not found"},
		{"blank lines first", "\n\n  the reason\n", "the reason"},
		{"half a rune", "bad \xff byte", "bad � byte"},
	} {
		if got := tool.Summarise([]byte(one.printed)); got != one.want {
			t.Errorf("%s: Summarise(%q) = %q, want %q", one.name, one.printed, got, one.want)
		}
	}
}

// TestAProgramThatFailedInSilenceIsSaidToHave, rather than leaving an error
// that ends in a colon and reads as a bug in the error.
func TestAProgramThatFailedInSilenceIsSaidToHave(t *testing.T) {
	if got := tool.Summarise(nil); got != "nothing" {
		t.Errorf("Summarise(nil) = %q, want %q", got, "nothing")
	}
	_, err := tool.Run(program(t, "exit 2"), 10*time.Second, "--set", "missing")
	if !strings.HasSuffix(err.Error(), "nothing") {
		t.Errorf("err = %q, want it to end by saying it said nothing", err)
	}
}

// TestSummariseIsBounded, because a tool answering with a megabyte is a tool
// putting a megabyte in a log line.
func TestSummariseIsBounded(t *testing.T) {
	if got := tool.Summarise([]byte(strings.Repeat("x", 100_000))); len(got) > 256 {
		t.Errorf("Summarise gave back %d bytes", len(got))
	}
}

// TestStdinReachesTheProgram, which is the one thing RunWithInput adds.
//
// Ten seconds for a script that finishes in milliseconds, here and in the
// other tests that only need a script to run: the bound is there so a broken
// pipe cannot hang the test, not to measure anything, and a shell script
// sharing the machine with a dozen packages' linkers has been seen to wait
// more than a second to be scheduled.
func TestStdinReachesTheProgram(t *testing.T) {
	out, err := tool.RunWithInput(program(t, "cat"), 10*time.Second, []byte(`{"pane":7}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := string(out.Stdout); got != `{"pane":7}` {
		t.Errorf("stdout = %q, want what went in", got)
	}
}
