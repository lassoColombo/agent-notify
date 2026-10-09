package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lassoColombo/agent-notify/container"
	"github.com/lassoColombo/agent-notify/session"
)

// The two setups every test here is one of.
//
// bare: an agent in a terminal window, with the process chain reaching the
// terminal, which is the only case where the answer is exact.
// nested: the same agent inside zellij, where the chain ends at a server that
// owns no windows and the title is the only way in.
var (
	ghostty = Window{ID: 34, App: 3434, Name: "Ghostty", Title: "home |  agent-notify"}
	other   = Window{ID: 88, App: 3434, Name: "Ghostty", Title: "work |  notes"}
	firefox = Window{ID: 107, App: 3747, Name: "Firefox", Title: "LENNY — Grafana"}
)

func bareChain() []Ancestor {
	return []Ancestor{
		{PID: 91000, Command: "agent-notify-a"},
		{PID: 90000, Command: "agent-notify-c"},
		{PID: 89000, Command: "claude"},
		{PID: 3519, Command: "login"},
		{PID: 3434, Command: "ghostty"},
	}
}

func nestedChain() []Ancestor {
	return []Ancestor{
		{PID: 91000, Command: "agent-notify-a"},
		{PID: 90000, Command: "agent-notify-c"},
		{PID: 89000, Command: "claude"},
		{PID: 3655, Command: "zellij"}, // the SERVER, whose parent is 1
	}
}

// TestTheChainFindsTheTerminal is the no-multiplexer case, and the only one
// where the answer is exact rather than looked up.
func TestTheChainFindsTheTerminal(t *testing.T) {
	placement := Resolve([]Window{firefox, ghostty}, Coordinates{Chain: bareChain()})
	if placement.Problem != "" {
		t.Fatalf("problem = %q (%s), want the terminal found", placement.Problem, placement.Detail)
	}
	if len(placement.Windows) != 1 || placement.Windows[0].ID != 34 {
		t.Errorf("windows = %+v, want just Ghostty's", placement.Windows)
	}
}

// TestTwoWindowsOfTheSameTerminalCannotBeToldApart is the measurement that
// shapes this whole container: one Ghostty process owns every Ghostty window,
// so a pid is the application and never the window.
//
// Going to one of them at random is wrong half the time and looks exactly like
// success, which §A11.3 says is worse than not going.
func TestTwoWindowsOfTheSameTerminalCannotBeToldApart(t *testing.T) {
	placement := Resolve([]Window{ghostty, other, firefox}, Coordinates{Chain: bareChain()})
	if placement.Problem != container.Ambiguous {
		t.Fatalf("problem = %q, want ambiguous", placement.Problem)
	}
	if !strings.Contains(placement.Detail, "2 Ghostty windows") {
		t.Errorf("detail = %q, want it to say how many and whose", placement.Detail)
	}
}

// TestTheTitleTellsThemApart: the same two windows, and a session name that
// only one of them is showing.
func TestTheTitleTellsThemApart(t *testing.T) {
	placement := Resolve([]Window{ghostty, other, firefox},
		Coordinates{Chain: bareChain(), Title: "home | "})
	if placement.Problem != "" {
		t.Fatalf("problem = %q (%s), want one window", placement.Problem, placement.Detail)
	}
	if len(placement.Windows) != 1 || placement.Windows[0].ID != 34 {
		t.Errorf("windows = %+v, want the one titled for this session", placement.Windows)
	}
}

// TestInsideAMultiplexerTheTitleIsTheOnlyWayIn is the case the whole design
// bends around: the chain dead-ends at a server that owns no windows, and
// nothing in it names a window.
func TestInsideAMultiplexerTheTitleIsTheOnlyWayIn(t *testing.T) {
	placement := Resolve([]Window{firefox, ghostty, other},
		Coordinates{Chain: nestedChain(), Title: "home | "})
	if placement.Problem != "" {
		t.Fatalf("problem = %q (%s), want the terminal found", placement.Problem, placement.Detail)
	}
	if len(placement.Windows) != 1 || placement.Windows[0].ID != 34 {
		t.Errorf("windows = %+v, want the window titled for the zellij session", placement.Windows)
	}
}

// TestNothingIsShowingIt: a detached multiplexer session. The place really is
// gone, and saying so stops the walk before zellij focuses a pane in a window
// nobody can see — which is the exact failure this milestone exists to fix.
func TestNothingIsShowingIt(t *testing.T) {
	placement := Resolve([]Window{firefox}, Coordinates{Chain: nestedChain(), Title: "home | "})
	if placement.Problem != container.PlaceIsGone {
		t.Fatalf("problem = %q (%s), want place-is-gone", placement.Problem, placement.Detail)
	}
}

