package main

import (
	"fmt"
	"slices"
	"testing"

	"github.com/lassoColombo/agent-notify/session"
)

// TestWakingOnTheKernelLosesNoBanner is D-73 for a display that draws edges
// rather than state. A banner reads the name, the detail and the message, and
// D-73's sweep would ask to be woken for all of them; but a banner is only
// ever for a kernel that moved, and the view it is handed carries the whole
// record as it is then. So one stream of writes is shown twice — once waking
// on everything, once on what WhatToWakeFor names — and the banners must be
// the same (D-138).
func TestWakingOnTheKernelLosesNoBanner(t *testing.T) {
	alpha := aSession("alpha", session.Working, nine)
	var stream [][]session.Record
	write := func(change func(*session.Record)) {
		change(&alpha)
		alpha.Rank = alpha.Kernel.Rank()
		stream = append(stream, []session.Record{alpha})
	}
	write(func(r *session.Record) {})
	write(func(r *session.Record) { r.Message = "reading the tests" })
	write(func(r *session.Record) {
		r.Kernel, r.Detail, r.Message = session.BlockedOnYou, "permission-prompt", "Shall I delete the branch?"
	})
	write(func(r *session.Record) { r.Message = "Shall I delete the branch? It has no upstream." })
	write(func(r *session.Record) { r.Name = "branch-cleanup" })
	write(func(r *session.Record) { r.Kernel, r.Detail = session.Working, "" })
	write(func(r *session.Record) { r.Kernel, r.Message = session.FinishedATurn, "Deleted." })
	write(func(r *session.Record) { r.Cwd = "/Users/somebody/projects/elsewhere" })
	write(func(r *session.Record) { r.Kernel, r.Detail = session.Broke, "server-error" })

	everything, named := bannersFor(stream, nil), bannersFor(stream, WhatToWakeFor())
	if !slices.Equal(everything, named) {
		t.Errorf("waking on %v, the banners are\n%v\nand waking on everything they are\n%v",
			WhatToWakeFor(), named, everything)
	}
	if len(everything) != 3 {
		t.Errorf("the stream made %d banners, want the three it moved into a state worth one", len(everything))
	}
}

// bannersFor is every banner a stream of worlds produces for a display that
// asked to be woken for wakeOn.
func bannersFor(stream [][]session.Record, wakeOn []string) []string {
	var shown session.LastShown
	var banners []string
	for _, world := range stream {
		view, worth := shown.Replace(world, wakeOn)
		if !worth {
			continue
		}
		for _, notice := range notifier().Fresh(view.Changed) {
			banners = append(banners, fmt.Sprintf("%s | %s | %s", notice.Title, notice.Subtitle, notice.Body))
		}
	}
	return banners
}
