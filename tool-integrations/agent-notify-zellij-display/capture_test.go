package main

import (
	"encoding/json"
	"testing"

	"github.com/lassoColombo/agent-notify/session"
)

// TestWhatCaptureReturnsIsWhatPlaceOfReads is the loop that could not be closed
// before D-57.
//
// The two halves — what is read out of the agent's environment, and what is
// read back off a record — used to be joined by a line in the user's config
// file, so the strongest thing a test could do was compare that line against a
// const in this package and hope nobody edited it. Both halves are now in this
// program, so the join is testable: capture, encode exactly as core encodes,
// decode exactly as the display decodes.
func TestWhatCaptureReturnsIsWhatPlaceOfReads(t *testing.T) {
	t.Setenv(sessionVariable, "home")
	t.Setenv(paneVariable, "7")

	captured, err := Capture()
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	blob, err := json.Marshal(captured)
	if err != nil {
		t.Fatalf("what Capture returned does not encode: %v", err)
	}

	record := session.Record{
		CapturedContext: session.CapturedContext{
			By: map[string]json.RawMessage{Name: blob},
		},
	}
	place, found := PlaceOf(record)
	if !found {
		t.Fatalf("PlaceOf could not read what Capture wrote: %s", blob)
	}
	if place.Session != "home" || place.Pane != 7 {
		t.Errorf("PlaceOf = %+v, want session home pane 7", place)
	}
}

// A session outside zellij captures two empty strings, and that must read as
// "this display has nothing to say about it" rather than as pane zero of the
// session with no name. Pane 0 is a real pane, so the discriminator is the
// session name (§A11.7).
func TestASessionOutsideZellijIsNotPlaced(t *testing.T) {
	t.Setenv(sessionVariable, "")
	t.Setenv(paneVariable, "")

	captured, _ := Capture()
	blob, _ := json.Marshal(captured)
	record := session.Record{
		CapturedContext: session.CapturedContext{
			By: map[string]json.RawMessage{Name: blob},
		},
	}
	if place, found := PlaceOf(record); found {
		t.Errorf("PlaceOf = %+v, found, want nothing for a session outside zellij", place)
	}
}
