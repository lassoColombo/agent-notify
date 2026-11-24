package containers_test

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lassoColombo/agent-notify/container"
	"github.com/lassoColombo/agent-notify/internal/config"
	"github.com/lassoColombo/agent-notify/internal/containers"
	"github.com/lassoColombo/agent-notify/internal/process"
	"github.com/lassoColombo/agent-notify/session"
)

// fake writes a program that answers whatever this test needs it to, on PATH
// under the name core will look for (D-101).
//
// A shell script rather than a Go binary because what is under test is the
// contract with an arbitrary program — one JSON object on stdout, exit zero —
// and a script is the most honest stand-in for "somebody else's code".
func fake(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, config.Program(name))
	script := "#!/bin/sh\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	return path
}

// answers is a container that prints one thing per subcommand.
func answers(t *testing.T, name string, byCommand map[string]string) string {
	t.Helper()
	var body strings.Builder
	body.WriteString("case \"$1\" in\n")
	for command, answer := range byCommand {
		fmt.Fprintf(&body, "  %s) echo '%s' ;;\n", command, answer)
	}
	body.WriteString("  *) exit 1 ;;\nesac")
	return fake(t, name, body.String())
}

// settingsWith is the configuration, and beside it what the session-watcher
// would have discovered about these programs.
//
// Every fixture here answers all three container verbs unless a test says
// otherwise, because what each test is about is the walking and the verdicts
// rather than the handshake. A test that cares hands over its own map.
func settingsWith(
	order []string, integrations map[string]config.Integration,
) (config.Config, map[string]session.Capabilities) {
	settings := config.Defaults()
	settings.Containers = order
	settings.Integrations = integrations
	settings.Installed = slices.Sorted(maps.Keys(integrations))

	methods := make(map[string]session.Capabilities, len(integrations))
	for name := range integrations {
		methods[name] = session.Capabilities{Methods: []string{
			session.MethodInterpret, session.MethodFocus, session.MethodFocused}}
	}
	return settings, methods
}

// machine is a process table for a focus to walk up: pid to what it is.
type machine map[int]process.Facts

func (m machine) FactsAbout(pid int) (process.Facts, error) {
	facts, found := m[pid]
	if !found {
		return process.Facts{}, process.ErrNoSuchProcess
	}
	return facts, nil
}

// nobody is a machine with nothing on it, for a focus that walks nothing.
var nobody = machine{}

func placed(coordinates map[string]json.RawMessage) session.Record {
	return session.Record{
		Key:            session.Key{Host: "mac", Agent: "claude", SessionID: "s1"},
		Name:           "thing",
		Kernel:         session.Working,
		DerivedContext: coordinates,
	}
}

func TestNothingConfiguredIsItsOwnAnswer(t *testing.T) {
	// A fresh install has no container, and that is the default rather than an
	// edge case: everything except navigation works without one (§A11.7).
	_, outcome := containers.FocusSession(config.Defaults(), nil, placed(nil), nobody)
	if outcome.OK || outcome.Problem != container.NoContainer {
		t.Errorf("outcome = %+v, want no-container-configured", outcome)
	}
}

// TestNeverPlacedIsNotTheSameAsNothingConfigured is the distinction §A11.7 says
// feels identical and is not.
func TestNeverPlacedIsNotTheSameAsNothingConfigured(t *testing.T) {
	answers(t, "outer", map[string]string{"focus": `{"ok":true}`})
	settings, methods := settingsWith([]string{"outer"}, map[string]config.Integration{
		"outer": {},
	})

	_, outcome := containers.FocusSession(settings, methods, placed(nil), nobody)
	if outcome.OK || outcome.Problem != container.NeverPlaced {
		t.Errorf("outcome = %+v, want never-placed", outcome)
	}
}

// TestFocusWalksOutermostFirstAndStops is §A11.2: the inner layer is
// meaningless without the outer, so a failure ends the walk rather than being
// stepped over.
func TestFocusWalksOutermostFirstAndStops(t *testing.T) {
	answers(t, "outer", map[string]string{
		"focus": `{"ok":false,"problem":"not-running","detail":"aerospace is not there"}`})
	fake(t, "inner", "echo 'the inner one was run' >&2; echo '{\"ok\":true}'")

	settings, methods := settingsWith([]string{"outer", "inner"}, map[string]config.Integration{
		"outer": {},
		"inner": {},
	})
	record := placed(map[string]json.RawMessage{
		"outer": json.RawMessage(`{"window":3}`),
		"inner": json.RawMessage(`{"pane":7}`),
	})

	steps, outcome := containers.FocusSession(settings, methods, record, nobody)
	if outcome.OK || outcome.Problem != container.NotRunning {
		t.Errorf("outcome = %+v, want the outer one's failure", outcome)
	}
	if len(steps) != 1 || steps[0].Container != "outer" {
		t.Errorf("steps = %+v, want the walk to have stopped at the outer layer", steps)
	}
}

