package main

import (
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lassoColombo/agent-notify/container"
	"github.com/lassoColombo/agent-notify/session"
)

// These tests run against a real tmux. That is the point: everything this
// integration knows about tmux is a fact about a program, and a fake tmux
// would only ever confirm what this file already believes.
//
// Each test gets a server of its own on a socket of its own, started with no
// configuration file, so nothing here can touch — or be shaped by — the
// developer's own tmux. They skip themselves where there is no tmux.

func realTmux(t *testing.T) Tmux {
	t.Helper()
	// A test runs in a shell, which is the one context where looking tmux up
	// is the right thing to do (D-67).
	binary, err := exec.LookPath("tmux")
	if err != nil {
		t.Skipf("no tmux here: %v", err)
	}
	return Tmux{Binary: binary, Timeout: 5 * time.Second}
}

// aServer starts a private tmux server holding one session, and returns its
// socket. The directory is made by hand rather than by t.TempDir, because a
// unix socket path is limited to about a hundred bytes and t.TempDir's are
// longer than that on macOS.
func aServer(t *testing.T, tmux Tmux, name string) string {
	t.Helper()
	directory, err := os.MkdirTemp("", "an-tmux-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	socket := filepath.Join(directory, "s")
	t.Cleanup(func() {
		_, _ = bare(tmux, socket, "kill-server")
		_ = os.RemoveAll(directory)
	})
	start(t, tmux, socket, name)
	return socket
}

func start(t *testing.T, tmux Tmux, socket, name string) {
	t.Helper()
	if out, err := bare(tmux, socket, "new-session", "-d", "-s", name, "-x", "120", "-y", "40",
		"sleep", "600"); err != nil {
		t.Skipf("cannot start a tmux server (%v): %s", err, out)
	}
}

// bare runs tmux the way a person would set a server up: with no
// configuration, and without this process's own $TMUX, so that a suite run
// from inside tmux never reaches the developer's server by accident.
func bare(tmux Tmux, socket string, arguments ...string) (string, error) {
	command := exec.Command(tmux.Binary, append([]string{"-f", "/dev/null", "-S", socket}, arguments...)...)
	command.Env = stripped(os.Environ())
	out, err := command.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func must(t *testing.T, tmux Tmux, socket string, arguments ...string) string {
	t.Helper()
	out, err := bare(tmux, socket, arguments...)
	if err != nil {
		t.Fatalf("tmux %s: %v: %s", strings.Join(arguments, " "), err, out)
	}
	return out
}

func stripped(environment []string) []string {
	var kept []string
	for _, variable := range environment {
		if !strings.HasPrefix(variable, "TMUX") {
			kept = append(kept, variable)
		}
	}
	return kept
}

func pidOf(t *testing.T, tmux Tmux, socket string) int {
	t.Helper()
	pid, err := strconv.Atoi(must(t, tmux, socket, "display-message", "-p", "#{pid}"))
	if err != nil {
		t.Fatalf("the server's pid: %v", err)
	}
	return pid
}

func paneOf(t *testing.T, tmux Tmux, socket, target string) string {
	t.Helper()
	return must(t, tmux, socket, "display-message", "-p", "-t", target, "#{pane_id}")
}

func marksOf(t *testing.T, tmux Tmux, socket string) map[string]Mark {
	t.Helper()
	server, err := tmux.Look(socket)
	if err != nil {
		t.Fatalf("Look: %v", err)
	}
	return server.Panes
}

func display(tmux Tmux) *Display {
	return &Display{Tmux: tmux, Glyphs: testGlyphs, Logger: slog.New(slog.DiscardHandler)}
}

func render(t *testing.T, tmux Tmux, records ...session.Record) {
	t.Helper()
	if err := display(tmux).Render(session.View{Sessions: records}); err != nil {
		t.Fatalf("Render: %v", err)
	}
}

// TestTheParseDecidesNotTheExitCode: /bin/echo is a program that exits zero
// and does not answer in the format it was asked for, which is precisely the
// failure being guarded against.
func TestTheParseDecidesNotTheExitCode(t *testing.T) {
	liar := Tmux{Binary: "/bin/echo", Timeout: 5 * time.Second}
	if _, err := liar.Look(home); err == nil {
		t.Fatal("a program that exits 0 and answers with prose was believed")
	} else if !strings.Contains(err.Error(), "is not there") {
		t.Errorf("the error does not say what happened: %v", err)
	}
}

// TestAMissingServerFails: a socket nobody is listening on is a server that is
// not there, and asking it starts nothing.
func TestAMissingServerFails(t *testing.T) {
	tmux := realTmux(t)
	directory, err := os.MkdirTemp("", "an-tmux-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(directory)
	socket := filepath.Join(directory, "s")

	if _, err := tmux.Look(socket); err == nil {
		t.Fatal("asking a server that is not there succeeded")
	}
	if _, err := os.Stat(socket); err == nil {
		t.Error("asking a server that is not there started one")
	}
}

// TestReadingWithNoLocale: the session-watcher runs with no LANG, and a tmux
// that does not think its output is UTF-8 prints the separator and every glyph
// as `_` (D-123).
func TestReadingWithNoLocale(t *testing.T) {
	tmux := realTmux(t)
	socket := aServer(t, tmux, "an-test")
	pane := paneOf(t, tmux, socket, "an-test")
	for _, variable := range []string{"LANG", "LC_ALL", "LC_CTYPE"} {
		t.Setenv(variable, "")
	}

	shipped := &Display{Tmux: tmux, Glyphs: session.NewPalette(DefaultGlyphs, nil), Logger: slog.New(slog.DiscardHandler)}
	if err := shipped.Render(session.View{Sessions: []session.Record{
		placedIn("alpha", session.Working, socket, pidOf(t, tmux, socket), pane),
	}}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	want := Mark{State: "working", Glyph: DefaultGlyphs[session.Working], Name: "alpha"}
	if got := marksOf(t, tmux, socket)[pane]; got != want {
		t.Errorf("with no locale, pane %s reads back %+v, want %+v", pane, got, want)
	}
}

func TestMarkingARealTmux(t *testing.T) {
	tmux := realTmux(t)
	socket := aServer(t, tmux, "an-test")
	server := pidOf(t, tmux, socket)
	first := paneOf(t, tmux, socket, "an-test")
	second := must(t, tmux, socket, "split-window", "-P", "-F", "#{pane_id}", "-t", first, "sleep", "600")
	window := must(t, tmux, socket, "display-message", "-p", "-t", first, "#{window_id}")

	alpha := placedIn("alpha", session.Working, socket, server, first)
	render(t, tmux, alpha, placedIn("beta", session.BlockedOnYou, socket, server, second))
	marks := marksOf(t, tmux, socket)
	if want := (Mark{State: "working", Glyph: "*", Name: "alpha"}); marks[first] != want {
		t.Errorf("pane %s says %+v, want %+v", first, marks[first], want)
	}
	if want := (Mark{State: "blocked-on-you", Glyph: "!", Name: "beta"}); marks[second] != want {
		t.Errorf("pane %s says %+v, want %+v", second, marks[second], want)
	}

	// One ends, and its pane is given back.
	render(t, tmux, alpha, placedIn("beta", session.Ended, socket, server, second))
	if marks = marksOf(t, tmux, socket); marks[second] != (Mark{}) {
		t.Errorf("pane %s still says %+v after its agent ended", second, marks[second])
	}

	// Nothing is ever written on the window.
	for _, option := range options {
		if got, _ := bare(tmux, socket, "show-options", "-w", "-v", "-q", "-t", window, option); got != "" {
			t.Errorf("window %s carries %s = %q", window, option, got)
		}
	}
}

// TestAWindowShowsWhatItsPanesSay is why nothing is written on a window: a
// format loops over the panes a window holds when tmux draws it, so a pane
// moved to another window takes its mark there, with no render in between.
func TestAWindowShowsWhatItsPanesSay(t *testing.T) {
	tmux := realTmux(t)
	socket := aServer(t, tmux, "an-test")
	first := paneOf(t, tmux, socket, "an-test")
	agent := must(t, tmux, socket, "split-window", "-P", "-F", "#{pane_id}", "-t", first, "sleep", "600")
	render(t, tmux, placedIn("alpha", session.FinishedATurn, socket, pidOf(t, tmux, socket), agent))

	windows := func() string {
		return must(t, tmux, socket, "display-message", "-p", "-t", "an-test",
			"#{W:#{window_index}=#{P:#{"+glyphOption+"}} }")
	}
	if got := windows(); got != "0=>" {
		t.Errorf("windows read %q, want the agent's glyph on window 0", got)
	}
	must(t, tmux, socket, "break-pane", "-d", "-s", agent)
	if got := windows(); got != "0= 1=>" {
		t.Errorf("after break-pane, windows read %q, want the glyph on window 1 only", got)
	}
}

// TestRenderingTwiceRunsNothingTheSecondTime is the property that lets this be
// called on every change: a render is the difference, not the state.
func TestRenderingTwiceRunsNothingTheSecondTime(t *testing.T) {
	tmux := realTmux(t)
	socket := aServer(t, tmux, "an-test")
	records := []session.Record{
		placedIn("alpha", session.Working, socket, pidOf(t, tmux, socket), paneOf(t, tmux, socket, "an-test")),
	}
	render(t, tmux, records...)

	server, err := tmux.Look(socket)
	if err != nil {
		t.Fatalf("Look: %v", err)
	}
	if plan := Plan(socket, records, server, testGlyphs); len(plan) != 0 {
		t.Errorf("a second render would run %v", ran(plan))
	}
}

// TestNamesTmuxWouldMisreadArriveWhole: a name is the agent's, it is written
// in one invocation with every other mark, and it reads back as it was —
// including one that ends in `;`, which tmux would otherwise take for the end
// of the command — with the commands after it still run.
func TestNamesTmuxWouldMisreadArriveWhole(t *testing.T) {
	tmux := realTmux(t)
	socket := aServer(t, tmux, "an-test")
	server := pidOf(t, tmux, socket)

	names := []string{"fix it;", ";", `a\;`, "-u odd", "x;y", "#{session_name}", "last"}
	var records []session.Record
	panes := map[string]string{}
	for _, name := range names {
		pane := must(t, tmux, socket, "new-window", "-d", "-P", "-F", "#{pane_id}", "-t", "an-test", "sleep", "600")
		records = append(records, placedIn(name, session.Idle, socket, server, pane))
		panes[pane] = name
	}
	render(t, tmux, records...)

	marks := marksOf(t, tmux, socket)
	for pane, name := range panes {
		if marks[pane].Name != name {
			t.Errorf("pane %s reads back %q, want %q", pane, marks[pane].Name, name)
		}
	}
}

// TestOneFailedCommandStopsTheRest is the fact paint's second attempt is for.
func TestOneFailedCommandStopsTheRest(t *testing.T) {
	tmux := realTmux(t)
	socket := aServer(t, tmux, "an-test")
	pane := paneOf(t, tmux, socket, "an-test")

	err := tmux.Do(socket, chain([]Command{
		{Args: []string{"set-option", "-p", "-t", "%999", nameOption, "gone"}},
		{Args: []string{"set-option", "-p", "-t", pane, nameOption, "here"}},
	}))
	if err == nil {
		t.Fatal("a command naming a pane that is not there succeeded")
	}
	if got := marksOf(t, tmux, socket)[pane].Name; got != "" {
		t.Errorf("the command after the failed one ran: pane %s says %q", pane, got)
	}
}

// TestAPaneThatClosesMidwayIsLookedAtAgain: a pane closes between the look
// and the write — here, by a tmux that closes it the moment the first write
// arrives — so the write is refused, and the marks still arrive.
func TestAPaneThatClosesMidwayIsLookedAtAgain(t *testing.T) {
	tmux := realTmux(t)
	socket := aServer(t, tmux, "an-test")
	leftover := paneOf(t, tmux, socket, "an-test")
	agent := must(t, tmux, socket, "split-window", "-P", "-F", "#{pane_id}", "-t", leftover, "sleep", "600")
	// A mark nobody claims on the lower-numbered pane, so that giving it back
	// is the first command of the write.
	must(t, tmux, socket, "set-option", "-p", "-t", leftover, nameOption, "old")

	directory := t.TempDir()
	marker := filepath.Join(directory, "closed")
	script := filepath.Join(directory, "tmux")
	body := "#!/bin/sh\n" +
		"case \" $* \" in *\" set-option \"*)\n" +
		"  if [ ! -e '" + marker + "' ]; then : > '" + marker + "'; '" + tmux.Binary + "' -S '" + socket + "' kill-pane -t " + leftover + "; fi;;\n" +
		"esac\n" +
		"exec '" + tmux.Binary + "' \"$@\"\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	closing := Tmux{Binary: script, Timeout: 5 * time.Second}
	render(t, closing, placedIn("alpha", session.Working, socket, pidOf(t, tmux, socket), agent))

	if _, err := os.Stat(marker); err != nil {
		t.Fatal("the pane was never closed under the write, so this tested nothing")
	}
	if want := (Mark{State: "working", Glyph: "*", Name: "alpha"}); marksOf(t, tmux, socket)[agent] != want {
		t.Errorf("pane %s says %+v, want %+v", agent, marksOf(t, tmux, socket)[agent], want)
	}
}

// TestAServerStartedAgainIsAnotherServer: tmux numbers panes from %0 in every
// server it starts, so a pane id outlives the server it was handed out by.
// A record of the one before marks nothing in the one after.
func TestAServerStartedAgainIsAnotherServer(t *testing.T) {
	tmux := realTmux(t)
	socket := aServer(t, tmux, "an-test")
	before := pidOf(t, tmux, socket)
	pane := paneOf(t, tmux, socket, "an-test")
	must(t, tmux, socket, "kill-server")
	start(t, tmux, socket, "an-test")
	if again := paneOf(t, tmux, socket, "an-test"); again != pane {
		t.Skipf("this tmux numbered the first pane %s and then %s; the case does not arise", pane, again)
	}

	render(t, tmux, placedIn("alpha", session.Working, socket, before, pane))
	if got := marksOf(t, tmux, socket)[pane]; got != (Mark{}) {
		t.Errorf("pane %s of the new server was marked %+v for an agent of the old one", pane, got)
	}

	coordinates, _ := json.Marshal(Place{Socket: socket, Server: before, Pane: pane})
	if outcome, _ := Focus(tmux, coordinates); outcome.OK || outcome.Problem != container.PlaceIsGone {
		t.Errorf("focus on the old server's pane = %+v, want place-is-gone", outcome)
	}
}
