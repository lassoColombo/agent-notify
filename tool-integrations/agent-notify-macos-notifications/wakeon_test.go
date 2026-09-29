package main

import (
	"fmt"
	"testing"

	"github.com/lassoColombo/agent-notify/session"
)

// TestEveryFieldABannerReadsIsOneItWakesFor asks the notifier what it reads
// rather than trusting the list (D-72, D-73). The previous kernel is held at
// `working` so that every record is still a transition worth announcing.
func TestEveryFieldABannerReadsIsOneItWakesFor(t *testing.T) {
	base := aSession("alpha", session.BlockedOnYou, nine)
	base.Detail = "permission-prompt"
	base.Message = "Shall I delete the branch?"
	base.Cwd = "/Users/somebody/projects/agent-notify"

	missing := session.FieldsRenderedButNotWokenFor(base, WhatToWakeFor(), func(record session.Record) string {
		return fmt.Sprintf("%+v", notifier().Fresh([]session.Change{
			{Record: record, PreviousKernel: session.Working},
		}))
	})
	if len(missing) > 0 {
		t.Errorf("a different banner comes out when %v move, and this display does not ask to be woken for them", missing)
	}
}
