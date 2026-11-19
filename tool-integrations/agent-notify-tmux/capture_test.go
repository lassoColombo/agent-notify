package main

import (
	"encoding/json"
	"testing"

	"github.com/lassoColombo/agent-notify/session"
)

// TestWhatCaptureReturnsIsWhatPlaceOfReads closes the loop D-57 opened: both
// halves — what is read out of the agent's environment, and what is read back
// off a record — are in this program, so the join is testable: capture, encode
// exactly as core encodes, decode exactly as the display decodes.
func TestWhatCaptureReturnsIsWhatPlaceOfReads(t *testing.T) {
	t.Setenv(serverVariable, "/private/tmp/tmux-501/default,4242,0")
	t.Setenv(paneVariable, "%7")

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
	if place.Socket != "/private/tmp/tmux-501/default" || place.Pane != "%7" {
		t.Errorf("PlaceOf = %+v, want the default socket and pane %%7", place)
	}
}

// A session outside tmux captures two empty strings, and that must read as
// "this display has nothing to say about it" (§A11.7).
func TestASessionOutsideTmuxIsNotPlaced(t *testing.T) {
	t.Setenv(serverVariable, "")
	t.Setenv(paneVariable, "")

	captured, _ := Capture()
	blob, _ := json.Marshal(captured)
	record := session.Record{
		CapturedContext: session.CapturedContext{
			By: map[string]json.RawMessage{Name: blob},
		},
	}
	if place, found := PlaceOf(record); found {
		t.Errorf("PlaceOf = %+v, found, want nothing for a session outside tmux", place)
	}
}

// TestAProcessOfTmuxThatIsNotInAPaneIsNowhere: a popup or a `run-shell` has
// $TMUX and no $TMUX_PANE [measured on 3.8]. It is inside the server and in no
// pane, so there is nothing to paint and nothing to focus.
func TestAProcessOfTmuxThatIsNotInAPaneIsNowhere(t *testing.T) {
	blob, _ := json.Marshal(Captured{Server: "/tmp/tmux-501/default,4242,1"})
	record := session.Record{
		CapturedContext: session.CapturedContext{
			By: map[string]json.RawMessage{Name: blob},
		},
	}
	if place, found := PlaceOf(record); found {
		t.Errorf("PlaceOf = %+v, found, want nothing for a process in no pane", place)
	}
}

func TestTheSocketIsCutFromTheRight(t *testing.T) {
	for _, one := range []struct {
		server  string
		socket  string
		session string
		found   bool
	}{
		{"/private/tmp/tmux-501/default,4242,0", "/private/tmp/tmux-501/default", "$0", true},
		// TMUX_TMPDIR is the user's, and a comma in it is legal.
		{"/tmp/odd,dir/default,4242,12", "/tmp/odd,dir/default", "$12", true},
		{"", "", "", false},
		{"/tmp/tmux-501/default", "", "", false},
		{"/tmp/tmux-501/default,pid,0", "", "", false},
		{",4242,0", "", "", false},
	} {
		socket, found := SocketOf(one.server)
		if found != one.found || socket != one.socket {
			t.Errorf("SocketOf(%q) = %q, %v; want %q, %v", one.server, socket, found, one.socket, one.found)
		}
		if !one.found {
			continue
		}
		if session, _ := SessionOf(one.server); session != one.session {
			t.Errorf("SessionOf(%q) = %q, want %q", one.server, session, one.session)
		}
	}
}

func TestCaptureReadsTheEnvironmentAndNothingElse(t *testing.T) {
	t.Setenv(serverVariable, "/tmp/tmux-501/default,4242,0")
	t.Setenv(paneVariable, "%17")

	captured, err := Capture()
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	encoded, err := json.Marshal(captured)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var back map[string]string
	if err := json.Unmarshal(encoded, &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if back[serverVariable] != "/tmp/tmux-501/default,4242,0" || back[paneVariable] != "%17" {
		t.Errorf("captured %v", back)
	}
	// Nothing else may travel. An agent's environment holds API keys, and what
	// reaches the disk is bounded by what this returned (§A15, R11).
	if len(back) != 2 {
		t.Errorf("captured %d values, want exactly the two that were asked for: %v", len(back), back)
	}
}
