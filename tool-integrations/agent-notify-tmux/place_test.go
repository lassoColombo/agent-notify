package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lassoColombo/agent-notify/container"
)

// TestTheFocusRule pins what "in front" means in tmux, as a table, so that it
// can be reasoned about without a terminal somebody has to be looking at.
//
// A session has one current window, shared by every client on it, and a
// window has one active pane; a linked window is in several sessions and is in
// front if ANY of them shows it. All of it was read off a real server with a
// real client attached [3.8].
func TestTheFocusRule(t *testing.T) {
	front := row("%5", "@2", "", "")
	sameWindow := row("%6", "@2", "", "")
	sameWindow.PaneActive = false
	behind := row("%17", "@1", "", "")
	behind.WindowActive = false
	unwatched := row("%9", "@3", "", "")
	unwatched.SessionID, unwatched.SessionName, unwatched.Attached = "$1", "work", 0
	linkedHidden := row("%20", "@4", "", "")
	linkedHidden.WindowActive = false
	linkedShown := linkedHidden
	linkedShown.SessionID, linkedShown.SessionName, linkedShown.WindowActive = "$2", "other", true
	rows := []Row{front, sameWindow, behind, unwatched, linkedHidden, linkedShown}

	for _, want := range []struct {
		pane   string
		answer container.Answer
		why    string
	}{
		{"%5", container.Yes, "the active pane of the current window of an attached session"},
		{"%6", container.No, "in the current window and not its active pane"},
		{"%17", container.No, "the active pane of a window that is not the current one"},
		{"%9", container.No, "in front of a session nobody is attached to"},
		{"%20", container.Yes, "linked into two sessions and current in one of them"},
		{"%99", container.No, "not there at all, so you are certainly not looking at it"},
	} {
		got := Verdict(rows, want.pane)
		if got.Answer != want.answer {
			t.Errorf("%s: pane %s answered %q (%s), want %q",
				want.why, want.pane, got.Answer, got.Detail, want.answer)
		}
	}
}

func TestASessionOutsideTmuxIsNotInterpreted(t *testing.T) {
	// Emptied through t.Setenv rather than unset outright, because an
	// os.Unsetenv is not undone when the test ends, and $TMUX is the one
	// condition the switch in [Focus] turns on.
	t.Setenv(serverVariable, "")
	t.Setenv(paneVariable, "")

	captured, err := Capture()
	if err != nil {
		t.Fatalf("Capture outside tmux should not be an error: %v", err)
	}
	encoded, _ := json.Marshal(captured)
	if _, err := Interpret(Tmux{Binary: "/nonexistent", Timeout: time.Second}, encoded); err == nil {
		t.Error("interpreting an empty capture produced coordinates")
	}
}

func TestCoordinatesThatAreNotOursAreRefusedNotGuessed(t *testing.T) {
	outcome, err := Focus(Tmux{Binary: "/nonexistent", Timeout: time.Second}, json.RawMessage(`{"window":7}`))
	if err != nil {
		t.Fatalf("Focus: %v", err)
	}
	if outcome.OK || outcome.Problem != container.NeverPlaced {
		t.Errorf("outcome = %+v, want never-placed", outcome)
	}
}

// standIn is a tmux that answers `list-panes` with `before` until it has been
// asked to do anything else, and with `after` from then on, which is what the
// real one does and what lets Focus's read-back be tested. Everything it was
// asked is written to `asked`; every action to `ran`.
type standIn struct {
	binary, asked, ran string
}

func fakeTmux(t *testing.T, before, after []Row, clients []Client, refuse bool) standIn {
	t.Helper()
	directory := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		return path
	}
	lines := func(rows []Row) string {
		var out strings.Builder
		for _, r := range rows {
			out.WriteString(strings.Join([]string{
				r.Pane, r.SessionID, r.SessionName, strconv.Itoa(r.Attached),
				r.WindowID, strconv.Itoa(r.WindowIndex), r.WindowName,
				flag(r.WindowActive), flag(r.PaneActive), r.PaneMark, r.WindowMark,
			}, separator) + "\n")
		}
		return out.String()
	}
	var listed strings.Builder
	for _, c := range clients {
		listed.WriteString(strings.Join([]string{c.Name, c.SessionID, fmt.Sprint(c.Activity)}, separator) + "\n")
	}

	beforePath := write("before", lines(before))
	afterPath := write("after", lines(after))
	clientsPath := write("clients", listed.String())
	s := standIn{asked: filepath.Join(directory, "asked"), ran: filepath.Join(directory, "ran")}
	action := ": >> " + s.ran + "; echo \"$@\" >> " + s.ran
	if refuse {
		action = "echo \"refused\" >&2; exit 2"
	}
	s.binary = write("tmux", `#!/bin/sh
echo "$@" >> `+s.asked+`
case "$4" in
  list-panes) if [ -f `+s.ran+` ]; then cat `+afterPath+`; else cat `+beforePath+`; fi ;;
  list-clients) cat `+clientsPath+` ;;
  *) `+action+` ;;
esac
`)
	if err := os.Chmod(s.binary, 0o700); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	return s
}

