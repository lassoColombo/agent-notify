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

// TestTheFocusRule is the only genuinely surprising thing about zellij's model,
// pinned as a table so that it can be reasoned about without a terminal
// somebody has to be looking at.
//
// `is_focused` is per TAB — every tab has one — and a tab has TWO focused panes
// at once, the focused tiled one and the focused floating one. Which is in front
// depends on whether that tab is showing its floating panes. All three facts
// were read off a real attached session.
func TestTheFocusRule(t *testing.T) {
	tiled := Pane{ID: 5, Focused: true, TabID: 2}
	floating := Pane{ID: 48, Focused: true, Floating: true, TabID: 2}
	elsewhere := Pane{ID: 17, Focused: true, TabID: 1}
	panes := []Pane{tiled, floating, elsewhere, {ID: 6, TabID: 2}}

	active := func(floatingVisible bool) []Tab {
		return []Tab{
			{ID: 1, Name: "agent-notify", Active: false},
			{ID: 2, Name: "lenny", Active: true, FloatingVisible: floatingVisible},
		}
	}

	for _, want := range []struct {
		pane   int
		tabs   []Tab
		answer container.Answer
		why    string
	}{
		{5, active(false), container.Yes,
			"the focused tiled pane of the active tab, with nothing floating over it"},
		{48, active(false), container.No,
			"focused among the floating panes, but they are hidden"},
		{48, active(true), container.Yes,
			"the same pane once the floating panes are shown"},
		{5, active(true), container.No,
			"the tiled pane is now covered by the floating ones"},
		{17, active(false), container.No, "focused in its own tab, but that tab is not the active one"},
		{6, active(false), container.No, "in the active tab and simply not focused"},
		{99, active(false), container.No, "not there at all, so you are certainly not looking at it"},
	} {
		got := Verdict(panes, want.tabs, want.pane)
		if got.Answer != want.answer {
			t.Errorf("%s: pane %d answered %q (%s), want %q",
				want.why, want.pane, got.Answer, got.Detail, want.answer)
		}
	}
}

// TestATabNobodyReportedIsNotAGuess: everything this cannot establish is
// cannot-tell rather than no, because the caller's rule for the third answer is
// to notify anyway, and a wrong `no` silences a notification that should have
// fired (R27).
func TestATabNobodyReportedIsNotAGuess(t *testing.T) {
	panes := []Pane{{ID: 5, Focused: true, TabID: 7}}
	got := Verdict(panes, []Tab{{ID: 1, Active: true}}, 5)
	if got.Answer != container.CannotTell {
		t.Errorf("answered %q, want cannot-tell", got.Answer)
	}
}

func TestCaptureReadsTheEnvironmentAndNothingElse(t *testing.T) {
	t.Setenv(sessionVariable, "home")
	t.Setenv(paneVariable, "17")

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
	if back[sessionVariable] != "home" || back[paneVariable] != "17" {
		t.Errorf("captured %v", back)
	}
	// Nothing else may travel. An agent's environment holds API keys, and what
	// reaches the disk is bounded by what this returned (§A15, R11).
	if len(back) != 2 {
		t.Errorf("captured %d values, want exactly the two that were asked for: %v", len(back), back)
	}
}

func TestASessionOutsideZellijCapturesNothingUseful(t *testing.T) {
	// Emptied through t.Setenv rather than unset outright, because an
	// os.Unsetenv here is not undone when this test ends: it left every test
	// after it believing it was running outside zellij, which is the one
	// condition the switch in [Focus] turns on. The value this program reads is
	// os.Getenv either way, and both spellings answer "".
	t.Setenv(sessionVariable, "")
	t.Setenv(paneVariable, "")

	captured, err := Capture()
	if err != nil {
		t.Fatalf("Capture outside zellij should not be an error: %v", err)
	}
	encoded, _ := json.Marshal(captured)

	// And interpreting it says so plainly rather than inventing a place.
	if _, err := Interpret(Zellij{Binary: "/nonexistent", Timeout: time.Second}, encoded); err == nil {
		t.Error("interpreting an empty capture produced coordinates")
	}
}

func TestCoordinatesThatAreNotOursAreRefusedNotGuessed(t *testing.T) {
	outcome, err := Focus(Zellij{Binary: "/nonexistent", Timeout: time.Second}, json.RawMessage(`{"window":7}`))
	if err != nil {
		t.Fatalf("Focus: %v", err)
	}
	if outcome.OK || outcome.Problem != container.NeverPlaced {
		t.Errorf("outcome = %+v, want never-placed", outcome)
	}
}

