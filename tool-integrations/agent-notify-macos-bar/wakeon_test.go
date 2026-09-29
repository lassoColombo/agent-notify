package main

import (
	"fmt"
	"testing"

	"github.com/lassoColombo/agent-notify/session"
)

// TestEveryFieldTheMenuReadsIsOneItWakesFor asks the renderer what it reads
// rather than trusting the list (D-72, D-73).
func TestEveryFieldTheMenuReadsIsOneItWakesFor(t *testing.T) {
	base := aSession("alpha", session.Working, nine)
	base.Detail = "running-bash"
	base.Message = "Shall I delete the branch?"
	base.Cwd = "/Users/somebody/projects/agent-notify"

	missing := session.FieldsRenderedButNotWokenFor(base, WhatToWakeFor(), func(record session.Record) string {
		shown, arrival := Render(bar(), []session.Record{record})
		return fmt.Sprintf("%+v|%+v", shown, arrival)
	})
	if len(missing) > 0 {
		t.Errorf("the menu draws differently when %v move, and this display does not ask to be woken for them", missing)
	}
}
