package containers_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agentnotify "github.com/lassoColombo/agent-notify"
	"github.com/lassoColombo/agent-notify/container"
	"github.com/lassoColombo/agent-notify/internal/config"
	"github.com/lassoColombo/agent-notify/internal/containers"
)

// fake writes a program that answers whatever this test needs it to.
//
// A shell script rather than a Go binary because what is under test is the
// contract with an arbitrary program — one JSON object on stdout, exit zero —
// and a script is the most honest stand-in for "somebody else's code".
func fake(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	script := "#!/bin/sh\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
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

func settingsWith(order []string, integrations map[string]config.Integration) config.Config {
	settings := config.Defaults()
	settings.Container.Order = order
	settings.Integration = integrations
	return settings
}

func placed(coordinates map[string]json.RawMessage) agentnotify.Record {
	return agentnotify.Record{
		Key:            agentnotify.Key{Host: "mac", Agent: "claude", SessionID: "s1"},
		Name:           "thing",
		Kernel:         agentnotify.Working,
		DerivedContext: coordinates,
	}
}

func TestNothingConfiguredIsItsOwnAnswer(t *testing.T) {
	// A fresh install has no container, and that is the default rather than an
	// edge case: everything except navigation works without one (§A11.7).
	_, outcome := containers.FocusSession(config.Defaults(), placed(nil))
	if outcome.OK || outcome.Problem != container.NoContainer {
		t.Errorf("outcome = %+v, want no-container-configured", outcome)
	}
}

// TestNeverPlacedIsNotTheSameAsNothingConfigured is the distinction §A11.7 says
// feels identical and is not.
func TestNeverPlacedIsNotTheSameAsNothingConfigured(t *testing.T) {
	binary := answers(t, "outer", map[string]string{"focus": `{"ok":true}`})
	settings := settingsWith([]string{"outer"}, map[string]config.Integration{
		"outer": {Binary: binary},
	})

	_, outcome := containers.FocusSession(settings, placed(nil))
	if outcome.OK || outcome.Problem != container.NeverPlaced {
		t.Errorf("outcome = %+v, want never-placed", outcome)
	}
}

// TestFocusWalksOutermostFirstAndStops is §A11.2: the inner layer is
// meaningless without the outer, so a failure ends the walk rather than being
// stepped over.
func TestFocusWalksOutermostFirstAndStops(t *testing.T) {
	outer := answers(t, "outer", map[string]string{
		"focus": `{"ok":false,"problem":"not-running","detail":"aerospace is not there"}`})
	inner := fake(t, "inner", "echo 'the inner one was run' >&2; echo '{\"ok\":true}'")

	settings := settingsWith([]string{"outer", "inner"}, map[string]config.Integration{
		"outer": {Binary: outer},
		"inner": {Binary: inner},
	})
	record := placed(map[string]json.RawMessage{
		"outer": json.RawMessage(`{"window":3}`),
		"inner": json.RawMessage(`{"pane":7}`),
	})

	steps, outcome := containers.FocusSession(settings, record)
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
	outer := answers(t, "outer", map[string]string{"focus": `{"ok":true}`})
	inner := answers(t, "inner", map[string]string{"focus": `{"ok":true}`})

	settings := settingsWith([]string{"outer", "inner"}, map[string]config.Integration{
		"outer": {Binary: outer},
		"inner": {Binary: inner},
	})
	// Only the inner one ever saw this session — you started zellij inside a
	// terminal window no window manager was recording.
	record := placed(map[string]json.RawMessage{"inner": json.RawMessage(`{"pane":7}`)})

	steps, outcome := containers.FocusSession(settings, record)
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
	outer := answers(t, "outer", map[string]string{
		"focus": `{"ok":false,"problem":"never-placed","detail":"no window is showing this session"}`})
	inner := answers(t, "inner", map[string]string{"focus": `{"ok":true}`})

	settings := settingsWith([]string{"outer", "inner"}, map[string]config.Integration{
		"outer": {Binary: outer},
		"inner": {Binary: inner},
	})
	record := placed(map[string]json.RawMessage{
		"outer": json.RawMessage(`{"title":"home | "}`),
		"inner": json.RawMessage(`{"pane":7}`),
	})

	steps, outcome := containers.FocusSession(settings, record)
	if !outcome.OK {
		t.Fatalf("outcome = %+v, want the inner layer to have gone ahead", outcome)
	}
	if len(steps) != 2 || !steps[0].Skipped || steps[1].Skipped {
		t.Errorf("steps = %+v, want the outer skipped and the inner acted on", steps)
	}
}

// TestAmbiguityStopsTheWalk is the other half of the same decision: a layer
// that can see several places and cannot choose must not let the walk carry on
// as if it had arrived (§A11.3).
func TestAmbiguityStopsTheWalk(t *testing.T) {
	outer := answers(t, "outer", map[string]string{
		"focus": `{"ok":false,"problem":"ambiguous","detail":"3 windows could be this session"}`})
	inner := fake(t, "inner", "echo 'the inner one was run' >&2; echo '{\"ok\":true}'")

	settings := settingsWith([]string{"outer", "inner"}, map[string]config.Integration{
		"outer": {Binary: outer},
		"inner": {Binary: inner},
	})
	record := placed(map[string]json.RawMessage{
		"outer": json.RawMessage(`{"title":"home | "}`),
		"inner": json.RawMessage(`{"pane":7}`),
	})

	steps, outcome := containers.FocusSession(settings, record)
	if outcome.OK || outcome.Problem != container.Ambiguous {
		t.Errorf("outcome = %+v, want ambiguous", outcome)
	}
	if len(steps) != 1 {
		t.Errorf("steps = %+v, want the walk to have stopped at the outer layer", steps)
	}
}

// TestAContainerThatIsOnlyConfiguration is §A11.4: four lines in a file, and
// not a degraded form of anything.
// A container is always a program (D-57). A table with no binary used to mean
// "capture these variables and run this command"; now it means nothing can be
// asked, and saying so is the whole of what is left to test.
func TestAContainerWithNoBinaryIsRefusedAndNamed(t *testing.T) {
	settings := settingsWith([]string{"tmux"}, map[string]config.Integration{
		"tmux": {},
	})
	configured, problems := containers.Configured(settings)
	if len(configured) != 0 {
		t.Errorf("Configured = %v, want nothing runnable", configured)
	}
	if len(problems) != 1 || !strings.Contains(problems[0].Error(), "tmux") {
		t.Fatalf("problems = %v, want one naming tmux", problems)
	}

	record := placed(map[string]json.RawMessage{"tmux": json.RawMessage(`{"TMUX_PANE":"%7"}`)})
	_, outcome := containers.FocusSession(settings, record)
	if outcome.OK || outcome.Problem != container.NoContainer {
		t.Errorf("outcome = %+v, want no-container-configured", outcome)
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
	outcome := containers.Focus(one, json.RawMessage(`{"pane":1}`), 200*time.Millisecond)
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
		binary := fake(t, "odd", body)
		settings := settingsWith([]string{"odd"}, map[string]config.Integration{"odd": {Binary: binary}})
		record := placed(map[string]json.RawMessage{"odd": json.RawMessage(`{"pane":1}`)})

		if _, outcome := containers.FocusSession(settings, record); outcome.Problem != container.Unreachable {
			t.Errorf("%q produced %+v, want container-unreachable", body, outcome)
		}
	}
}

// TestAContainerThatSaysNoWithoutSayingWhyHasStillSaidNo.
func TestARefusalWithNoReasonIsStillARefusal(t *testing.T) {
	binary := answers(t, "shy", map[string]string{"focus": `{"ok":false}`})
	settings := settingsWith([]string{"shy"}, map[string]config.Integration{"shy": {Binary: binary}})
	record := placed(map[string]json.RawMessage{"shy": json.RawMessage(`{"pane":1}`)})

	if _, outcome := containers.FocusSession(settings, record); outcome.Problem != container.Refused {
		t.Errorf("outcome = %+v, want refused", outcome)
	}
}

// TestInFrontOnlyIfEveryLayerAgrees is §A11.6, and the reason the third answer
// exists: aerospace saying yes while zellij cannot say is not a yes.
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
		settings := settingsWith([]string{"outer", "inner"}, map[string]config.Integration{
			"outer": {Binary: want.outer(t, "outer")},
			"inner": {Binary: want.inner(t, "inner")},
		})
		record := placed(map[string]json.RawMessage{
			"outer": json.RawMessage(`{"window":3}`),
			"inner": json.RawMessage(`{"pane":7}`),
		})
		if got := containers.IsFocused(settings, record); got.Answer != want.answer {
			t.Errorf("%s: answered %q, want %q", want.why, got.Answer, want.answer)
		}
	}
}

