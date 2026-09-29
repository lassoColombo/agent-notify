package sessionwatcher_test

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lassoColombo/agent-notify/internal/config"
	"github.com/lassoColombo/agent-notify/internal/paths"
	"github.com/lassoColombo/agent-notify/internal/sessionwatcher"
	"github.com/lassoColombo/agent-notify/session"
)

// TestAnIntegrationIsAskedWhatItAnswers is the handshake arriving: the
// session-watcher runs `capabilities` once at startup and the report carries
// what came back.
func TestAnIntegrationIsAskedWhatItAnswers(t *testing.T) {
	atATestablePace(t, 200*time.Millisecond)
	root := shortRoot(t)
	binary := answersCapabilities(t, root, "painter",
		`{"version":"0.0.0-dev","methods":["render"],"wake_on":["kernel"],"want_ended":true}`)
	write(t, root+"/config.toml", fmt.Sprintf(
		"[integration.painter]\nbinary = %q\n", binary))

	layout := running(t, root)
	waitFor(t, "the handshake to be reported", func() bool {
		one, found := reported(t, layout, "painter")
		return found && len(one.Answers.Methods) > 0
	})

	one, _ := reported(t, layout, "painter")
	if !slices.Equal(one.Answers.Methods, []string{session.MethodRender}) {
		t.Errorf("answers %v, want [render]", one.Answers.Methods)
	}
	if !slices.Equal(one.Answers.WakeOn, []string{"kernel"}) {
		t.Errorf("wakes on %v, want [kernel]", one.Answers.WakeOn)
	}
	if !one.Answers.WantEnded {
		t.Error("asked for ended sessions and the report says otherwise")
	}
	if one.Answers.Version != "0.0.0-dev" {
		t.Errorf("built against %q", one.Answers.Version)
	}
	if one.Problem != "" {
		t.Errorf("answered and the report still carries a problem: %s", one.Problem)
	}
}

// TestAnIntegrationThatCannotBeAskedSaysSoInTheReport is the case the Problem
// field exists for.
//
// Without it, a display whose binary was deleted reports no methods, which is
// exactly what a display with nothing to offer reports — and core would stop
// running it for ever without anybody being told why.
func TestAnIntegrationThatCannotBeAskedSaysSoInTheReport(t *testing.T) {
	atATestablePace(t, 100*time.Millisecond)
	root := shortRoot(t)
	missing := filepath.Join(root, "agent-notify-ghost")
	write(t, root+"/config.toml", fmt.Sprintf(
		"[integration.ghost]\nbinary = %q\n", missing))

	layout := running(t, root)
	waitFor(t, "the missing integration to be reported", func() bool {
		one, found := reported(t, layout, "ghost")
		return found && one.Problem != ""
	})

	one, _ := reported(t, layout, "ghost")
	if len(one.Answers.Methods) != 0 {
		t.Errorf("a program that is not there answers %v", one.Answers.Methods)
	}
	if !strings.Contains(one.Problem, missing) {
		t.Errorf("the problem does not name the path that is wrong: %s", one.Problem)
	}
}

// TestWithNoReportItAsksRatherThanSayingThereIsNoContainer is what keeps a
// focus working on a machine where nothing is running.
//
// Reading the report is the shortcut — a focus comes off a keypress and must
// not run every container to find out what a container is — but the runtime
// directory does not survive a reboot, so the first focus after one has no
// report to read. Answering "you have no containers" then would be a lie told
// at exactly the wrong moment.
func TestWithNoReportItAsksRatherThanSayingThereIsNoContainer(t *testing.T) {
	root := shortRoot(t)
	binary := answersCapabilities(t, root, "placer",
		`{"version":"0.0.0-dev","methods":["interpret-environment","focus","focused"]}`)
	write(t, root+"/config.toml", fmt.Sprintf(
		"[integration.placer]\nbinary = %q\n", binary))

	t.Setenv(paths.TheVariableThatNamesTheRoot, root)
	layout, err := paths.FromEnvironment()
	if err != nil {
		t.Fatalf("FromEnvironment: %v", err)
	}
	settings, problems := config.Load(layout)
	if len(problems) > 0 {
		t.Fatalf("reading the config: %v", problems)
	}

	// No session-watcher has ever run here, so there is no report at all.
	methods := sessionwatcher.CapabilitiesByIntegration(layout, settings)
	if !slices.Contains(methods["placer"].Methods, session.MethodFocus) {
		t.Errorf("with no report it found %v, and the program on disk answers focus",
			methods["placer"].Methods)
	}
}

// TestAContainerIsInTheReportAtAll is the hole this step closes.
//
// A container is run when core has a question and never supervised, so it was
// absent from the report entirely: doctor could tell you about the displays and
// not one word about the programs that place your sessions.
func TestAContainerIsInTheReportAtAll(t *testing.T) {
	atATestablePace(t, 100*time.Millisecond)
	root := shortRoot(t)
	binary := answersCapabilities(t, root, "placer",
		`{"version":"0.0.0-dev","methods":["interpret-environment","focus","focused"]}`)
	write(t, root+"/config.toml", fmt.Sprintf(
		"[integration.placer]\nbinary = %q\n\n[container]\norder = [\"placer\"]\n", binary))

	layout := running(t, root)
	waitFor(t, "the container to be reported", func() bool {
		_, found := reported(t, layout, "placer")
		return found
	})

	one, _ := reported(t, layout, "placer")
	if !slices.Contains(one.Answers.Methods, session.MethodFocus) {
		t.Errorf("answers %v, and a container answers focus", one.Answers.Methods)
	}
	if one.State != "run when core needs it" {
		t.Errorf("a container is reported as %q, and it has no process to have a state", one.State)
	}
}
