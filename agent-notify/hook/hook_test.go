package hook

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/lassoColombo/agent-notify/internal/config"
	"github.com/lassoColombo/agent-notify/session"
)

// TestWhoIsAskedToCapture pins the predicate that decides which integrations
// run on the hook path.
//
// It is worth its own test because it is asked twice for two purposes — whether
// to capture at all, and who to run — and getting it wrong is silent in both
// directions: too small and an installed integration never captures, too large
// and every hook captures for ever.
//
// It does NOT check that askEveryoneWhoCaptures() spawns exactly this set. askEveryoneWhoCaptures() iterates
// this list, so there is nothing there that could disagree, and a test asserting
// it would be reading the same function back to itself.
func TestWhoIsAskedToCapture(t *testing.T) {
	yes, no := true, false

	for _, one := range []struct {
		what        string
		integration map[string]config.Integration
		want        []string
	}{
		{
			what: "two that capture",
			integration: map[string]config.Integration{
				"pane":   {Binary: "/bin/echo", CaptureEnvironment: true},
				"window": {Binary: "/bin/echo", CaptureEnvironment: true},
			},
			want: []string{"pane", "window"},
		},
		{
			what: "one turned off",
			integration: map[string]config.Integration{
				"pane":   {Binary: "/bin/echo", CaptureEnvironment: true},
				"window": {Binary: "/bin/echo", CaptureEnvironment: true, Enabled: &no},
			},
			want: []string{"pane"},
		},
		{
			what: "an explicit enabled = true is still enabled",
			integration: map[string]config.Integration{
				"pane": {Binary: "/bin/echo", CaptureEnvironment: true, Enabled: &yes},
			},
			want: []string{"pane"},
		},
		{
			// A display that only draws has nothing to read out of an agent's
			// environment, and must not be run on the path the agent waits on.
			what: "a display that captures nothing",
			integration: map[string]config.Integration{
				"pane": {Binary: "/bin/echo", CaptureEnvironment: true},
				"bar":  {Binary: "/bin/echo"},
			},
			want: []string{"pane"},
		},
		{
			// Whatever the boolean says there is nothing to run. config.Parse
			// complains about the table separately; this must simply not name it.
			what: "capture-environment with no binary",
			integration: map[string]config.Integration{
				"pane":    {Binary: "/bin/echo", CaptureEnvironment: true},
				"nothing": {CaptureEnvironment: true},
			},
			want: []string{"pane"},
		},
		{
			what:        "nothing configured",
			integration: map[string]config.Integration{},
			want:        nil,
		},
	} {
		t.Run(one.what, func(t *testing.T) {
			settings := config.Defaults()
			settings.Integration = one.integration
			if got := wouldBeAskedNow(settings); !slices.Equal(got, one.want) {
				t.Errorf("wouldBeAskedNow = %v, want %v", got, one.want)
			}
		})
	}
}

// TestEachAnswerLandsUnderItsOwnName is what askEveryoneWhoCaptures() does beyond running the
// list: each blob is kept under its own integration's name, and one that fails
// contributes nothing rather than an empty entry.
//
// The difference matters to worthCapturing, which compares who would be asked
// against who is stored. A failure that left a blank entry behind would read as
// a success, and that session would never be captured again.
func TestEachAnswerLandsUnderItsOwnName(t *testing.T) {
	// This test is about routing, not about the bound. Three shell scripts
	// against the production second is a race with whatever else the machine
	// is doing, and losing it says nothing about where an answer landed.
	was := captureTimeout
	captureTimeout = 10 * time.Second
	t.Cleanup(func() { captureTimeout = was })

	dir := t.TempDir()
	settings := config.Defaults()
	settings.Integration = map[string]config.Integration{
		"pane":   {Binary: program(t, dir, "pane", `{"PANE":"7"}`, 0), CaptureEnvironment: true},
		"window": {Binary: program(t, dir, "window", `{"WINDOW":"left"}`, 0), CaptureEnvironment: true},
		"broken": {Binary: program(t, dir, "broken", `nonsense`, 1), CaptureEnvironment: true},
	}

	captured := askEveryoneWhoCaptures(settings, slog.New(slog.DiscardHandler), nil)

	if got := owners(captured.By); !slices.Equal(got, []string{"pane", "window"}) {
		t.Fatalf("captured for %v, want only the two that answered", got)
	}
	if got := string(captured.By["pane"]); got != `{"PANE":"7"}` {
		t.Errorf("pane's blob is %s", got)
	}
	if got := string(captured.By["window"]); got != `{"WINDOW":"left"}` {
		t.Errorf("window's blob is %s", got)
	}
}

