package main

import (
	"fmt"
	"testing"

	"github.com/lassoColombo/agent-notify/session"
)

// TestEveryFieldAMarkReadsIsOneItWakesFor asks the planner what it reads
// rather than trusting the list (D-72, D-73).
func TestEveryFieldAMarkReadsIsOneItWakesFor(t *testing.T) {
	const socket = "/tmp/tmux-501/default"
	rows := []Row{row("%7", "@1", "", ""), row("%8", "@1", "", "")}
	base := placed("alpha", session.Working, socket, "%7")
	base.Detail = "running-bash"
	base.Cwd = "/Users/somebody/projects/agent-notify"

	missing := session.FieldsRenderedButNotWokenFor(base, WhatToWakeFor(), func(record session.Record) string {
		return fmt.Sprintf("%+v", Plan(socket, []session.Record{record}, rows, testGlyphs))
	})
	if len(missing) > 0 {
		t.Errorf("a different plan comes out when %v move, and this display does not ask to be woken for them", missing)
	}
}
