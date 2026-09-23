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

// stand writes a program that answers the way aerospace answers, and records
// what it was asked.
//
// A shell script rather than a Go double because what is under test is the
// other side of a subprocess: the argv that goes out, the JSON that comes back,
// and the exit code. A double would test none of those.
func stand(t *testing.T, body string) (Aerospace, func() string) {
	t.Helper()
	directory := t.TempDir()
	log := filepath.Join(directory, "asked")
	path := filepath.Join(directory, "aerospace")

	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + log + "\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return Aerospace{Binary: path, Timeout: 2 * time.Second}, func() string {
		asked, err := os.ReadFile(log)
		if err != nil {
			return ""
		}
		return string(asked)
	}
}

// windows is the answer `list-windows --all --json --format …` gives, copied
// from the shape aerospace 0.20.3 actually prints.
const windowsJSON = `[
  {"app-name":"Firefox","app-pid":3747,"window-id":107,"window-title":"LENNY — Grafana"},
  {"app-name":"Ghostty","app-pid":3434,"window-id":34,"window-title":"home |  agent-notify"}
]`

const focusedJSON = `[{"app-name":"Ghostty","app-pid":3434,"window-id":34,"window-title":"home |  agent-notify"}]`

func listing(all, focused string) string {
	return `case "$1 $2" in
  "list-windows --all") cat <<'JSON'
` + all + `
JSON
  ;;
  "list-windows --focused") cat <<'JSON'
` + focused + `
JSON
  ;;
  "focus --window-id") exit 0 ;;
  *) echo "aerospace was asked something it does not know: $*" >&2; exit 1 ;;
esac`
}

func TestWindowsCarryThePidAndTheTitle(t *testing.T) {
	aerospace, asked := stand(t, listing(windowsJSON, focusedJSON))

	windows, err := aerospace.Windows()
	if err != nil {
		t.Fatalf("Windows: %v", err)
	}
	if len(windows) != 2 || windows[1].ID != 34 || windows[1].App != 3434 {
		t.Fatalf("windows = %+v, want the two aerospace printed", windows)
	}
	// The format is not decoration: --json on its own answers with no pid at
	// all, and the pid is half of how a window is recognised here.
	if !strings.Contains(asked(), "%{app-pid}") || !strings.Contains(asked(), "--json") {
		t.Errorf("asked %q, want it to name the fields it needs", asked())
	}
}

func TestFocusGoesToTheWindowTheKeysName(t *testing.T) {
	aerospace, asked := stand(t, listing(windowsJSON, focusedJSON))

	coordinates, _ := json.Marshal(Coordinates{Chain: nestedChain(), Title: "home | "})
	outcome, err := Focus(aerospace, coordinates)
	if err != nil {
		t.Fatalf("Focus: %v", err)
	}
	if !outcome.OK {
		t.Fatalf("outcome = %+v, want the window focused", outcome)
	}
	if !strings.Contains(asked(), "focus --window-id 34") {
		t.Errorf("asked %q, want the window the title named", asked())
	}
}

// TestAerospaceThatIsNotThereIsNamed: the window manager not running must be
// its own word, because the answer to it is "start aerospace" and the answer to
// place-is-gone is not.
func TestAerospaceThatIsNotThereIsNamed(t *testing.T) {
	aerospace, _ := stand(t, `echo "Can't connect to AeroSpace server" >&2; exit 1`)

	coordinates, _ := json.Marshal(Coordinates{Title: "home | "})
	outcome, _ := Focus(aerospace, coordinates)
	if outcome.Problem != container.NotRunning {
		t.Fatalf("problem = %q, want not-running", outcome.Problem)
	}
	if !strings.Contains(outcome.Detail, "Can't connect") {
		t.Errorf("detail = %q, want aerospace's own words in it", outcome.Detail)
	}
}

