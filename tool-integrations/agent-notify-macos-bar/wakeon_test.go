package main

import (
	"fmt"
	"slices"
	"testing"

	"github.com/lassoColombo/agent-notify/session"
)

// TestEveryFieldTheMenuReadsIsOneItWakesFor is D-73.
//
// A WakeOn list is a promise about what this display looks at, kept by hand,
// in a different file from the render that does the looking. Kept by hand it
// drifts: sketchybar drew the agent's message in a popup for weeks without
// asking to be woken when the message changed, so a second prompt queued at a
// working agent left the popup showing the first one (D-72).
//
// So this does not read the list and check it. It asks the renderer. Move one
// field of a record, render again, and if the menu came out different then the
// renderer reads that field and the declaration has to name it. A line added
// to a row next year fails here the day it is added, without anybody having
// thought about it.
func TestEveryFieldTheMenuReadsIsOneItWakesFor(t *testing.T) {
	drawn := func(record session.Record) string {
		shown, arrival := Render(bar(), []session.Record{record})
		return fmt.Sprintf("%+v|%+v", shown, arrival)
	}

	base := aSession("alpha", session.Working, nine)
	base.Detail = "running-bash"
	base.Message = "Shall I delete the branch?"
	base.Cwd = "/Users/somebody/projects/agent-notify"
	wakeOn := WhatToWakeFor()

	for field, moved := range session.EachFieldMoved(base) {
		if drawn(base) == drawn(moved) {
			continue
		}
		if !slices.Contains(wakeOn, field) {
			t.Errorf("the menu draws differently when %q moves, and this display does not "+
				"ask to be woken for it: the change would reach the bar only when something "+
				"else happened to move too", field)
		}
	}
}

// TestWhatItWakesForIsSpelledTheWayARecordIs, which core refuses at startup
// (D-70) — but at startup is late to find out, and this says so in a second.
func TestWhatItWakesForIsSpelledTheWayARecordIs(t *testing.T) {
	if err := session.ReasonTheseFieldsCannotBeWokenOn(WhatToWakeFor()); err != nil {
		t.Error(err)
	}
}
