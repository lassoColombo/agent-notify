package main

import (
	"encoding/json"
	"strings"
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
	record := session.Record{CapturedContext: session.CapturedContext{By: map[string]json.RawMessage{Name: blob}}}

	place, found := PlaceOf(record)
	want := Place{Socket: "/private/tmp/tmux-501/default", Server: 4242, Pane: "%7"}
	if !found || place != want {
		t.Errorf("PlaceOf = %+v, %v, want %+v", place, found, want)
	}
}

// TestWhereNothingIsPlaced: a session outside tmux captures two empty
// strings, and a popup or a `run-shell` has $TMUX and no $TMUX_PANE [measured
// on 3.8]. Neither is anywhere, and interpreting either says why.
func TestWhereNothingIsPlaced(t *testing.T) {
	for _, given := range []struct {
		captured Captured
		why      string
	}{
		{Captured{}, "not started inside tmux"},
		{Captured{Server: "/tmp/tmux-501/default,4242,0"}, "outside any pane"},
		{Captured{Server: "/tmp/tmux-501/default", Pane: "%7"}, "does not name a server"},
		{Captured{Server: "/tmp/tmux-501/default,4242,0", Pane: "7"}, "not a pane id"},
	} {
		blob, _ := json.Marshal(given.captured)
		record := session.Record{CapturedContext: session.CapturedContext{By: map[string]json.RawMessage{Name: blob}}}
		if place, found := PlaceOf(record); found {
			t.Errorf("%+v is placed at %+v", given.captured, place)
		}
		if _, err := Interpret(blob); err == nil || !strings.Contains(err.Error(), given.why) {
			t.Errorf("interpreting %+v: %v, want an error saying %q", given.captured, err, given.why)
		}
	}
}

// TestInterpretingIsTheCaptureReadBack: interpretation asks tmux nothing — it
// is handed no tmux to ask — and what it returns is what focus reads.
func TestInterpretingIsTheCaptureReadBack(t *testing.T) {
	blob, _ := json.Marshal(Captured{Server: "/tmp/tmux-501/default,4242,3", Pane: "%12"})
	interpreted, err := Interpret(blob)
	if err != nil {
		t.Fatalf("Interpret: %v", err)
	}
	coordinates, _ := json.Marshal(interpreted)
	place, ours := placeIn(coordinates)
	if want := (Place{Socket: "/tmp/tmux-501/default", Server: 4242, Pane: "%12"}); !ours || place != want {
		t.Errorf("read back %+v, %v from %s, want %+v", place, ours, coordinates, want)
	}
}

// TestCoordinatesWithoutTheServerAreNotOurs: the shape before D-126 named a
// socket and a pane, and a pane id alone may be somebody else's pane in a
// server started again on that socket.
func TestCoordinatesWithoutTheServerAreNotOurs(t *testing.T) {
	for _, coordinates := range []string{
		`{"socket":"/tmp/tmux-501/default","pane":"%3","session":"home","window":1}`,
		`{"socket":"/tmp/tmux-501/default","server":4242,"pane":"3"}`,
		`{"server":4242,"pane":"%3"}`,
		`null`,
		`"%3"`,
	} {
		if place, ours := placeIn(json.RawMessage(coordinates)); ours {
			t.Errorf("%s was read as %+v", coordinates, place)
		}
	}
}

// TestTheServerIsCutFromTheRight: the socket is whatever TMUX_TMPDIR made it,
// commas included, and the two fields after it are numbers.
func TestTheServerIsCutFromTheRight(t *testing.T) {
	socket, pid, sessionID, found := serverOf("/tmp/a,b/tmux-501/default,4242,3")
	if !found || socket != "/tmp/a,b/tmux-501/default" || pid != 4242 || sessionID != "$3" {
		t.Errorf("serverOf = %q %d %q %v", socket, pid, sessionID, found)
	}
	for _, value := range []string{"", "/tmp/x", "/tmp/x,4242", "/tmp/x,abc,0", "/tmp/x,4242,", ",4242,0", "/tmp/x,0,0"} {
		if _, _, _, found := serverOf(value); found {
			t.Errorf("serverOf(%q) found a server", value)
		}
	}
}
