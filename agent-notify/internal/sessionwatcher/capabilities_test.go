package sessionwatcher_test

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lassoColombo/agent-notify/internal/handshake"
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
	var one handshake.Integration
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); {
		if one, _ = reported(t, layout, "painter"); len(one.Answers.Methods) > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(one.Answers.Methods) == 0 {
		t.Fatalf("waited for the handshake to be reported and it never happened; the report says %+v", one)
	}
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
