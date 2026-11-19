package main

import (
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
func aServer(t *testing.T, tmux Tmux, session string) string {
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
	if out, err := bare(tmux, socket, "new-session", "-d", "-s", session, "-x", "120", "-y", "40",
		"sleep", "600"); err != nil {
		t.Skipf("cannot start a tmux server (%v): %s", err, out)
	}
	return socket
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
		if strings.HasPrefix(variable, "TMUX") {
			continue
		}
		kept = append(kept, variable)
	}
	return kept
}

// twoPanes is a server with one window split in two, and the two panes' ids.
func twoPanes(t *testing.T, tmux Tmux) (string, string, string) {
	t.Helper()
	socket := aServer(t, tmux, "an-test")
	first := must(t, tmux, socket, "display-message", "-p", "-t", "an-test", "#{pane_id}")
	second := must(t, tmux, socket, "split-window", "-P", "-F", "#{pane_id}", "-t", "an-test", "sleep", "600")
	return socket, first, second
}

func marks(t *testing.T, tmux Tmux, socket string) (map[string]string, map[string]string) {
	t.Helper()
	rows, err := tmux.Panes(socket)
	if err != nil {
		t.Fatalf("Panes: %v", err)
	}
	panes, windows := map[string]string{}, map[string]string{}
	for _, row := range rows {
		panes[row.Pane] = row.PaneMark
		windows[row.WindowID] = row.WindowMark
	}
	return panes, windows
}

// TestTheParseDecidesNotTheExitCode: /bin/echo is a program that exits zero
// and does not answer in the format it was asked for, which is precisely the
// failure being guarded against.
func TestTheParseDecidesNotTheExitCode(t *testing.T) {
	liar := Tmux{Binary: "/bin/echo", Timeout: 5 * time.Second}
	if _, err := liar.Panes(home); err == nil {
		t.Fatal("a program that exits 0 and answers with prose was believed")
	} else if !strings.Contains(err.Error(), "is not there") {
		t.Errorf("the error does not say what happened: %v", err)
	}
}

