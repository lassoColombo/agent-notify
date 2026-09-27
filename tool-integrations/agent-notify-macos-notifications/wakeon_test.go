package main

import (
	"fmt"
	"slices"
	"testing"

	"github.com/lassoColombo/agent-notify/session"
	"github.com/lassoColombo/agent-notify/subscribe"
)

// TestEveryFieldABannerReadsIsOneItWakesFor is D-73.
//
// A WakeOn list is a promise about what this display looks at, kept by hand,
// in a different file from the code that does the looking. Kept by hand it
// drifts: sketchybar drew the agent's message in a popup for weeks without
// asking to be woken when the message changed, so a second prompt queued at a
// working agent left the popup showing the first one (D-72).
//
// So this does not read the list and check it. It asks the notifier. Move one
// field of a record, ask again, and if a different banner came out then the
// field is one this display reads and the declaration has to name it.
//
// The previous kernel is held at `working` throughout, so that every record
// the sweep produces is still a transition worth announcing and the only thing
// varying is the field under test.
func TestEveryFieldABannerReadsIsOneItWakesFor(t *testing.T) {
	said := func(record session.Record) string {
		return fmt.Sprintf("%+v", notifier().Fresh([]subscribe.Change{
			{Record: record, PreviousKernel: session.Working},
		}))
	}

	base := aSession("alpha", session.BlockedOnYou, nine)
	base.Detail = "permission-prompt"
	base.Message = "Shall I delete the branch?"
	base.Cwd = "/Users/somebody/projects/agent-notify"
	wakeOn := WhatToWakeFor()

	for field, moved := range session.EachFieldMoved(base) {
		if said(base) == said(moved) {
			continue
		}
		if !slices.Contains(wakeOn, field) {
			t.Errorf("a different banner comes out when %q moves, and this display does "+
				"not ask to be woken for it: the change would reach somebody only when "+
				"something else happened to move too", field)
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
