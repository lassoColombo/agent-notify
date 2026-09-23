package main_test

import (
	"strings"
	"testing"
)

// The command line is cobra, and these are the things that came with it that
// are worth a test: the ones that answer with LIVE state rather than with a
// fixed list. A hand-written completion script could offer the flags; nothing
// but the program itself can offer the sessions that are running.

// TestCompletingASessionOffersTheOnesThatAreRunning, most urgent first, each
// with what it is doing beside it. This is the whole reason the command line is
// a library rather than a switch.
func TestCompletingASessionOffersTheOnesThatAreRunning(t *testing.T) {
	pretend := start(t)
	running := pretend.records(t)
	if len(running) == 0 {
		t.Fatal("the fake agent recorded nothing to complete")
	}

	stdout, stderr, code := pretend.run(t, "__complete", "focus-session", "")
	if code != 0 {
		t.Fatalf("completing exited %d: %s", code, stderr)
	}

	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) < 2 {
		t.Fatalf("nothing was offered:\n%s", stdout)
	}
	offered, directive := lines[:len(lines)-1], lines[len(lines)-1]

	// The names are the ones `list` prints and `focus-session` accepts, so a
	// completion cannot offer something the command would then refuse.
	first, gloss, described := strings.Cut(offered[0], "\t")
	if first != running[0].DisplayName() {
		t.Errorf("offered %q, want the session that is running, %q", first, running[0].DisplayName())
	}
	// The gloss is what makes it worth having: which of these is asking for me?
	if !described || gloss != string(running[0].Kernel) {
		t.Errorf("offered %q, want the state beside the name", offered[0])
	}
	// 4 is ShellCompDirectiveNoFileComp: a session is not a filename, and
	// falling back to one would offer the whole directory.
	if directive != ":4" {
		t.Errorf("the directive is %q, want no file completion", directive)
	}
}

// TestCompletingAnIntegrationOffersWhatIsOnPath, which is the same question
// `install` asks a moment later — so nothing is offered that would then be
// refused.
func TestCompletingAnIntegrationOffersWhatIsOnPath(t *testing.T) {
	pretend := start(t)
	stdout, _, code := pretend.run(t, "__complete", "install", "")
	if code != 0 {
		t.Fatalf("completing exited %d", code)
	}
	// The fake agent is installed as `agent-notify-fake-agent` in the test's
	// own PATH only when a test puts it there; what must hold everywhere is
	// that nothing offered carries the prefix, because the prefix is the
	// convention and not part of the name a person types.
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		if strings.HasPrefix(line, "agent-notify-") {
			t.Errorf("offered %q — the prefix is the convention, not the name", line)
		}
	}
}
