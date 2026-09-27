package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lassoColombo/agent-notify/container"
)

// These run against a real zellij, for the same reason the display's do:
// everything this program knows about zellij is a fact about a program.
//
// They also assert the thing this file used to say no test could — whether a
// focus moved anybody — because a client does not have to be a person. zellij
// is a terminal emulator, so a pane of one session running `zellij attach` to
// another IS a real attached client, reported by `list-clients` like any other.
// That is what `viewer` below builds, and it is what lets both halves of the
// focus rule be asserted rather than described: a session nobody is attached to
// is named `not-attached` instead of reported as focused, and one somebody is
// attached to is read back and shown to be in front.

func realZellij(t *testing.T) Zellij {
	t.Helper()
	// A test runs in a shell, which is the one context where looking zellij up
	// is right — and exactly why the program no longer does it (D-67).
	binary, err := exec.LookPath("zellij")
	if err != nil {
		t.Skipf("no zellij here: %v", err)
	}
	return Zellij{Binary: binary, Timeout: 5 * time.Second}
}

func session(t *testing.T, zellij Zellij) (string, int) {
	return namedSession(t, zellij, "")
}

// namedSession takes a suffix because a test can need two sessions at once:
// one holding the agent, one holding the terminal that is looking at it.
func namedSession(t *testing.T, zellij Zellij, suffix string) (string, int) {
	t.Helper()
	name := fmt.Sprintf("an-ctr-%d%s", os.Getpid(), suffix)

	bare := func(arguments ...string) (string, error) {
		command := exec.Command(zellij.Binary, arguments...)
		command.Env = stripped(os.Environ())
		out, err := command.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	if out, err := bare("attach", "--create-background", name); err != nil {
		t.Skipf("cannot start a detached zellij (%v): %s", err, out)
	}
	t.Cleanup(func() {
		_, _ = bare("kill-session", name)
		_, _ = bare("delete-session", name)
	})

	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := zellij.Panes(name); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the detached session %q never came up", name)
		}
		time.Sleep(50 * time.Millisecond)
	}

	out, err := bare("--session", name, "run", "--name", "agent", "--", "sleep", "600")
	if err != nil {
		t.Fatalf("zellij run: %v: %s", err, out)
	}
	id, found := strings.CutPrefix(out, "terminal_")
	if !found {
		t.Fatalf("zellij run answered %q", out)
	}
	pane, err := strconv.Atoi(id)
	if err != nil {
		t.Fatalf("zellij run answered %q: %v", out, err)
	}
	return name, pane
}