func flag(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func (s standIn) tmux() Tmux { return Tmux{Binary: s.binary, Timeout: 5 * time.Second} }

func (s standIn) didRun(t *testing.T) string {
	t.Helper()
	done, err := os.ReadFile(s.ran)
	if err != nil {
		return ""
	}
	return string(done)
}

func focusOn(t *testing.T, tmux Tmux, pane string) container.Outcome {
	t.Helper()
	coordinates, _ := json.Marshal(Coordinates{Socket: home, Pane: pane, Session: "home"})
	outcome, err := Focus(tmux, coordinates)
	if err != nil {
		t.Fatalf("Focus: %v", err)
	}
	return outcome
}

// TestFocusIsIdempotent: anything a person can click twice has to survive being
// clicked twice. The stand-in refuses every action, so this fails if Focus acts
// at all on a pane that is already in front.
func TestFocusIsIdempotent(t *testing.T) {
	inFront := []Row{row("%60", "@3", "", "")}
	s := fakeTmux(t, inFront, inFront, nil, true)

	if outcome := focusOn(t, s.tmux(), "%60"); !outcome.OK {
		t.Errorf("outcome = %+v — focusing a pane that is already in front must be a no-op, "+
			"not a failure", outcome)
	}
}

// TestFocusActsWhenYouAreElsewhere: the window, then the pane, named with the
// session they are to change in, and the answer read back.
func TestFocusActsWhenYouAreElsewhere(t *testing.T) {
	t.Setenv(serverVariable, "")
	behind := row("%60", "@3", "", "")
	behind.WindowActive, behind.PaneActive = false, false
	s := fakeTmux(t, []Row{behind}, []Row{row("%60", "@3", "", "")}, nil, false)

	if outcome := focusOn(t, s.tmux(), "%60"); !outcome.OK {
		t.Fatalf("outcome = %+v", outcome)
	}
	done := s.didRun(t)
	for _, want := range []string{"-S " + home + " select-window -t $0:@3", "-S " + home + " select-pane -t %60"} {
		if !strings.Contains(done, want) {
			t.Errorf("did not run %q; it ran:\n%s", want, done)
		}
	}
}

// TestAFocusThatChangedNothingIsNotASuccess pins why the answer is read back at
// all: a tmux that takes every command and moves nothing looks, by exit code,
// exactly like one that worked.
func TestAFocusThatChangedNothingIsNotASuccess(t *testing.T) {
	t.Setenv(serverVariable, "")
	behind := row("%60", "@3", "", "")
	behind.WindowActive = false
	s := fakeTmux(t, []Row{behind}, []Row{behind}, nil, false)

	if outcome := focusOn(t, s.tmux(), "%60"); outcome.OK || outcome.Problem != container.Refused {
		t.Errorf("outcome = %+v, want refused", outcome)
	}
}

// TestFocusWithNobodyAttachedIsItsOwnWord: the pane is there, the server is
// running, and there is no screen to put anything in front of. Asked from
// outside tmux — a notification, anything under launchd — the next move is
// `tmux attach`, which belongs to no other failure (§A11.3).
func TestFocusWithNobodyAttachedIsItsOwnWord(t *testing.T) {
	t.Setenv(serverVariable, "")
	alone := row("%60", "@3", "", "")
	alone.Attached = 0
	s := fakeTmux(t, []Row{alone}, []Row{alone}, nil, false)

	outcome := focusOn(t, s.tmux(), "%60")
	if outcome.OK || outcome.Problem != NotAttached {
		t.Errorf("outcome = %+v, want not-attached", outcome)
	}
	if !strings.Contains(outcome.Detail, "attach -t home") {
		t.Errorf("detail = %q — it should say what to attach to", outcome.Detail)
	}
	if done := s.didRun(t); done != "" {
		t.Errorf("acted on a session nobody is looking at:\n%s", done)
	}
}

// TestAFocusAskedFromAPopupNamesItsOwnClient is the picker's case: it
// runs in a popup, which has $TMUX and no pane [measured on 3.8], and the
// terminal it is drawn on is the one that should go. The client is named, so
// the one on another session that happens to have been used last stays where
// it is.
func TestAFocusAskedFromAPopupNamesItsOwnClient(t *testing.T) {
	t.Setenv(serverVariable, home+",4242,1")
	alone := row("%60", "@3", "", "")
	alone.Attached = 0
	arrived := row("%60", "@3", "", "")
	s := fakeTmux(t, []Row{alone}, []Row{arrived}, []Client{
		{Name: "/dev/ttys004", SessionID: "$1", Activity: 100},
		{Name: "/dev/ttys009", SessionID: "$2", Activity: 900},
		{Name: "/dev/ttys007", SessionID: "$1", Activity: 200},
	}, false)

	if outcome := focusOn(t, s.tmux(), "%60"); !outcome.OK {
		t.Fatalf("outcome = %+v", outcome)
	}
	if done, want := s.didRun(t), "switch-client -c /dev/ttys007 -t %60"; !strings.Contains(done, want) {
		t.Errorf("did not run %q; it ran:\n%s", want, done)
	}
}

// TestAFocusFromOutsideMovesTheLatestClient is a notification's case: asked
// from outside this server (launchd, or another tmux), with nobody on the
// pane's session but somebody on the server. tmux can move any of its clients
// from anywhere, so the one used last goes there rather than nobody (D-123).
func TestAFocusFromOutsideMovesTheLatestClient(t *testing.T) {
	for _, asker := range []string{"", "/tmp/tmux-501/work,4242,0"} {
		t.Run(fmt.Sprintf("TMUX=%q", asker), func(t *testing.T) {
			t.Setenv(serverVariable, asker)
			alone := row("%60", "@3", "", "")
			alone.Attached = 0
			s := fakeTmux(t, []Row{alone}, []Row{row("%60", "@3", "", "")}, []Client{
				{Name: "/dev/ttys004", SessionID: "$1", Activity: 100},
				{Name: "/dev/ttys009", SessionID: "$2", Activity: 900},
			}, false)

			if outcome := focusOn(t, s.tmux(), "%60"); !outcome.OK {
				t.Fatalf("outcome = %+v", outcome)
			}
			if done, want := s.didRun(t), "switch-client -c /dev/ttys009 -t %60"; !strings.Contains(done, want) {
				t.Errorf("did not run %q; it ran:\n%s", want, done)
			}
		})
	}
}

// TestAFocusFromInsideMovesTheAsker: the picker's terminal is the screen the
// person is looking at, so it goes, even though another terminal already shows
// the pane's session. Moving that other one would report success to somebody
// whose own screen did not change.
func TestAFocusFromInsideMovesTheAsker(t *testing.T) {
	t.Setenv(serverVariable, home+",4242,1")
	behind := row("%60", "@3", "", "")
	behind.WindowActive = false
	s := fakeTmux(t, []Row{behind}, []Row{row("%60", "@3", "", "")}, []Client{
		{Name: "/dev/ttys004", SessionID: "$0", Activity: 900},
		{Name: "/dev/ttys007", SessionID: "$1", Activity: 100},
	}, false)

	if outcome := focusOn(t, s.tmux(), "%60"); !outcome.OK {
		t.Fatalf("outcome = %+v", outcome)
	}
	done := s.didRun(t)
	if want := "switch-client -c /dev/ttys007 -t %60"; !strings.Contains(done, want) {
		t.Errorf("did not run %q; it ran:\n%s", want, done)
	}
	if strings.Contains(done, "select-window") {
		t.Errorf("moved the other terminal's session too:\n%s", done)
	}
}

func TestAPaneThatIsGoneIsNamedAsGone(t *testing.T) {
	s := fakeTmux(t, []Row{row("%1", "@1", "", "")}, nil, nil, false)
	if outcome := focusOn(t, s.tmux(), "%60"); outcome.OK || outcome.Problem != container.PlaceIsGone {
		t.Errorf("outcome = %+v, want place-is-gone", outcome)
	}
}

// TestEveryInvocationNamesItsServer: the session-watcher's own $TMUX, if it has
// one, is whichever pane started it, so a command without `-S` would go to a
// server chosen by accident. And every one says `-u`, without which a tmux
// started with no UTF-8 locale answers in underscores.
func TestEveryInvocationNamesItsServer(t *testing.T) {
	t.Setenv(serverVariable, home+",4242,1")
	alone := row("%60", "@3", "", "")
	alone.Attached = 0
	s := fakeTmux(t, []Row{alone}, []Row{row("%60", "@3", "", "")},
		[]Client{{Name: "/dev/ttys004", SessionID: "$1"}}, false)
	focusOn(t, s.tmux(), "%60")

	asked, err := os.ReadFile(s.asked)
	if err != nil {
		t.Fatalf("nothing was asked: %v", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(asked)), "\n") {
		if !strings.HasPrefix(line, "-u -S "+home+" ") {
			t.Errorf("asked %q without -u and the server", line)
		}
	}
}