// TestASkippedLayerIsNotAFailedLayer is the refinement that makes focus usable
// at all: partial placement is the ordinary case.
func TestASkippedLayerIsNotAFailedLayer(t *testing.T) {
	answers(t, "outer", map[string]string{"focus": `{"ok":true}`})
	answers(t, "inner", map[string]string{"focus": `{"ok":true}`})

	settings, methods := settingsWith([]string{"outer", "inner"}, map[string]config.Integration{
		"outer": {},
		"inner": {},
	})
	// Only the inner one ever saw this session — you started tmux inside a
	// terminal window no window manager was recording.
	record := placed(map[string]json.RawMessage{"inner": json.RawMessage(`{"pane":7}`)})

	steps, outcome := containers.FocusSession(settings, methods, record, nobody)
	if !outcome.OK {
		t.Fatalf("outcome = %+v, want success", outcome)
	}
	if len(steps) != 2 || !steps[0].Skipped || steps[1].Skipped {
		t.Errorf("steps = %+v, want the outer skipped and the inner acted on", steps)
	}
}

// TestALayerThatSaysItCannotPlaceThisOneIsSkipped is the second spelling of
// "nothing to do here", and the one M15 needed.
//
// aerospace interprets every session it is handed and can place only the ones
// it can see a window for — a session inside a multiplexer whose client has
// gone leaves it with coordinates and no window. Treating that as a failure
// would stop the walk on the outer layer and never focus the pane, which is
// there and would have worked (D-45).
func TestALayerThatSaysItCannotPlaceThisOneIsSkipped(t *testing.T) {
	answers(t, "outer", map[string]string{
		"focus": `{"ok":false,"problem":"never-placed","detail":"no window is showing this session"}`})
	answers(t, "inner", map[string]string{"focus": `{"ok":true}`})

	settings, methods := settingsWith([]string{"outer", "inner"}, map[string]config.Integration{
		"outer": {},
		"inner": {},
	})
	record := placed(map[string]json.RawMessage{
		"outer": json.RawMessage(`{"title":"home | "}`),
		"inner": json.RawMessage(`{"pane":7}`),
	})

	steps, outcome := containers.FocusSession(settings, methods, record, nobody)
	if !outcome.OK {
		t.Fatalf("outcome = %+v, want the inner layer to have gone ahead", outcome)
	}
	if len(steps) != 2 || !steps[0].Skipped || steps[1].Skipped {
		t.Errorf("steps = %+v, want the outer skipped and the inner acted on", steps)
	}
}

// windowScene is D-141's: an outer container with nothing of its own for the
// session that can bring a window forward, answering focus-window with
// `raised`, and an inner one whose place is shown in one window, a tmux client
// in a Ghostty window. Each writes what it was handed into a directory, under
// the verb's name.
func windowScene(t *testing.T, raised string, outerMethods []string) (
	config.Config, map[string]session.Capabilities, session.Record, machine, string,
) {
	t.Helper()
	handed := t.TempDir()
	fake(t, "outer", `case "$1" in
  focus-window) cat > '`+handed+`/focus-window'; echo '`+raised+`' ;;
  *) exit 1 ;;
esac`)
	fake(t, "inner", `case "$1" in
  windows-showing) cat > '`+handed+`/windows-showing'; echo '[{"id":"/dev/ttys000","pid":1251,"title":"lenny / claude"}]' ;;
  focus) cat > '`+handed+`/focus'; echo '{"ok":true}' ;;
  *) exit 1 ;;
esac`)
	settings, _ := settingsWith([]string{"outer", "inner"}, map[string]config.Integration{
		"outer": {},
		"inner": {},
	})
	methods := map[string]session.Capabilities{
		"outer": {Methods: outerMethods},
		"inner": {Methods: []string{session.MethodFocus, session.MethodWindowsShowing}},
	}
	record := placed(map[string]json.RawMessage{"inner": json.RawMessage(`{"pane":"%38"}`)})
	// tmux attach, under a shell, under Ghostty
	processes := machine{
		1251: {PID: 1251, Command: "tmux", Parent: 1088},
		1088: {PID: 1088, Command: "nu", Parent: 1054},
		1054: {PID: 1054, Command: "ghostty", Parent: 1},
	}
	return settings, methods, record, processes, handed
}