// TestAMissingServerFails: a socket nobody is listening on is a server that is
// not there, and asking it starts nothing — `list-panes` never starts a server.
func TestAMissingServerFails(t *testing.T) {
	tmux := realTmux(t)
	directory, err := os.MkdirTemp("", "an-tmux-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(directory)
	socket := filepath.Join(directory, "s")

	if _, err := tmux.Panes(socket); err == nil {
		t.Fatal("asking a server that is not there succeeded")
	}
	if _, err := os.Stat(socket); err == nil {
		t.Error("asking a server that is not there started one")
	}
}

// TestReadingWithNoLocale: the session-watcher runs with no LANG, and a tmux
// that does not think its output is UTF-8 prints the separator and every glyph
// as `_`. Every read then failed to parse, so nothing was ever placed, painted
// or focused, while the suite, run from a UTF-8 shell, passed.
func TestReadingWithNoLocale(t *testing.T) {
	tmux := realTmux(t)
	socket, first, _ := twoPanes(t, tmux)
	for _, variable := range []string{"LANG", "LC_ALL", "LC_CTYPE"} {
		t.Setenv(variable, "")
	}

	rows, err := tmux.Panes(socket)
	if err != nil {
		t.Fatalf("with no locale, Panes: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("with no locale, no rows")
	}

	display := &Display{Tmux: tmux, Glyphs: session.NewPalette(DefaultGlyphs, nil), Logger: slog.New(slog.DiscardHandler)}
	if err := display.Render(session.View{Sessions: []session.Record{placed("alpha", session.Working, socket, first)}}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	want := DefaultGlyphs[session.Working] + " alpha"
	if panes, _ := marks(t, tmux, socket); panes[first] != want {
		t.Errorf("with no locale, pane %s reads back %q, want %q", first, panes[first], want)
	}
}

func TestPaintingARealTmux(t *testing.T) {
	tmux := realTmux(t)
	socket, first, second := twoPanes(t, tmux)
	window := must(t, tmux, socket, "display-message", "-p", "-t", first, "#{window_id}")

	display := &Display{Tmux: tmux, Glyphs: testGlyphs, Logger: slog.New(slog.DiscardHandler)}
	paint := func(sessions ...session.Record) {
		t.Helper()
		if err := display.Render(session.View{Sessions: sessions}); err != nil {
			t.Fatalf("Render: %v", err)
		}
	}

	working := placed("alpha", session.Working, socket, first)
	paint(working, placed("beta", session.BlockedOnYou, socket, second))
	panes, windows := marks(t, tmux, socket)
	if panes[first] != "* alpha" {
		t.Errorf("pane %s says %q, want %q", first, panes[first], "* alpha")
	}
	if panes[second] != "! beta" {
		t.Errorf("pane %s says %q, want %q", second, panes[second], "! beta")
	}
	if windows[window] != "!" {
		t.Errorf("window %s says %q, want the most urgent agent in it", window, windows[window])
	}

	// The blocked one is answered and goes back to work. The window follows
	// the aggregate down, which is the half of max(rank) that is easy to get
	// wrong.
	paint(working, placed("beta", session.Working, socket, second))
	if _, windows = marks(t, tmux, socket); windows[window] != "*" {
		t.Errorf("window %s says %q, want %q", window, windows[window], "*")
	}

	// One ends: its pane is given back, the window keeps the other's glyph.
	ended := placed("beta", session.Ended, socket, second)
	paint(working, ended)
	panes, windows = marks(t, tmux, socket)
	if panes[second] != "" {
		t.Errorf("pane %s still says %q after its session ended", second, panes[second])
	}
	if windows[window] != "*" {
		t.Errorf("window %s says %q, want %q", window, windows[window], "*")
	}

	// Both end, and nothing of ours is left anywhere.
	paint(placed("alpha", session.Ended, socket, first), ended)
	panes, windows = marks(t, tmux, socket)
	if panes[first] != "" || windows[window] != "" {
		t.Errorf("after everything ended, pane %s says %q and window %s says %q",
			first, panes[first], window, windows[window])
	}

	// And the window's own name was never touched: automatic-rename is the
	// thing renaming it would have switched off.
	if automatic := must(t, tmux, socket, "show-options", "-w", "-v", "-t", window, "automatic-rename"); automatic == "off" {
		t.Errorf("window %s lost automatic-rename", window)
	}
}

// TestRenderingTwiceRunsNothingTheSecondTime is the property that lets this be
// called on every change: a render is the difference, not the state.
func TestRenderingTwiceRunsNothingTheSecondTime(t *testing.T) {
	tmux := realTmux(t)
	socket, first, _ := twoPanes(t, tmux)

	records := []session.Record{placed("alpha", session.Working, socket, first)}
	display := &Display{Tmux: tmux, Glyphs: testGlyphs, Logger: slog.New(slog.DiscardHandler)}
	if err := display.Render(session.View{Sessions: records}); err != nil {
		t.Fatalf("Render: %v", err)
	}

	rows, err := tmux.Panes(socket)
	if err != nil {
		t.Fatalf("Panes: %v", err)
	}
	if plan := Plan(socket, records, rows, testGlyphs); len(plan) != 0 {
		t.Errorf("a second render would run %v", ran(plan))
	}
}

// TestAGlyphThatLooksLikeAFlagIsAValue: the option's value comes after its
// name, so tmux never reads it as a flag — pinned because a name is the
// agent's, and the agent can call a session anything.
func TestAGlyphThatLooksLikeAFlagIsAValue(t *testing.T) {
	tmux := realTmux(t)
	socket, first, _ := twoPanes(t, tmux)

	display := &Display{Tmux: tmux, Glyphs: testGlyphs, Logger: slog.New(slog.DiscardHandler)}
	if err := display.Render(session.View{Sessions: []session.Record{
		placed("-u odd #{name}", session.Idle, socket, first),
	}}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if panes, _ := marks(t, tmux, socket); panes[first] != "-u odd #{name}" {
		t.Errorf("pane %s says %q", first, panes[first])
	}
}
