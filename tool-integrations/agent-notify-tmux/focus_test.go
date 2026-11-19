package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lassoColombo/agent-notify/container"
)

// These run against a real tmux, for the same reason the display's do.
//
// They also assert whether a focus moved anybody, because a client does not
// have to be a person: a pane of a second server running `tmux attach` to the
// first IS a real attached client, with a real tty, reported by `list-clients`
// like any other. That is what `viewer` builds.

// viewer attaches a real client to `target` on `socket` and returns its name
// once tmux can see it.
func viewer(t *testing.T, tmux Tmux, socket, target string) string {
	t.Helper()
	directory, err := os.MkdirTemp("", "an-tmux-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	host := filepath.Join(directory, "h")
	t.Cleanup(func() {
		_, _ = bare(tmux, host, "kill-server")
		_ = os.RemoveAll(directory)
	})
	must(t, tmux, host, "new-session", "-d", "-x", "120", "-y", "40",
		"env -u TMUX "+tmux.Binary+" -f /dev/null -S "+socket+" attach -t "+target)

	deadline := time.Now().Add(15 * time.Second)
	for {
		clients, err := tmux.Clients(socket)
		if err == nil && len(clients) > 0 {
			return clients[0].Name
		}
		if time.Now().After(deadline) {
			t.Fatalf("nothing ever attached to %q (last answer: %v)", target, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// onScreen is the pane a client is showing, asked of the client itself.
func onScreen(t *testing.T, tmux Tmux, socket, client string) string {
	t.Helper()
	return must(t, tmux, socket, "display-message", "-p", "-c", client, "#{pane_id}")
}

func captureOf(socket, index, pane string) json.RawMessage {
	captured, _ := json.Marshal(Captured{Server: socket + ",4242," + index, Pane: pane})
	return captured
}

// TestInterpretFindsTheWindow is the whole of placing: the environment says
// which pane of which server, and only tmux knows which session and window
// hold it now.
func TestInterpretFindsTheWindow(t *testing.T) {
	tmux := realTmux(t)
	socket := aServer(t, tmux, "an-ctr")
	pane := must(t, tmux, socket, "new-window", "-P", "-F", "#{pane_id}", "-n", "agent", "-t", "an-ctr", "sleep", "600")

	got, err := Interpret(tmux, captureOf(socket, "0", pane))
	if err != nil {
		t.Fatalf("Interpret: %v", err)
	}
	coordinates := got.(Coordinates)
	if coordinates.Socket != socket || coordinates.Pane != pane || coordinates.Session != "an-ctr" {
		t.Errorf("coordinates = %+v, want server %q pane %s in session an-ctr", coordinates, socket, pane)
	}
	if coordinates.WindowName != "agent" || coordinates.Window != 1 {
		t.Errorf("coordinates = %+v — the window is what makes a record legible", coordinates)
	}
}

func TestAPaneThatIsGoneIsNamedAsGoneByARealTmux(t *testing.T) {
	tmux := realTmux(t)
	socket := aServer(t, tmux, "an-ctr")

	coordinates, _ := json.Marshal(Coordinates{Socket: socket, Pane: "%9999"})
	outcome, err := Focus(tmux, coordinates)
	if err != nil {
		t.Fatalf("Focus: %v", err)
	}
	if outcome.OK || outcome.Problem != container.PlaceIsGone {
		t.Errorf("outcome = %+v, want place-is-gone", outcome)
	}
}

// TestAServerThatIsGoneIsNotAPaneProblem: "the pane is gone" should offer to
// prune the record and "tmux is not running" should not, which is the whole
// reason these are two words and not one message (§A11.3).
func TestAServerThatIsGoneIsNotAPaneProblem(t *testing.T) {
	tmux := realTmux(t)
	directory, err := os.MkdirTemp("", "an-tmux-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(directory)

	coordinates, _ := json.Marshal(Coordinates{Socket: filepath.Join(directory, "s"), Pane: "%1"})
	outcome, err := Focus(tmux, coordinates)
	if err != nil {
		t.Fatalf("Focus: %v", err)
	}
	if outcome.OK || outcome.Problem != container.NotRunning {
		t.Errorf("outcome = %+v, want not-running", outcome)
	}
}

// TestFocusOnASessionNobodyIsLookingAt: select-window and select-pane on a
// detached session succeed and change no screen, so they are not run at all.
//
// The empty $TMUX is the test saying which caller it stands in for: `go test`
// is often run from a tmux pane, and a focus asked from one may bring that
// terminal along.
func TestFocusOnASessionNobodyIsLookingAt(t *testing.T) {
	tmux := realTmux(t)
	socket := aServer(t, tmux, "an-ctr")
	pane := must(t, tmux, socket, "display-message", "-p", "-t", "an-ctr", "#{pane_id}")
	t.Setenv(serverVariable, "")

	coordinates, _ := json.Marshal(Coordinates{Socket: socket, Pane: pane})
	outcome, err := Focus(tmux, coordinates)
	if err != nil {
		t.Fatalf("Focus: %v", err)
	}
	if outcome.OK || outcome.Problem != NotAttached {
		t.Errorf("outcome = %+v, want not-attached", outcome)
	}
	if !strings.Contains(outcome.Detail, "attach -t an-ctr") {
		t.Errorf("detail = %q — a named failure should say what to do next", outcome.Detail)
	}

	verdict, err := Focused(tmux, coordinates)
	if err != nil {
		t.Fatalf("Focused: %v", err)
	}
	if verdict.Answer == container.Yes {
		t.Errorf("a session nobody is attached to reported a focused pane: %+v", verdict)
	}
}

// TestFocusPutsThePaneInFrontOfAnAttachedClient: with somebody looking, the
// window and the pane move, and the proof is read off the client.
func TestFocusPutsThePaneInFrontOfAnAttachedClient(t *testing.T) {
	tmux := realTmux(t)
	socket := aServer(t, tmux, "an-ctr")
	pane := must(t, tmux, socket, "display-message", "-p", "-t", "an-ctr", "#{pane_id}")
	// A split, zoomed on the other pane, in a window that is then left: the
	// pane is behind on every count there is.
	other := must(t, tmux, socket, "split-window", "-P", "-F", "#{pane_id}", "-t", pane, "sleep", "600")
	must(t, tmux, socket, "resize-pane", "-Z", "-t", other)
	must(t, tmux, socket, "new-window", "-t", "an-ctr", "sleep", "600")
	client := viewer(t, tmux, socket, "an-ctr")
	t.Setenv(serverVariable, "")

	interpreted, err := Interpret(tmux, captureOf(socket, "0", pane))
	if err != nil {
		t.Fatalf("Interpret: %v", err)
	}
	coordinates, _ := json.Marshal(interpreted)
	if verdict, _ := Focused(tmux, coordinates); verdict.Answer != container.No {
		t.Fatalf("focused = %+v before the focus, want no", verdict)
	}

	outcome, err := Focus(tmux, coordinates)
	if err != nil {
		t.Fatalf("Focus: %v", err)
	}
	if !outcome.OK {
		t.Fatalf("outcome = %+v", outcome)
	}
	if shown := onScreen(t, tmux, socket, client); shown != pane {
		t.Errorf("focus said yes and the client shows %s, not %s", shown, pane)
	}
	if verdict, _ := Focused(tmux, coordinates); verdict.Answer != container.Yes {
		t.Errorf("focused = %+v, want yes right after a focus that succeeded", verdict)
	}
}

// TestAFocusAskedFromInsideTheServerBringsTheTerminal: the agent's session has
// nobody on it, and the focus is asked from a terminal on another session of
// the same server — the picker in a popup. That terminal goes.
func TestAFocusAskedFromInsideTheServerBringsTheTerminal(t *testing.T) {
	tmux := realTmux(t)
	socket := aServer(t, tmux, "an-ctr")
	pane := must(t, tmux, socket, "display-message", "-p", "-t", "an-ctr", "#{pane_id}")
	must(t, tmux, socket, "new-session", "-d", "-s", "an-here", "sleep", "600")
	here := strings.TrimPrefix(must(t, tmux, socket, "display-message", "-p", "-t", "an-here", "#{session_id}"), "$")
	client := viewer(t, tmux, socket, "an-here")
	t.Setenv(serverVariable, socket+",4242,"+here)

	coordinates, _ := json.Marshal(Coordinates{Socket: socket, Pane: pane})
	outcome, err := Focus(tmux, coordinates)
	if err != nil {
		t.Fatalf("Focus: %v", err)
	}
	if !outcome.OK {
		t.Fatalf("outcome = %+v", outcome)
	}
	if shown := onScreen(t, tmux, socket, client); shown != pane {
		t.Errorf("the terminal shows %s, not %s", shown, pane)
	}
}