// TestFocusIsIdempotent pins the fact that made it necessary: zellij REFUSES to
// focus a pane that is already focused, exiting 2 with "Pane Terminal(60) is
// already focused". A focus that had nothing left to do would otherwise be
// reported as a failure, having in fact succeeded — and anything a person can
// click twice has to survive being clicked twice.
//
// The stand-in zellij answers the two questions and exits 2 on any action, so
// the test fails if Focus acts at all.
func TestFocusIsIdempotent(t *testing.T) {
	script := `#!/bin/sh
case "$4" in
  list-panes) echo '[{"id":60,"is_plugin":false,"is_focused":true,"is_floating":false,"tab_id":3,"tab_name":"work"}]' ;;
  list-tabs)  echo '[{"tab_id":3,"name":"work","active":true,"are_floating_panes_visible":false}]' ;;
  *) echo "Pane Terminal(60) is already focused" >&2; exit 2 ;;
esac
`
	path := filepath.Join(t.TempDir(), "zellij")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	zellij := Zellij{Binary: path, Timeout: 5 * time.Second}

	coordinates, _ := json.Marshal(Coordinates{Session: "home", Pane: 60, Tab: 3})
	outcome, err := Focus(zellij, coordinates)
	if err != nil {
		t.Fatalf("Focus: %v", err)
	}
	if !outcome.OK {
		t.Errorf("outcome = %+v — focusing a pane that is already in front must be a no-op, "+
			"not a failure", outcome)
	}
}

// TestFocusActsWhenYouAreElsewhere is the other half: the same fixture with the
// tab inactive must actually issue the commands.
//
// The stand-in answers differently once it has been asked to do something,
// because that is what the real one does and Focus now reads the answer back:
// before the actions the pane is not in front, after them it is.
func TestFocusActsWhenYouAreElsewhere(t *testing.T) {
	ran := filepath.Join(t.TempDir(), "ran")
	script := `#!/bin/sh
focused=false
if [ -f ` + ran + ` ]; then focused=true; fi
case "$4" in
  list-panes) echo '[{"id":60,"is_plugin":false,"is_focused":'$focused',"is_floating":false,"tab_id":3,"tab_name":"work"}]' ;;
  list-tabs)  echo '[{"tab_id":3,"name":"work","active":'$focused',"are_floating_panes_visible":false}]' ;;
  list-clients) printf 'CLIENT_ID ZELLIJ_PANE_ID RUNNING_COMMAND\n1         terminal_9     nu\n' ;;
  *) echo "$4 $5" >> ` + ran + ` ;;
esac
`
	path := filepath.Join(t.TempDir(), "zellij")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	coordinates, _ := json.Marshal(Coordinates{Session: "home", Pane: 60, Tab: 3})
	outcome, err := Focus(Zellij{Binary: path, Timeout: 5 * time.Second}, coordinates)
	if err != nil || !outcome.OK {
		t.Fatalf("Focus: %v, %+v", err, outcome)
	}

	done, err := os.ReadFile(ran)
	if err != nil {
		t.Fatalf("nothing was run: %v", err)
	}
	for _, want := range []string{"go-to-tab-by-id 3", "focus-pane-id terminal_60"} {
		if !strings.Contains(string(done), want) {
			t.Errorf("did not run %q; it ran:\n%s", want, done)
		}
	}
}

// TestAFocusThatChangedNothingIsNotASuccess pins why the answer is read back at
// all: zellij exits 0 whether or not an action did anything, so a stand-in that
// accepts every command and moves nothing looks exactly like one that worked.
//
// This is the shape the old code got wrong. It is not hypothetical — a session
// whose only client has gone is precisely a server that takes the commands and
// changes no screen — and it is the difference between "X is in front" and a
// person still looking at something else.
func TestAFocusThatChangedNothingIsNotASuccess(t *testing.T) {
	script := `#!/bin/sh
case "$4" in
  list-panes) echo '[{"id":60,"is_plugin":false,"is_focused":false,"is_floating":false,"tab_id":3,"tab_name":"work"}]' ;;
  list-tabs)  echo '[{"tab_id":3,"name":"work","active":false,"are_floating_panes_visible":false}]' ;;
  list-clients) printf 'CLIENT_ID ZELLIJ_PANE_ID RUNNING_COMMAND\n1         terminal_9     nu\n' ;;
  *) exit 0 ;;
esac
`
	path := filepath.Join(t.TempDir(), "zellij")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	coordinates, _ := json.Marshal(Coordinates{Session: "home", Pane: 60, Tab: 3})
	outcome, err := Focus(Zellij{Binary: path, Timeout: 5 * time.Second}, coordinates)
	if err != nil {
		t.Fatalf("Focus: %v", err)
	}
	if outcome.OK || outcome.Problem != container.Refused {
		t.Errorf("outcome = %+v, want refused", outcome)
	}
}

