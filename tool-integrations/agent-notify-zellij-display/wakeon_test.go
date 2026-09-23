package main

import (
	"fmt"
	"slices"
	"testing"

	agentnotify "github.com/lassoColombo/agent-notify"
)

// TestEveryFieldATitleReadsIsOneItWakesFor is D-73.
//
// A WakeOn list is a promise about what this display looks at, kept by hand,
// in a different file from the render that does the looking. Kept by hand it
// drifts: sketchybar drew the agent's message in a popup for weeks without
// asking to be woken when the message changed, so a second prompt queued at a
// working agent left the popup showing the first one (D-72).
//
// So this does not read the list and check it. It asks the planner. Move one
// field of a record, plan again, and if a different set of renames came out
// then the field is one this display reads and the declaration has to name it.
func TestEveryFieldATitleReadsIsOneItWakesFor(t *testing.T) {
	const zellijSession = "home"
	panes := []Pane{pane(7, "shell", 1, "one"), pane(8, "other", 1, "one")}

	planned := func(record agentnotify.Record) string {
		return fmt.Sprintf("%+v", Plan(zellijSession,
			[]agentnotify.Record{record}, panes, testGlyphs))
	}

	base := placed("alpha", agentnotify.Working, zellijSession, 7)
	base.Detail = "running-bash"
	base.Cwd = "/Users/somebody/projects/agent-notify"
	wakeOn := WhatToWakeFor()

	for field, moved := range agentnotify.EachFieldMoved(base) {
		if planned(base) == planned(moved) {
			continue
		}
		if !slices.Contains(wakeOn, field) {
			t.Errorf("the titles come out different when %q moves, and this display does "+
				"not ask to be woken for it: the change would reach the pane only when "+
				"something else happened to move too", field)
		}
	}
}

// TestWhatItWakesForIsSpelledTheWayARecordIs, which core refuses at startup
// (D-70) — but at startup is late to find out, and this says so in a second.
func TestWhatItWakesForIsSpelledTheWayARecordIs(t *testing.T) {
	if err := agentnotify.ReasonTheseFieldsCannotBeWokenOn(WhatToWakeFor()); err != nil {
		t.Error(err)
	}
}