// TestATitleKeyCanOnlyMatchATitle is the limitation this container carries
// knowingly, written down as a test so that nobody discovers it as a surprise.
//
// Inside a multiplexer there is no process chain to narrow by, so a window
// belonging to something else entirely — a browser on a page whose name starts
// the same way — matches the key and is believed. What makes it survivable is
// that the key carries zellij's own separator: `home | ` is a great deal more
// specific than `home`, and the alternative is refusing to focus anything at
// all for every session inside a multiplexer.
func TestATitleKeyCanOnlyMatchATitle(t *testing.T) {
	impostor := Window{ID: 700, App: 3747, Name: "Firefox", Title: "home |  the wrong thing"}
	placement := Resolve([]Window{impostor}, Coordinates{Chain: nestedChain(), Title: "home | "})
	if placement.Problem != "" || placement.Windows[0].ID != 700 {
		t.Errorf("placement = %+v, want the impostor matched — this is the known cost", placement)
	}
}

// TestAChainThatOwnsNothingAndNoTitleIsNotAFailure: an agent somewhere this
// container cannot see — a launchd job, a session started before it was
// installed. It has nothing to do, which is not the same as failing (D-45).
func TestAChainThatOwnsNothingAndNoTitleIsNotAFailure(t *testing.T) {
	placement := Resolve([]Window{firefox}, Coordinates{Chain: nestedChain()})
	if placement.Problem != container.NeverPlaced {
		t.Fatalf("problem = %q (%s), want never-placed", placement.Problem, placement.Detail)
	}
}

// TestSomebodyElsesCoordinatesAreNotOurs. zellij's section decodes cleanly into
// this struct and means nothing in it; a container that acted on it would focus
// a window chosen by a pane id.
func TestSomebodyElsesCoordinatesAreNotOurs(t *testing.T) {
	var theirs Coordinates
	if err := json.Unmarshal([]byte(`{"session":"home","pane":17,"tab":1}`), &theirs); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if theirs.ours() {
		t.Fatalf("zellij's coordinates decoded as ours: %+v", theirs)
	}
	placement := Resolve([]Window{ghostty}, theirs)
	if placement.Problem != container.NeverPlaced {
		t.Errorf("problem = %q, want never-placed", placement.Problem)
	}
}

// TestTheNearestOwnerWins is core's own rule for nested agents, applied to
// nested windows: the innermost match is the one you are actually inside.
func TestTheNearestOwnerWins(t *testing.T) {
	editor := Window{ID: 9, App: 5000, Name: "Code", Title: "project"}
	chain := []Ancestor{
		{PID: 89000, Command: "claude"},
		{PID: 3434, Command: "ghostty"}, // the terminal, nearer
		{PID: 5000, Command: "Code"},    // whatever launched it, further out
	}
	placement := Resolve([]Window{editor, ghostty}, Coordinates{Chain: chain})
	if len(placement.Windows) != 1 || placement.Windows[0].ID != 34 {
		t.Errorf("windows = %+v, want the nearest owner's", placement.Windows)
	}
}

// TestWhenTheKeysDisagreeTheTitleWins: a terminal that was restarted hands its
// pid to somebody else, while the title still names the session.
func TestWhenTheKeysDisagreeTheTitleWins(t *testing.T) {
	recycled := Window{ID: 500, App: 3434, Name: "Preview", Title: "holiday.pdf"}
	placement := Resolve([]Window{recycled, ghostty},
		Coordinates{Chain: bareChain(), Title: "home | "})
	if placement.Problem != "" {
		t.Fatalf("problem = %q (%s), want the title believed", placement.Problem, placement.Detail)
	}
	if len(placement.Windows) != 1 || placement.Windows[0].ID != 34 {
		t.Errorf("windows = %+v, want the window the title names", placement.Windows)
	}
}

// TestTwoClientsOnOneSessionAreBothRight: attach the same zellij session twice
// and both windows show it, so either is an arrival rather than a coin toss.
func TestTwoClientsOnOneSessionAreBothRight(t *testing.T) {
	second := Window{ID: 200, App: 3434, Name: "Ghostty", Title: "home |  agent-notify"}
	placement := Resolve([]Window{ghostty, second}, Coordinates{Chain: nestedChain(), Title: "home | "})
	if placement.Problem != "" {
		t.Fatalf("problem = %q, want both accepted", placement.Problem)
	}
	if len(placement.Windows) != 2 {
		t.Errorf("windows = %+v, want both", placement.Windows)
	}
}