// TestFocusWithNobodyAttachedIsItsOwnWord: the pane is there, the session is
// running, and there is no screen to put anything in front of. That is neither
// "gone" nor "refused", and the next move — `zellij attach` — belongs to no
// other failure (§A11.3).
//
// The empty ZELLIJ_SESSION_NAME is which caller this is: the menu bar, a
// notification, anything under launchd. Asked from a pane the answer is a
// different one, which is the test below this.
func TestFocusWithNobodyAttachedIsItsOwnWord(t *testing.T) {
	t.Setenv(sessionVariable, "")
	script := `#!/bin/sh
case "$4" in
  list-panes) echo '[{"id":60,"is_plugin":false,"is_focused":false,"is_floating":false,"tab_id":3,"tab_name":"work"}]' ;;
  list-tabs)  echo '[{"tab_id":3,"name":"work","active":false,"are_floating_panes_visible":false}]' ;;
  list-clients) printf 'CLIENT_ID ZELLIJ_PANE_ID RUNNING_COMMAND\n' ;;
  *) echo "acted" >&2; exit 0 ;;
esac
`
	path := filepath.Join(t.TempDir(), "zellij")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	coordinates, _ := json.Marshal(Coordinates{Session: "home", Pane: 60, Tab: 3})
	outcome, err := Focus(Zellij{Binary: path, Timeout: 5 * time.Second}, coordinates)
	if err != nil {
		t.Fatalf("Focus: %v", err)
	}
	if outcome.OK || outcome.Problem != NotAttached {
		t.Errorf("outcome = %+v, want not-attached", outcome)
	}
	if !strings.Contains(outcome.Detail, "home") {
		t.Errorf("detail = %q — it should name the session to attach to", outcome.Detail)
	}
}

// TestAFocusAskedFromAPaneBringsTheTerminal is the same session, nobody
// attached, asked from somewhere a terminal could come from — and it is the
// whole of the branch, including the exact command that does it.
//
// The stand-in zellij is a session that starts empty and has somebody in it
// from the moment it is asked to switch, which is what a real one does about a
// tenth of a second after answering. Everything about this is checkable here
// except the part zellij keeps to itself — whether it will move a terminal for
// this particular caller — and that is measured by hand, against a real one.
//
// What the recorded arguments are for: `--session` is the difference between
// this working and this being a silent no-op, and it is an easy thing to add
// back for consistency with every other command here. A test that only checked
// the outcome would pass with it, because the stand-in answers the same way
// either way.
func TestAFocusAskedFromAPaneBringsTheTerminal(t *testing.T) {
	t.Setenv(sessionVariable, "somewhere-else")
	path, asked := zellijThatSwitches(t, true)

	coordinates, _ := json.Marshal(Coordinates{Session: "home", Pane: 60, Tab: 3})
	outcome, err := Focus(Zellij{Binary: path, Timeout: 5 * time.Second}, coordinates)
	if err != nil {
		t.Fatalf("Focus: %v", err)
	}
	if !outcome.OK {
		t.Fatalf("outcome = %+v — the terminal came and this said otherwise", outcome)
	}

	recorded, err := os.ReadFile(asked)
	if err != nil {
		t.Fatalf("nothing recorded what zellij was asked: %v", err)
	}
	var switching string
	for _, line := range strings.Split(strings.TrimSpace(string(recorded)), "\n") {
		if strings.Contains(line, "switch-session") {
			switching = line
		}
	}
	switch {
	case switching == "":
		t.Fatalf("nobody was ever asked to switch: %s", recorded)
	case !strings.Contains(switching, "switch-session home --pane-id terminal_60"):
		t.Errorf("asked %q, want the session and the pane it is to land on", switching)
	case strings.Contains(switching, "--session"):
		t.Errorf("asked %q — with --session this program is its own throwaway client "+
			"and the terminal stays exactly where it is", switching)
	}
}