// viewer attaches a real client to `target` and returns when zellij can see it.
//
// It is a pane of a second session running `zellij attach`, which is a client
// in every sense that matters here: `list-clients` reports it, tabs become
// `active`, and a focus has somebody to be performed for. ZELLIJ* are stripped
// from the command because zellij refuses to attach from inside a session, and
// that is the only thing about this that differs from a person opening a
// terminal.
func viewer(t *testing.T, zellij Zellij, target string) {
	t.Helper()
	host, _ := namedSession(t, zellij, "-host")
	bareZellij(t, zellij, "--session", host, "run", "--",
		"env", "-u", "ZELLIJ", "-u", "ZELLIJ_SESSION_NAME", "-u", "ZELLIJ_PANE_ID",
		zellij.Binary, "attach", target)

	deadline := time.Now().Add(15 * time.Second)
	for {
		clients, err := zellij.Clients(target)
		if err == nil && len(clients) > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("nothing ever attached to %q (last answer: %v)", target, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func bareZellij(t *testing.T, zellij Zellij, arguments ...string) string {
	t.Helper()
	command := exec.Command(zellij.Binary, arguments...)
	command.Env = stripped(os.Environ())
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("zellij %s: %v: %s", strings.Join(arguments, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func stripped(environment []string) []string {
	var kept []string
	for _, variable := range environment {
		if strings.HasPrefix(variable, "ZELLIJ") {
			continue
		}
		kept = append(kept, variable)
	}
	return kept
}

// TestInterpretFindsTheTab is the whole of placing: the environment says which
// pane, and only zellij knows which tab holds it.
func TestInterpretFindsTheTab(t *testing.T) {
	zellij := realZellij(t)
	name, pane := session(t, zellij)

	captured, _ := json.Marshal(Captured{Session: name, Pane: strconv.Itoa(pane)})
	got, err := Interpret(zellij, captured)
	if err != nil {
		t.Fatalf("Interpret: %v", err)
	}
	coordinates := got.(Coordinates)
	if coordinates.Session != name || coordinates.Pane != pane {
		t.Errorf("coordinates = %+v, want session %q pane %d", coordinates, name, pane)
	}
	if coordinates.TabName == "" {
		t.Errorf("coordinates = %+v — the tab's name is what makes a record legible", coordinates)
	}

	// And the pane really is in the tab it named.
	panes, err := zellij.Panes(name)
	if err != nil {
		t.Fatalf("Panes: %v", err)
	}
	for _, one := range panes {
		if !one.Plugin && one.ID == pane && one.TabID != coordinates.Tab {
			t.Errorf("pane %d is in tab %d, not %d", pane, one.TabID, coordinates.Tab)
		}
	}
}

// TestAPaneThatIsGoneIsNamedAsGone: taking somebody confidently to the wrong
// place is worse than not going, so a coordinate is validated at the moment of
// use and never trusted from the record (R17, §A11.3).
func TestAPaneThatIsGoneIsNamedAsGone(t *testing.T) {
	zellij := realZellij(t)
	name, _ := session(t, zellij)

	coordinates, _ := json.Marshal(Coordinates{Session: name, Pane: 9999, Tab: 0})
	outcome, err := Focus(zellij, coordinates)
	if err != nil {
		t.Fatalf("Focus: %v", err)
	}
	if outcome.OK || outcome.Problem != container.PlaceIsGone {
		t.Errorf("outcome = %+v, want place-is-gone", outcome)
	}
}

// TestASessionThatIsGoneIsNotAPaneProblem: "the pane is gone" should offer to
// prune the record and "zellij is not running" should not, which is the whole
// reason these are two words and not one message (§A11.3).
func TestASessionThatIsGoneIsNotAPaneProblem(t *testing.T) {
	zellij := realZellij(t)

	coordinates, _ := json.Marshal(Coordinates{Session: "an-no-such", Pane: 1, Tab: 0})
	outcome, err := Focus(zellij, coordinates)
	if err != nil {
		t.Fatalf("Focus: %v", err)
	}
	if outcome.OK || outcome.Problem != container.NotRunning {
		t.Errorf("outcome = %+v, want not-running", outcome)
	}
}

// TestFocusOnASessionNobodyIsLookingAt is the regression, and it used to assert
// the opposite.
//
// The commands are accepted by a live detached server — they exit 0, and the
// stored focus really does move — so "zellij took it" was read as "you are
// there now", and `focus-session` printed "X is in front" at somebody looking
// at a different screen entirely. Nothing is in front of nobody: the answer is
// a word that says which nothing it was (§A11.3).
//
// The empty ZELLIJ_SESSION_NAME is the test saying which caller it is standing
// in for, and it is not decoration. `go test` is usually run FROM a zellij pane,
// and a focus asked from one is now allowed to bring that terminal along — so
// without this line the suite would take the terminal somebody is running it in
// to a throwaway session and leave it there. See the three-way switch in
// [Focus].
func TestFocusOnASessionNobodyIsLookingAt(t *testing.T) {
	zellij := realZellij(t)
	name, pane := session(t, zellij)
	t.Setenv(sessionVariable, "")

	coordinates, _ := json.Marshal(Coordinates{Session: name, Pane: pane})
	outcome, err := Focus(zellij, coordinates)
	if err != nil {
		t.Fatalf("Focus: %v", err)
	}
	if outcome.OK || outcome.Problem != NotAttached {
		t.Errorf("outcome = %+v, want not-attached", outcome)
	}
	if !strings.Contains(outcome.Detail, "zellij attach") {
		t.Errorf("detail = %q — a named failure should say what to do next", outcome.Detail)
	}
}

// TestFocusPutsThePaneInFrontOfAnAttachedClient is the other half: with
// somebody looking, the same coordinates work, and the proof is read back from
// zellij rather than taken from an exit code.
func TestFocusPutsThePaneInFrontOfAnAttachedClient(t *testing.T) {
	zellij := realZellij(t)
	name, pane := session(t, zellij)
	viewer(t, zellij, name)

	// The viewer arrives on whatever the session's focus was, which is the pane
	// `session` just created — so this moves it away first, leaving Focus
	// something to actually do.
	bareZellij(t, zellij, "--session", name, "run", "--", "sleep", "600")

	captured, _ := json.Marshal(Captured{Session: name, Pane: strconv.Itoa(pane)})
	interpreted, err := Interpret(zellij, captured)
	if err != nil {
		t.Fatalf("Interpret: %v", err)
	}
	coordinates, _ := json.Marshal(interpreted)

	outcome, err := Focus(zellij, coordinates)
	if err != nil {
		t.Fatalf("Focus: %v", err)
	}
	if !outcome.OK {
		t.Fatalf("outcome = %+v", outcome)
	}

	// The claim is about a screen, so it is checked against the only thing that
	// knows about screens: the client's own row.
	clients, err := zellij.Clients(name)
	if err != nil {
		t.Fatalf("Clients: %v", err)
	}
	looking := false
	for _, client := range clients {
		if client.Pane == Address(pane) {
			looking = true
		}
	}
	if !looking {
		t.Errorf("focus said yes and no client is on %s: %+v", Address(pane), clients)
	}

	verdict, err := Focused(zellij, coordinates)
	if err != nil {
		t.Fatalf("Focused: %v", err)
	}
	if verdict.Answer != container.Yes {
		t.Errorf("focused = %+v, want yes right after a focus that succeeded", verdict)
	}
}

// TestParseClients needs no zellij: it is the table zellij prints, kept here
// verbatim, including the one that is not a table at all.
//
// `list-clients` has no --json, and asking it about a session that does not
// exist exits 0 and writes the list of sessions that DO where the table should
// be — so the header is the only thing separating "nobody is attached" from
// "that session is not there", and those two must never be confused: the first
// is a `zellij attach` away and the second means the record is stale.
func TestParseClients(t *testing.T) {
	for _, one := range []struct {
		what    string
		output  string
		want    []Client
		refused bool
	}{{
		what:   "nobody attached",
		output: "CLIENT_ID ZELLIJ_PANE_ID RUNNING_COMMAND\n",
		want:   nil,
	}, {
		what: "two clients, and a command with spaces in it",
		output: "CLIENT_ID ZELLIJ_PANE_ID RUNNING_COMMAND\n" +
			"1         terminal_2     nu             \n" +
			"2         terminal_7     bash -c echo hello; sleep 900\n",
		want: []Client{{ID: 1, Pane: "terminal_2"}, {ID: 2, Pane: "terminal_7"}},
	}, {
		what:    "the session list, which is what a missing session answers",
		output:  "\x1b[32;1mhome\x1b[m [Created 4days ago] (current)\n",
		refused: true,
	}, {
		what:    "nothing at all",
		output:  "",
		refused: true,
	}} {
		t.Run(one.what, func(t *testing.T) {
			got, err := ParseClients([]byte(one.output))
			if one.refused {
				if err == nil {
					t.Fatalf("ParseClients read %d clients out of %q", len(got), one.output)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseClients: %v", err)
			}
			if len(got) != len(one.want) {
				t.Fatalf("got %+v, want %+v", got, one.want)
			}
			for i := range got {
				if got[i] != one.want[i] {
					t.Errorf("client %d = %+v, want %+v", i, got[i], one.want[i])
				}
			}
		})
	}
}

// TestFocusedSaysCannotTellRatherThanGuessing: with nobody attached there is no
// focus, and the honest answer is the third one.
func TestFocusedOnADetachedSession(t *testing.T) {
	zellij := realZellij(t)
	name, pane := session(t, zellij)

	coordinates, _ := json.Marshal(Coordinates{Session: name, Pane: pane})
	verdict, err := Focused(zellij, coordinates)
	if err != nil {
		t.Fatalf("Focused: %v", err)
	}
	if verdict.Answer == container.Yes {
		t.Errorf("a session nobody is attached to reported a focused pane: %+v", verdict)
	}
}