// TestARefusalCarriesAerospacesOwnWords. A window that dies between the listing
// and the focus is the likeliest cause, and aerospace says so itself —
// "Invalid <window-id> 34 passed to --window-id", measured.
func TestARefusalCarriesAerospacesOwnWords(t *testing.T) {
	aerospace, _ := stand(t, strings.Replace(listing(windowsJSON, focusedJSON),
		`"focus --window-id") exit 0 ;;`,
		`"focus --window-id") echo "Invalid <window-id> 34 passed to --window-id" >&2; exit 1 ;;`, 1))

	coordinates, _ := json.Marshal(Coordinates{Title: "home | "})
	outcome, _ := Focus(aerospace, coordinates)
	if outcome.Problem != container.Refused {
		t.Fatalf("problem = %q, want refused", outcome.Problem)
	}
	if !strings.Contains(outcome.Detail, "Invalid <window-id>") {
		t.Errorf("detail = %q, want aerospace's own words in it", outcome.Detail)
	}
}

func TestFocusedSaysYesWhenItIsTheOneInFront(t *testing.T) {
	aerospace, _ := stand(t, listing(windowsJSON, focusedJSON))

	coordinates, _ := json.Marshal(Coordinates{Chain: nestedChain(), Title: "home | "})
	verdict, err := Focused(aerospace, coordinates)
	if err != nil {
		t.Fatalf("Focused: %v", err)
	}
	if verdict.Answer != container.Yes {
		t.Errorf("verdict = %+v, want yes", verdict)
	}
}

func TestFocusedSaysNoWhenYouAreLookingAtSomethingElse(t *testing.T) {
	front := `[{"app-name":"Firefox","app-pid":3747,"window-id":107,"window-title":"LENNY — Grafana"}]`
	aerospace, _ := stand(t, listing(windowsJSON, front))

	coordinates, _ := json.Marshal(Coordinates{Chain: nestedChain(), Title: "home | "})
	verdict, _ := Focused(aerospace, coordinates)
	if verdict.Answer != container.No {
		t.Fatalf("verdict = %+v, want no", verdict)
	}
	if !strings.Contains(verdict.Detail, "Firefox") {
		t.Errorf("detail = %q, want it to say what you are looking at instead", verdict.Detail)
	}
}

// TestFocusedCannotTellBetweenTwoWindowsOfTheSameTerminal is R27's third answer
// earning its place: the window in front is one of the candidates, and which of
// them holds the agent is exactly what cannot be known.
func TestFocusedCannotTellBetweenTwoWindowsOfTheSameTerminal(t *testing.T) {
	two := `[
  {"app-name":"Ghostty","app-pid":3434,"window-id":34,"window-title":"one"},
  {"app-name":"Ghostty","app-pid":3434,"window-id":88,"window-title":"two"}
]`
	front := `[{"app-name":"Ghostty","app-pid":3434,"window-id":34,"window-title":"one"}]`
	aerospace, _ := stand(t, listing(two, front))

	coordinates, _ := json.Marshal(Coordinates{Chain: bareChain()})
	verdict, _ := Focused(aerospace, coordinates)
	if verdict.Answer != container.CannotTell {
		t.Errorf("verdict = %+v, want cannot-tell", verdict)
	}
}

// TestAerospaceThatHangsIsGivenUpOn, and given up on ON TIME. Without
// WaitDelay, exec kills the child and then waits for pipes a grandchild still
// holds — measured elsewhere in this project as a 200ms timeout that took 30
// seconds.
func TestAerospaceThatHangsIsGivenUpOn(t *testing.T) {
	aerospace, _ := stand(t, "sleep 30")
	aerospace.Timeout = 200 * time.Millisecond

	started := time.Now()
	coordinates, _ := json.Marshal(Coordinates{Title: "home | "})
	outcome, _ := Focus(aerospace, coordinates)
	if outcome.Problem != container.NotRunning {
		t.Errorf("problem = %q, want not-running", outcome.Problem)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Errorf("a 200ms timeout took %s", elapsed)
	}
}

func TestSomethingThatIsNotAWindowListIsNotAWindowList(t *testing.T) {
	aerospace, _ := stand(t, `echo 'not json at all'`)
	if _, err := aerospace.Windows(); err == nil {
		t.Fatal("Windows accepted something that is not a window list")
	}
}
