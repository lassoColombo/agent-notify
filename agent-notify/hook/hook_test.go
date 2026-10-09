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

// TestEachAnswerLandsUnderItsOwnName is what askEveryoneWhoCaptures() does
// beyond running the list: each blob is kept under its own integration's name,
// and neither a failure nor an empty answer leaves an entry behind.
//
// An empty answer is kept and a failure is not, and whoToAsk tells them
// apart: an integration with no entry is asked again on every hook until it
// is fixed, and one with nothing to read has its empty object and is not.
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
		"pane":   {Binary: program(t, dir, "pane", `{"PANE":"7"}`, 0)},
		"window": {Binary: program(t, dir, "window", `{"WINDOW":"left"}`, 0)},
		"broken": {Binary: program(t, dir, "broken", `nonsense`, 1)},
		"bar":    {Binary: program(t, dir, "bar", `{}`, 0)},
	}

	captured := askEveryoneWhoCaptures(settings,
		[]string{"bar", "broken", "pane", "window"}, slog.New(slog.DiscardHandler), nil)

	if got := owners(captured.By); !slices.Equal(got, []string{"bar", "pane", "window"}) {
		t.Fatalf("captured for %v, want everybody that answered", got)
	}
	if got := string(captured.By["bar"]); got != `{}` {
		t.Errorf("bar's blob is %s, want the empty object it printed", got)
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

	captured := askEveryoneWhoCaptures(
		config.Defaults(), nil, slog.New(slog.DiscardHandler), chain)

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

// TestOnlyWhatIsMissingIsCapturedForOneUnchangedSession is the point of the
// whole arrangement: the steady state spawns nothing, and a session that is
// missing one integration's answer spawns that one and no other (D-90).
//
// No filesystem and no subprocess anywhere in it, which is itself the claim —
// the decision is a record, a list and a pid, and that is exactly why it can
// be made before anything is run.
func TestOnlyWhatIsMissingIsCapturedForOneUnchangedSession(t *testing.T) {
	running := session.Process{PID: 4242}
	stored := session.Record{
		Process: running,
		CapturedContext: session.CapturedContext{
			CapturedAt: time.Now().UTC(),
			By:         map[string]json.RawMessage{"pane": json.RawMessage(`{"PANE":"7"}`)},
		},
	}

	for _, one := range []struct {
		what      string
		previous  session.Record
		runnable  []string
		process   session.Process
		wantAsked []string
		wantStale bool
	}{
		{"nothing stored yet", session.Record{}, []string{"pane"}, running, []string{"pane"}, true},
		{"nothing moved", stored, []string{"pane"}, running, nil, false},
		{"the session came back in a new process", stored, []string{"pane"},
			session.Process{PID: 9999}, []string{"pane"}, true},
		{"an integration installed since", stored, []string{"pane", "window"}, running,
			[]string{"window"}, false},
		{
			// Its entry stays with the record and nothing asks for it: the
			// environment it read has not changed.
			"an integration removed since", stored, nil, running, nil, false,
		},
		{
			// One that is run and fails is never in `by`, so it alone is asked
			// again on every hook until it is fixed (D-58) — and the one that
			// answered is left alone, with what was derived from it.
			"one that keeps failing", stored, []string{"broken", "pane"}, running,
			[]string{"broken"}, false,
		},
		{
			// A machine with no capturing integrations stores its ancestry once
			// and is then left alone.
			"nothing captures, and nothing ever did",
			session.Record{
				Process:         running,
				CapturedContext: session.CapturedContext{CapturedAt: time.Now().UTC()},
			},
			nil, running, nil, false,
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
			[]string{"pane"}, session.Process{}, nil, false,
		},
	} {
		t.Run(one.what, func(t *testing.T) {
			asked, stale := whoToAsk(one.previous, one.runnable, one.process)
			if !slices.Equal(asked, one.wantAsked) || stale != one.wantStale {
				t.Errorf("whoToAsk = %v, %v; want %v, %v", asked, stale, one.wantAsked, one.wantStale)
			}
		})
	}
}

func owners(by map[string]json.RawMessage) []string {
	names := make([]string, 0, len(by))
	for name := range by {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
