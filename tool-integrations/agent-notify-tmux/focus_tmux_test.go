package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lassoColombo/agent-notify/container"
)

// These run against a real tmux, for the same reason the marks' do, and they
// assert whose screen moved. A terminal does not have to be a person: a pane
// of a second server running `tmux attach` to the first IS a real attached
// client, with a real tty, reported by `list-clients` like any other, and
// keys sent to that pane reach it as a terminal's would — the focus reports
// among them.

// Viewer is one such terminal.
type Viewer struct {
	Name string
	// host is the second server, whose one session is the terminal.
	host string
}

// viewer attaches a terminal to `target` and returns it once tmux lists it.
func viewer(t *testing.T, tmux Tmux, socket, target string) Viewer {
	t.Helper()
	seen := map[string]bool{}
	if server, err := tmux.Look(socket); err == nil {
		for _, client := range server.Clients {
			seen[client.Name] = true
		}
	}
	directory, err := os.MkdirTemp("", "an-tmux-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	host := filepath.Join(directory, "h")
	t.Cleanup(func() {
		_, _ = bare(tmux, host, "kill-server")
		_ = os.RemoveAll(directory)
	})
	must(t, tmux, host, "new-session", "-d", "-s", "terminal", "-x", "120", "-y", "40",
		"env -u TMUX "+tmux.Binary+" -f /dev/null -S "+socket+" attach -t "+target)

	deadline := time.Now().Add(15 * time.Second)
	for {
		server, err := tmux.Look(socket)
		if err == nil {
			for _, client := range server.Clients {
				if !seen[client.Name] && client.Pane != "" {
					return Viewer{Name: client.Name, host: host}
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("nothing ever attached to %q (last answer: %v)", target, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// lose and gain are the terminal's own focus reports, ESC [ O and ESC [ I,
// arriving at its tmux client; each waits until tmux has heard it.
func (v Viewer) lose(t *testing.T, tmux Tmux, socket string) { v.report(t, tmux, socket, "4f", false) }
func (v Viewer) gain(t *testing.T, tmux Tmux, socket string) { v.report(t, tmux, socket, "49", true) }

func (v Viewer) report(t *testing.T, tmux Tmux, socket, last string, focused bool) {
	t.Helper()
	must(t, tmux, v.host, "send-keys", "-t", "terminal", "-H", "1b", "5b", last)
	deadline := time.Now().Add(5 * time.Second)
	for v.client(t, tmux, socket).Focused != focused {
		if time.Now().After(deadline) {
			t.Fatalf("tmux never heard terminal %s's focus go to %v", v.Name, focused)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (v Viewer) client(t *testing.T, tmux Tmux, socket string) Client {
	t.Helper()
	server, err := tmux.Look(socket)
	if err != nil {
		t.Fatalf("Look: %v", err)
	}
	for _, client := range server.Clients {
		if client.Name == v.Name {
			return client
		}
	}
	t.Fatalf("terminal %s is not attached", v.Name)
	return Client{}
}

func (v Viewer) shows(t *testing.T, tmux Tmux, socket string) string {
	t.Helper()
	return v.client(t, tmux, socket).Pane
}

func coordinatesOf(t *testing.T, tmux Tmux, socket, pane string) json.RawMessage {
	t.Helper()
	coordinates, _ := json.Marshal(Place{Socket: socket, Server: pidOf(t, tmux, socket), Pane: pane})
	return coordinates
}

// outside is a focus asked from no tmux at all: a banner, a script. `go test`
// is often run from a tmux pane, and a focus asked from one would bring that
// terminal along.
func outside(t *testing.T) {
	t.Setenv(serverVariable, "")
	t.Setenv(paneVariable, "")
}

// TestFocusBringsThePaneToTheTerminal: the pane is behind on every count
// there is — another window is in front, and its own window is zoomed on
// another pane — and one command brings it, which the terminal then shows.
func TestFocusBringsThePaneToTheTerminal(t *testing.T) {
	tmux := realTmux(t)
	socket := aServer(t, tmux, "an-agent")
	pane := paneOf(t, tmux, socket, "an-agent")
	other := must(t, tmux, socket, "split-window", "-P", "-F", "#{pane_id}", "-t", pane, "sleep", "600")
	must(t, tmux, socket, "resize-pane", "-Z", "-t", other)
	must(t, tmux, socket, "new-window", "-t", "an-agent", "sleep", "600")
	terminal := viewer(t, tmux, socket, "an-agent")
	outside(t)

	coordinates := coordinatesOf(t, tmux, socket, pane)
	if verdict, _ := Focused(tmux, coordinates); verdict.Answer != container.No {
		t.Fatalf("focused = %+v before the focus, want no", verdict)
	}
	outcome, err := Focus(tmux, coordinates)
	if err != nil || !outcome.OK {
		t.Fatalf("Focus = %+v, %v", outcome, err)
	}
	if shown := terminal.shows(t, tmux, socket); shown != pane {
		t.Errorf("focus said yes and the terminal shows %s, not %s", shown, pane)
	}
	if verdict, _ := Focused(tmux, coordinates); verdict.Answer != container.Yes {
		t.Errorf("focused = %+v right after a focus that succeeded, want yes", verdict)
	}
}

// TestFocusMovesTheTerminalItWasAskedFrom: another terminal already shows the
// agent, and the focus is asked from a terminal that does not — from the
// picker in a popup, then from a pane. The terminal asked from goes, both
// times, and the other is left where it was.
func TestFocusMovesTheTerminalItWasAskedFrom(t *testing.T) {
	tmux := realTmux(t)
	socket := aServer(t, tmux, "an-agent")
	pane := paneOf(t, tmux, socket, "an-agent")
	must(t, tmux, socket, "new-session", "-d", "-s", "an-here", "sleep", "600")
	here := paneOf(t, tmux, socket, "an-here")
	index := strings.TrimPrefix(must(t, tmux, socket, "display-message", "-p", "-t", "an-here", "#{session_id}"), "$")
	other := strings.TrimPrefix(must(t, tmux, socket, "display-message", "-p", "-t", "an-agent", "#{session_id}"), "$")
	watching := viewer(t, tmux, socket, "an-agent")
	asking := viewer(t, tmux, socket, "an-here")
	server := strconv.Itoa(pidOf(t, tmux, socket))
	coordinates := coordinatesOf(t, tmux, socket, pane)

	focusFrom := func(how, tmuxValue, paneValue string) {
		t.Helper()
		t.Setenv(serverVariable, tmuxValue)
		t.Setenv(paneVariable, paneValue)
		outcome, err := Focus(tmux, coordinates)
		if err != nil || !outcome.OK {
			t.Fatalf("from %s: Focus = %+v, %v", how, outcome, err)
		}
		if shown := asking.shows(t, tmux, socket); shown != pane {
			t.Errorf("from %s: the terminal asked from shows %s, not %s", how, shown, pane)
		}
		if shown := watching.shows(t, tmux, socket); shown != pane {
			t.Errorf("from %s: the other terminal was moved, to %s", how, shown)
		}
		must(t, tmux, socket, "switch-client", "-c", asking.Name, "-t", "an-here")
	}

	focusFrom("a popup", socket+","+server+","+index, "")
	// The pane decides over the session `$TMUX` names, which is where a pane
	// was started rather than where it is.
	focusFrom("a pane", socket+","+server+","+other, here)
}

// TestFocusFromOutsideMovesTheTerminalWithTheFocus: two terminals, neither on
// the agent; the one used last has lost the focus to another application, so
// the other one is where the person is.
func TestFocusFromOutsideMovesTheTerminalWithTheFocus(t *testing.T) {
	tmux := realTmux(t)
	socket := aServer(t, tmux, "an-agent")
	pane := paneOf(t, tmux, socket, "an-agent")
	must(t, tmux, socket, "set-option", "-s", "focus-events", "on")
	must(t, tmux, socket, "new-session", "-d", "-s", "an-elsewhere", "sleep", "600")
	front := viewer(t, tmux, socket, "an-elsewhere")
	back := viewer(t, tmux, socket, "an-elsewhere")
	back.lose(t, tmux, socket)
	outside(t)

	outcome, err := Focus(tmux, coordinatesOf(t, tmux, socket, pane))
	if err != nil || !outcome.OK {
		t.Fatalf("Focus = %+v, %v", outcome, err)
	}
	if shown := front.shows(t, tmux, socket); shown != pane {
		t.Errorf("the terminal with the focus shows %s, not %s", shown, pane)
	}
	if shown := back.shows(t, tmux, socket); shown == pane {
		t.Error("the terminal without the focus was moved")
	}
}

// TestFocusedNeedsTheTerminalToHaveTheFocus: a pane on screen in a terminal
// that is behind another application is not in front of anybody.
func TestFocusedNeedsTheTerminalToHaveTheFocus(t *testing.T) {
	tmux := realTmux(t)
	socket := aServer(t, tmux, "an-agent")
	pane := paneOf(t, tmux, socket, "an-agent")
	must(t, tmux, socket, "set-option", "-s", "focus-events", "on")
	terminal := viewer(t, tmux, socket, "an-agent")
	coordinates := coordinatesOf(t, tmux, socket, pane)

	for _, step := range []struct {
		do   func(*testing.T, Tmux, string)
		want container.Answer
	}{
		{nil, container.Yes},
		{terminal.lose, container.No},
		{terminal.gain, container.Yes},
	} {
		if step.do != nil {
			step.do(t, tmux, socket)
		}
		if verdict, _ := Focused(tmux, coordinates); verdict.Answer != step.want {
			t.Errorf("focused = %+v, want %s", verdict, step.want)
		}
	}
}

// TestFocusWithNobodyAttached: a server with no terminal has nothing to bring,
// and the answer says how to get one.
func TestFocusWithNobodyAttached(t *testing.T) {
	tmux := realTmux(t)
	socket := aServer(t, tmux, "an-agent")
	coordinates := coordinatesOf(t, tmux, socket, paneOf(t, tmux, socket, "an-agent"))
	outside(t)

	outcome, err := Focus(tmux, coordinates)
	if err != nil {
		t.Fatalf("Focus: %v", err)
	}
	if outcome.OK || outcome.Problem != NotAttached || !strings.Contains(outcome.Detail, "attach") {
		t.Errorf("outcome = %+v, want not-attached saying what to do next", outcome)
	}
	if verdict, _ := Focused(tmux, coordinates); verdict.Answer != container.No {
		t.Errorf("focused = %+v with nobody attached, want no", verdict)
	}
}

// TestWhatIsGoneIsNamed: "the pane is gone" should offer to prune the record
// and "tmux is not running" should not, which is the whole reason these are
// two words and not one message (§A11.3).
func TestWhatIsGoneIsNamed(t *testing.T) {
	tmux := realTmux(t)
	socket := aServer(t, tmux, "an-agent")
	coordinates := coordinatesOf(t, tmux, socket, "%9999")
	if outcome, _ := Focus(tmux, coordinates); outcome.OK || outcome.Problem != container.PlaceIsGone {
		t.Errorf("a pane that is not there: %+v, want place-is-gone", outcome)
	}

	must(t, tmux, socket, "kill-server")
	if outcome, _ := Focus(tmux, coordinates); outcome.OK || outcome.Problem != container.NotRunning {
		t.Errorf("a server that is not there: %+v, want not-running", outcome)
	}
	if verdict, _ := Focused(tmux, coordinates); verdict.Answer != container.CannotTell {
		t.Errorf("focused on a server that is not there = %+v, want cannot-tell", verdict)
	}
	if outcome, _ := Focus(tmux, json.RawMessage(`{"socket":"`+socket+`","pane":"%1"}`)); outcome.Problem != container.NeverPlaced {
		t.Errorf("coordinates that are not ours: %+v, want never-placed", outcome)
	}
}