// TestTheAncestryIsCarriedWithNothingToCapture: the chain is core's own capture
// and the record needs it whether or not any integration contributed (§A8.4,
// §A7.4.2).
func TestTheAncestryIsCarriedWithNothingToCapture(t *testing.T) {
	chain := []session.Ancestor{{PID: 7, Command: "claude"}}

	captured := askEveryoneWhoCaptures(config.Defaults(), slog.New(slog.DiscardHandler), chain)

	if len(captured.Ancestry) != 1 || captured.Ancestry[0].Command != "claude" {
		t.Errorf("ancestry = %+v, want the chain it was handed", captured.Ancestry)
	}
	if len(captured.By) != 0 {
		t.Errorf("by = %v, want nothing: nothing captures", captured.By)
	}
}

// program writes one capture-environment that prints what it is told and exits
// with the code it is given, and returns its path.
func program(t *testing.T, dir, name, prints string, exit int) string {
	t.Helper()
	path := filepath.Join(dir, name)
	script := fmt.Sprintf("#!/bin/sh\necho '%s'\nexit %d\n", prints, exit)
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}

// TestNothingIsCapturedTwiceForOneUnchangedSession is the point of the whole
// arrangement: the steady state spawns nothing.
//
// No filesystem and no subprocess anywhere in it, which is itself the claim —
// the decision is two sorted lists and a pid, and that is exactly why it can be
// made before anything is run.
func TestNothingIsCapturedTwiceForOneUnchangedSession(t *testing.T) {
	running := session.Process{PID: 4242}
	asked := []string{"pane"}
	stored := session.Record{
		Process: running,
		CapturedContext: session.CapturedContext{
			CapturedAt: time.Now().UTC(),
			By:         map[string]json.RawMessage{"pane": json.RawMessage(`{"PANE":"7"}`)},
		},
	}

	for _, one := range []struct {
		what     string
		previous session.Record
		asked    []string
		process  session.Process
		want     bool
	}{
		{"nothing stored yet", session.Record{}, asked, running, true},
		{"nothing moved", stored, asked, running, false},
		{"the session came back in a new process", stored, asked, session.Process{PID: 9999}, true},
		{"an integration installed since", stored, []string{"pane", "window"}, running, true},
		{"an integration removed since", stored, nil, running, true},
		{
			// The accepted cost of deciding from the configuration: one that is
			// run and fails is never in `by`, so the sets differ and every hook
			// tries again until it is fixed (D-58).
			"one that is asked and keeps failing", stored, []string{"broken", "pane"}, running, true,
		},
		{
			// A machine with no capturing integrations stores its ancestry once
			// and is then left alone.
			"nothing captures, and nothing ever did",
			session.Record{
				Process:         running,
				CapturedContext: session.CapturedContext{CapturedAt: time.Now().UTC()},
			},
			nil, running, false,
		},
		{
			// An agent whose process could not be found leaves Process zero,
			// and that must not read as "the process changed" on every hook.
			"the agent's process was never found",
			session.Record{
				CapturedContext: session.CapturedContext{
					CapturedAt: time.Now().UTC(),
					By:         map[string]json.RawMessage{"pane": json.RawMessage(`{}`)},
				},
			},
			asked, session.Process{}, false,
		},
	} {
		t.Run(one.what, func(t *testing.T) {
			if got := worthCapturing(one.previous, one.asked, one.process); got != one.want {
				t.Errorf("worthCapturing = %v, want %v", got, one.want)
			}
		})
	}
}