// ── capture and interpret ────────────────────────────────────────────────────

// TestCaptureReadsOnlyWhatTheTemplateNames is R11: nothing leaves an agent's
// environment which the configuration did not name.
func TestCaptureReadsOnlyWhatTheTemplateNames(t *testing.T) {
	t.Setenv("ZELLIJ_SESSION_NAME", "home")
	t.Setenv("ANTHROPIC_API_KEY", "sk-not-this-one")

	captured, err := Capture("{ZELLIJ_SESSION_NAME} | ")
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	encoded, err := json.Marshal(captured)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(encoded), "sk-not-this-one") {
		t.Fatalf("the capture carries something nobody named: %s", encoded)
	}
	if !strings.Contains(string(encoded), `"home"`) {
		t.Errorf("the capture does not carry what the template named: %s", encoded)
	}
}

// TestInterpretBuildsTheKey: the title from what this program captured, the
// chain from what core walked (D-93), with a start time core keeps and a
// coordinate does not.
func TestInterpretBuildsTheKey(t *testing.T) {
	captured := []byte(`{"values":{"ZELLIJ_SESSION_NAME":"home"}}`)
	walked := []session.Ancestor{{PID: 3655, Command: "zellij", StartedAt: time.Unix(1, 0)}}
	answer, err := Interpret("{ZELLIJ_SESSION_NAME} | ", captured, walked)
	if err != nil {
		t.Fatalf("Interpret: %v", err)
	}
	coordinates, ok := answer.(Coordinates)
	if !ok {
		t.Fatalf("Interpret returned %T, want Coordinates", answer)
	}
	if coordinates.Title != "home | " {
		t.Errorf("title = %q, want %q", coordinates.Title, "home | ")
	}
	if want := []Ancestor{{PID: 3655, Command: "zellij"}}; !reflect.DeepEqual(coordinates.Chain, want) {
		t.Errorf("chain = %+v, want %+v: the one core walked, pid and name only", coordinates.Chain, want)
	}
}

// TestAHalfBuiltKeyIsNoKey: a template with a hole in it matches windows that
// have nothing to do with this session.
func TestAHalfBuiltKeyIsNoKey(t *testing.T) {
	answer, err := Interpret("{ZELLIJ_SESSION_NAME} | ", []byte(`{}`),
		[]session.Ancestor{{PID: 3434, Command: "ghostty"}})
	if err != nil {
		t.Fatalf("Interpret: %v", err)
	}
	coordinates := answer.(Coordinates)
	if coordinates.Title != "" {
		t.Errorf("title = %q, want none: nothing filled the placeholder", coordinates.Title)
	}
}

// TestNothingToPlaceAnswersNull is D-45's other half: core reads null as "this
// layer has nothing to do for this session" and steps past it, so the pane
// inside still gets focused.
func TestNothingToPlaceAnswersNull(t *testing.T) {
	answer, err := Interpret("{ZELLIJ_SESSION_NAME} | ", []byte(`{}`), nil)
	if err != nil {
		t.Fatalf("Interpret: %v", err)
	}
	encoded, err := json.Marshal(answer)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(encoded) != "null" {
		t.Errorf("Interpret answered %s, want null", encoded)
	}
}

func TestATemplateThatIsOffCapturesNothing(t *testing.T) {
	t.Setenv("ZELLIJ_SESSION_NAME", "home")
	captured, _ := Capture("")
	if captured.(Captured).Values != nil {
		t.Errorf("values = %+v, want none: the template names nothing", captured.(Captured).Values)
	}
	if _, complete := substitute("", map[string]string{"ZELLIJ_SESSION_NAME": "home"}); complete {
		t.Error("an empty template built a key, want none")
	}
}

func TestPlaceholders(t *testing.T) {
	for template, want := range map[string]string{
		"{ZELLIJ_SESSION_NAME} | ": "ZELLIJ_SESSION_NAME",
		"{A}-{B}":                  "A,B",
		"no placeholders":          "",
		"{unclosed":                "",
		"{}":                       "",
	} {
		if got := strings.Join(placeholders(template), ","); got != want {
			t.Errorf("placeholders(%q) = %q, want %q", template, got, want)
		}
	}
}