// TestASwitchNobodyCameFromIsNotASuccess is the same branch against the zellij
// that takes the command and moves nobody, which is what every caller but a
// terminal gets and what this program cannot tell in advance.
//
// It is [TestAFocusThatChangedNothingIsNotASuccess] for the other path, and the
// deadline is the whole of the difference: the wait after a switch exists
// because the terminal arrives late, so a wait with no end would hang here
// rather than answer.
func TestASwitchNobodyCameFromIsNotASuccess(t *testing.T) {
	t.Setenv(sessionVariable, "somewhere-else")
	path, _ := zellijThatSwitches(t, false)

	coordinates, _ := json.Marshal(Coordinates{Session: "home", Pane: 60, Tab: 3})
	outcome, err := Focus(Zellij{Binary: path, Timeout: 5 * time.Second}, coordinates)
	if err != nil {
		t.Fatalf("Focus: %v", err)
	}
	if outcome.OK || outcome.Problem != container.Refused {
		t.Errorf("outcome = %+v, want refused", outcome)
	}
}

// zellijThatSwitches is a stand-in zellij for the two tests above: a session
// nobody is in, which after a `switch-session` either has somebody in it or
// still has not, and a file recording everything it was asked.
//
// `arrives` is the fact a real zellij keeps to itself. It says yes about a tenth
// of a second after answering the switch, and it says no for every caller it
// will not move a terminal for — identically, and with the same exit 0.
func zellijThatSwitches(t *testing.T, arrives bool) (binary, asked string) {
	t.Helper()
	directory := t.TempDir()
	asked = filepath.Join(directory, "asked")
	came := filepath.Join(directory, "came")

	switched := ": > " + came
	if !arrives {
		switched = ":"
	}
	script := `#!/bin/sh
echo "$@" >> ` + asked + `
for argument in "$@"; do
  if [ "$argument" = "switch-session" ]; then ` + switched + `; exit 0; fi
done
if [ -f ` + came + ` ]; then here=true; else here=false; fi
case "$4" in
  list-panes) echo "[{\"id\":60,\"is_plugin\":false,\"is_focused\":$here,\"is_floating\":false,\"tab_id\":3,\"tab_name\":\"work\"}]" ;;
  list-tabs)  echo "[{\"tab_id\":3,\"name\":\"work\",\"active\":$here,\"are_floating_panes_visible\":false}]" ;;
  list-clients)
    printf 'CLIENT_ID ZELLIJ_PANE_ID RUNNING_COMMAND\n'
    if [ -f ` + came + ` ]; then printf '1         terminal_60    nu\n'; fi ;;
  *) exit 0 ;;
esac
`
	binary = filepath.Join(directory, "zellij")
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return binary, asked
}

// TestFocusLowersAFloatingPaneOverTheTarget is the bug it was written for: you
// were taken to the right pane and a floating one was still sitting on top of
// it, and closing that by hand was the only way through.
//
// `is_focused` is not "in front". A tab has two focused panes, so the focused
// TILED pane is `Focused` and underneath the floating layer at the same time —
// and the old guard, reading `Focused` alone, skipped the focus in exactly that
// case. `focus-pane-id` on a tiled pane is what LOWERS the layer (0.45.1: exit
// 0, floating panes hidden), so the call being skipped was the one that would
// have cleared the cover.
//
// The stand-in shows the floating layer until it is asked to do something, the
// way the real one does.
func TestFocusLowersAFloatingPaneOverTheTarget(t *testing.T) {
	ran := filepath.Join(t.TempDir(), "ran")
	script := `#!/bin/sh
floating=true
if [ -f ` + ran + ` ]; then floating=false; fi
case "$4" in
  list-panes) echo '[{"id":60,"is_plugin":false,"is_focused":true,"is_floating":false,"tab_id":3,"tab_name":"work"},
                     {"id":61,"is_plugin":false,"is_focused":true,"is_floating":true,"tab_id":3,"tab_name":"work"}]' ;;
  list-tabs)  echo '[{"tab_id":3,"name":"work","active":true,"are_floating_panes_visible":'$floating'}]' ;;
  list-clients) printf 'CLIENT_ID ZELLIJ_PANE_ID RUNNING_COMMAND\n1         terminal_61    nu\n' ;;
  *) echo "$4 $5" >> ` + ran + ` ;;
esac
`
	path := filepath.Join(t.TempDir(), "zellij")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	coordinates, _ := json.Marshal(Coordinates{Session: "home", Pane: 60, Tab: 3})
	outcome, err := Focus(Zellij{Binary: path, Timeout: 5 * time.Second}, coordinates)
	if err != nil {
		t.Fatalf("Focus: %v", err)
	}
	if !outcome.OK {
		t.Fatalf("outcome = %+v", outcome)
	}

	done, err := os.ReadFile(ran)
	if err != nil {
		t.Fatalf("nothing was run — the floating pane is still over pane 60: %v", err)
	}
	if !strings.Contains(string(done), "focus-pane-id terminal_60") {
		t.Errorf("did not focus the covered pane; it ran:\n%s", done)
	}
}
