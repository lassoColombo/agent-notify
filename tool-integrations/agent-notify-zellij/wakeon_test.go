package main

import (
	"fmt"
	"testing"

	"github.com/lassoColombo/agent-notify/session"
)

// TestEveryFieldATitleReadsIsOneItWakesFor asks the planner what it reads
// rather than trusting the list (D-72, D-73).
func TestEveryFieldATitleReadsIsOneItWakesFor(t *testing.T) {
	const zellijSession = "home"
	panes := []Pane{pane(7, "shell", 1, "one"), pane(8, "other", 1, "one")}
	base := placed("alpha", session.Working, zellijSession, 7)
	base.Detail = "running-bash"
	base.Cwd = "/Users/somebody/projects/agent-notify"

	missing := session.FieldsRenderedButNotWokenFor(base, WhatToWakeFor(), func(record session.Record) string {
		return fmt.Sprintf("%+v", Plan(zellijSession, []session.Record{record}, panes, testGlyphs))
	})
	if len(missing) > 0 {
		t.Errorf("a different plan comes out when %v move, and this display does not ask to be woken for them", missing)
	}
}