func handedTo(t *testing.T, handed, verb string) string {
	t.Helper()
	read, err := os.ReadFile(filepath.Join(handed, verb))
	if err != nil {
		return ""
	}
	return string(read)
}

// TestTheWindowShowingASessionComesForwardFirst is D-141: the outer layer
// that had nothing of its own is handed the window the inner one names, with
// the chain from its pid, and the inner one is then told to show its place
// through it.
func TestTheWindowShowingASessionComesForwardFirst(t *testing.T) {
	settings, methods, record, processes, handed := windowScene(t, `{"ok":true}`,
		[]string{session.MethodFocus, session.MethodFocusWindow})

	steps, outcome := containers.FocusSession(settings, methods, record, processes)
	if !outcome.OK {
		t.Fatalf("outcome = %+v, want success", outcome)
	}
	if len(steps) != 3 || !steps[0].Skipped || steps[1].Window != "/dev/ttys000" || !steps[1].Outcome.OK || steps[2].Container != "inner" {
		t.Errorf("steps = %+v, want the outer skipped, then its window brought forward, then the inner acted on", steps)
	}
	var window container.FocusWindowInput
	if err := json.Unmarshal([]byte(handedTo(t, handed, "focus-window")), &window); err != nil {
		t.Fatalf("focus-window was handed %q: %v", handedTo(t, handed, "focus-window"), err)
	}
	if window.Window.Title != "lenny / claude" || len(window.Ancestry) != 3 || window.Ancestry[2].PID != 1054 {
		t.Errorf("focus-window was handed %+v, want the window and the chain up to Ghostty", window)
	}
	var focus container.FocusInput
	if err := json.Unmarshal([]byte(handedTo(t, handed, "focus")), &focus); err != nil {
		t.Fatalf("focus was handed %q: %v", handedTo(t, handed, "focus"), err)
	}
	if focus.Window == nil || focus.Window.ID != "/dev/ttys000" || string(focus.Coordinates) != `{"pane":"%38"}` {
		t.Errorf("focus was handed %+v, want the coordinates and the window that came forward", focus)
	}
}

// TestAWindowThatDoesNotComeForwardIsNotUsed: the inner layer acts as it would
// have alone, and the focus has not failed. A terminal is never moved behind a
// window nobody brought forward (D-123).
func TestAWindowThatDoesNotComeForwardIsNotUsed(t *testing.T) {
	settings, methods, record, processes, handed := windowScene(t,
		`{"ok":false,"problem":"ambiguous","detail":"2 Ghostty windows"}`,
		[]string{session.MethodFocus, session.MethodFocusWindow})

	steps, outcome := containers.FocusSession(settings, methods, record, processes)
	if !outcome.OK {
		t.Fatalf("outcome = %+v, want the inner layer to have gone ahead", outcome)
	}
	if len(steps) != 3 || !steps[1].Skipped || steps[1].Window == "" {
		t.Errorf("steps = %+v, want the window that did not come forward as a step skipped", steps)
	}
	var focus container.FocusInput
	if err := json.Unmarshal([]byte(handedTo(t, handed, "focus")), &focus); err != nil {
		t.Fatalf("focus was handed %q: %v", handedTo(t, handed, "focus"), err)
	}
	if focus.Window != nil {
		t.Errorf("focus was handed the window %+v, which never came forward", focus.Window)
	}
}

// TestNoWindowIsAskedForWithNothingToBringItForward: an outer layer that
// cannot focus a window costs the inner one no question.
func TestNoWindowIsAskedForWithNothingToBringItForward(t *testing.T) {
	settings, methods, record, processes, handed := windowScene(t, `{"ok":true}`,
		[]string{session.MethodFocus})

	steps, outcome := containers.FocusSession(settings, methods, record, processes)
	if !outcome.OK || len(steps) != 2 {
		t.Fatalf("steps = %+v, outcome = %+v; want the outer skipped and the inner acted on", steps, outcome)
	}
	if asked := handedTo(t, handed, "windows-showing"); asked != "" {
		t.Errorf("the inner layer was asked which windows show it, with nobody to bring one forward")
	}
}