// TestAnUnplacedLayerCannotSay: no coordinates is not "no", because a wrong no
// silences a notification that should have fired.
func TestAnUnplacedLayerCannotSay(t *testing.T) {
	binary := answers(t, "inner", map[string]string{"focused": `{"answer":"yes"}`})
	settings := settingsWith([]string{"outer", "inner"}, map[string]config.Integration{
		"outer": {Binary: binary},
		"inner": {Binary: binary},
	})
	record := placed(map[string]json.RawMessage{"inner": json.RawMessage(`{"pane":7}`)})

	if got := containers.IsFocused(settings, record); got.Answer != container.CannotTell {
		t.Errorf("answered %q, want cannot-tell", got.Answer)
	}
}

// TestAnOrderThatNamesNothingIsReported: the order is the one place a typo
// cannot be caught by anything else, because a container's name is a name core
// has never heard of (R10).
func TestAnOrderThatNamesNothingIsReported(t *testing.T) {
	settings := settingsWith([]string{"zelij-container"}, map[string]config.Integration{
		"zellij-container": {Binary: "/bin/true"},
	})
	configured, problems := containers.Configured(settings)
	if len(configured) != 0 {
		t.Errorf("configured = %v, want nothing", configured)
	}
	if len(problems) != 1 || !strings.Contains(problems[0].Error(), "zelij-container") {
		t.Errorf("problems = %v, want one naming the misspelled entry", problems)
	}
}

func TestADisabledContainerIsSkippedSilently(t *testing.T) {
	off := false
	settings := settingsWith([]string{"zellij-container"}, map[string]config.Integration{
		"zellij-container": {Binary: "/bin/true", Enabled: &off},
	})
	configured, problems := containers.Configured(settings)
	if len(configured) != 0 || len(problems) != 0 {
		t.Errorf("configured = %v, problems = %v; turning one off is not a mistake", configured, problems)
	}
}