// TestAmbiguityStopsTheWalk is the other half of the same decision: a layer
// that can see several places and cannot choose must not let the walk carry on
// as if it had arrived (§A11.3).
func TestAmbiguityStopsTheWalk(t *testing.T) {
	answers(t, "outer", map[string]string{
		"focus": `{"ok":false,"problem":"ambiguous","detail":"3 windows could be this session"}`})
	fake(t, "inner", "echo 'the inner one was run' >&2; echo '{\"ok\":true}'")

	settings, methods := settingsWith([]string{"outer", "inner"}, map[string]config.Integration{
		"outer": {},
		"inner": {},
	})
	record := placed(map[string]json.RawMessage{
		"outer": json.RawMessage(`{"title":"home | "}`),
		"inner": json.RawMessage(`{"pane":7}`),
	})

	steps, outcome := containers.FocusSession(settings, methods, record, nobody)
	if outcome.OK || outcome.Problem != container.Ambiguous {
		t.Errorf("outcome = %+v, want ambiguous", outcome)
	}
	if len(steps) != 1 {
		t.Errorf("steps = %+v, want the walk to have stopped at the outer layer", steps)
	}
}

// TestAContainerThatIsNotOnPATHIsUnreachable: a container is always a program
// (D-57), and the table is the ask (D-101). One nobody built is not a
// configuration error, it is a program that could not be run.
func TestAContainerThatIsNotOnPATHIsUnreachable(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	settings, methods := settingsWith([]string{"tmux"}, map[string]config.Integration{
		"tmux": {},
	})
	record := placed(map[string]json.RawMessage{"tmux": json.RawMessage(`{"TMUX_PANE":"%7"}`)})
	_, outcome := containers.FocusSession(settings, methods, record, nobody)
	if outcome.OK || outcome.Problem != container.Unreachable {
		t.Errorf("outcome = %+v, want container-unreachable", outcome)
	}
}

// TestOneContainerNeedsNoOrder is D-101: with nothing to nest, an absent
// `containers` is the one container; with two, it has to be written.
func TestOneContainerNeedsNoOrder(t *testing.T) {
	settings, methods := settingsWith(nil, map[string]config.Integration{"only": {}})
	configured, problems := containers.Configured(settings, methods)
	if len(problems) != 0 || len(configured) != 1 || configured[0].Name != "only" {
		t.Errorf("configured = %v, problems = %v; want the one container and no complaint", configured, problems)
	}

	settings, methods = settingsWith(nil, map[string]config.Integration{"outer": {}, "inner": {}})
	configured, problems = containers.Configured(settings, methods)
	if len(configured) != 0 || len(problems) != 1 || !strings.Contains(problems[0].Error(), "`containers`") {
		t.Errorf("configured = %v, problems = %v; want nothing walked and one complaint asking for the order", configured, problems)
	}
}

// TestAContainerThatHangsIsGivenUpOn: a focus that never returns is a person
// waiting at a keyboard, so it is bounded and it is named (R18).
func TestAContainerThatHangsIsGivenUpOn(t *testing.T) {
	binary := fake(t, "slow", "sleep 30")
	one := containers.Container{Name: "slow", Binary: binary}

	// Asked directly rather than through FocusSession, because the timeout is
	// what is being measured and it is an argument here — five seconds is the
	// right bound for a person waiting and the wrong one for a test.
	start := time.Now()
	outcome := containers.Focus(one, json.RawMessage(`{"pane":1}`), nil, 200*time.Millisecond)
	waited := time.Since(start)

	if outcome.OK || outcome.Problem != container.Unreachable {
		t.Errorf("outcome = %+v, want container-unreachable", outcome)
	}
	if waited > 3*time.Second {
		t.Errorf("waited %v for a 200ms timeout", waited)
	}
}

// TestAContainerThatAnswersNonsenseIsUnreachable: core reads no meaning into
// how a program failed. Crashed, deleted, hung and babbling are one thing.
func TestAContainerThatAnswersNonsenseIsUnreachable(t *testing.T) {
	for _, body := range []string{"echo not json", "exit 3", "echo '{}' ; exit 9"} {
		fake(t, "odd", body)
		settings, methods := settingsWith([]string{"odd"}, map[string]config.Integration{"odd": {}})
		record := placed(map[string]json.RawMessage{"odd": json.RawMessage(`{"pane":1}`)})

		if _, outcome := containers.FocusSession(settings, methods, record, nobody); outcome.Problem != container.Unreachable {
			t.Errorf("%q produced %+v, want container-unreachable", body, outcome)
		}
	}
}

// TestAContainerThatSaysNoWithoutSayingWhyHasStillSaidNo.
func TestARefusalWithNoReasonIsStillARefusal(t *testing.T) {
	answers(t, "shy", map[string]string{"focus": `{"ok":false}`})
	settings, methods := settingsWith([]string{"shy"}, map[string]config.Integration{"shy": {}})
	record := placed(map[string]json.RawMessage{"shy": json.RawMessage(`{"pane":1}`)})

	if _, outcome := containers.FocusSession(settings, methods, record, nobody); outcome.Problem != container.Refused {
		t.Errorf("outcome = %+v, want refused", outcome)
	}
}

// TestInFrontOnlyIfEveryLayerAgrees is §A11.6, and the reason the third answer
// exists: aerospace saying yes while tmux cannot say is not a yes.
func TestInFrontOnlyIfEveryLayerAgrees(t *testing.T) {
	yes := func(t *testing.T, name string) string {
		return answers(t, name, map[string]string{"focused": `{"answer":"yes"}`})
	}
	no := func(t *testing.T, name string) string {
		return answers(t, name, map[string]string{"focused": `{"answer":"no","detail":"another tab"}`})
	}
	dunno := func(t *testing.T, name string) string {
		return answers(t, name, map[string]string{"focused": `{"answer":"cannot-tell"}`})
	}

	for _, want := range []struct {
		outer, inner func(*testing.T, string) string
		answer       container.Answer
		why          string
	}{
		{yes, yes, container.Yes, "every layer agrees"},
		{yes, no, container.No, "one no is decisive: you are demonstrably looking at something else"},
		{no, yes, container.No, "and it is decisive from either layer"},
		{yes, dunno, container.CannotTell, "a layer that cannot say turns a yes into an unknown"},
		{dunno, dunno, container.CannotTell, "nobody knows"},
	} {
		want.outer(t, "outer")
		want.inner(t, "inner")
		settings, methods := settingsWith([]string{"outer", "inner"}, map[string]config.Integration{
			"outer": {},
			"inner": {},
		})
		record := placed(map[string]json.RawMessage{
			"outer": json.RawMessage(`{"window":3}`),
			"inner": json.RawMessage(`{"pane":7}`),
		})
		if got := containers.IsFocused(settings, methods, record); got.Answer != want.answer {
			t.Errorf("%s: answered %q, want %q", want.why, got.Answer, want.answer)
		}
	}
}

// TestAnUnplacedLayerCannotSay: no coordinates is not "no", because a wrong no
// silences a notification that should have fired.
func TestAnUnplacedLayerCannotSay(t *testing.T) {
	// One program, under both names: the outer one is unplaced anyway.
	answers(t, "inner", map[string]string{"focused": `{"answer":"yes"}`})
	answers(t, "outer", map[string]string{"focused": `{"answer":"yes"}`})
	settings, methods := settingsWith([]string{"outer", "inner"}, map[string]config.Integration{
		"outer": {},
		"inner": {},
	})
	record := placed(map[string]json.RawMessage{"inner": json.RawMessage(`{"pane":7}`)})

	if got := containers.IsFocused(settings, methods, record); got.Answer != container.CannotTell {
		t.Errorf("answered %q, want cannot-tell", got.Answer)
	}
}

// TestAnOrderThatNamesNothingIsReported: the order is the one place a typo
// cannot be caught by anything else, because a container's name is a name core
// has never heard of (R10).
func TestAnOrderThatNamesNothingIsReported(t *testing.T) {
	settings, methods := settingsWith([]string{"tmx"}, map[string]config.Integration{
		"tmux": {},
	})
	configured, problems := containers.Configured(settings, methods)
	if len(configured) != 0 {
		t.Errorf("configured = %v, want nothing", configured)
	}
	// Two, and the second is the same mistake seen from the other side: the
	// real container answers `focus` and is in nobody's order, so nothing will
	// ever ask it to place anything.
	if len(problems) != 2 {
		t.Fatalf("problems = %v, want the typo and the container it orphaned", problems)
	}
	if !strings.Contains(problems[0].Error(), "tmx") {
		t.Errorf("problems = %v, want the first to name the misspelled entry", problems)
	}
	if !strings.Contains(problems[1].Error(), "`containers`") {
		t.Errorf("problems = %v, want the second to say it is in nobody's order", problems)
	}
}

func TestADisabledContainerIsSkippedSilently(t *testing.T) {
	off := false
	settings, methods := settingsWith([]string{"tmux"}, map[string]config.Integration{
		"tmux": {Enabled: &off},
	})
	configured, problems := containers.Configured(settings, methods)
	if len(configured) != 0 || len(problems) != 0 {
		t.Errorf("configured = %v, problems = %v; turning one off is not a mistake", configured, problems)
	}
}
