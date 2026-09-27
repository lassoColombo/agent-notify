package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lassoColombo/agent-notify/session"
	"github.com/lassoColombo/agent-notify/subscribe"
)

// These tests run against a real zellij. That is the point: everything this
// integration knows about zellij is a fact about a program, and a fake zellij
// would only ever confirm what this file already believes.
//
// They are cheap — a detached session costs about a second and about 15ms per
// call — and they skip themselves where there is no zellij to talk to.

func realZellij(t *testing.T) Zellij {
	t.Helper()
	// A test runs in a shell, which is the one context where looking zellij up
	// is the right thing to do — and exactly why the program itself no longer
	// does it (D-67): the session-watcher that starts it is not in one.
	binary, err := exec.LookPath("zellij")
	if err != nil {
		t.Skipf("no zellij here: %v", err)
	}
	return Zellij{Binary: binary, Timeout: 5 * time.Second}
}

// zellijSession brings up a detached zellij with two panes in it and returns their
// ids, having taken them out of whatever layout the machine's default produced.
func zellijSession(t *testing.T, zellij Zellij) (string, int, int) {
	t.Helper()
	name := fmt.Sprintf("an-test-%d", os.Getpid())

	// Never inherit this process's own zellij: a test that quietly renamed the
	// developer's panes instead of its own would be a very unpleasant surprise.
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

	// It takes a moment to be there, and asking is more honest than sleeping.
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

	first := newPane(t, bare, name, "first")
	second := newPane(t, bare, name, "second")
	return name, first, second
}

func newPane(t *testing.T, bare func(...string) (string, error), name, title string) int {
	t.Helper()
	out, err := bare("--session", name, "run", "--name", title, "--", "sleep", "600")
	if err != nil {
		t.Fatalf("zellij run: %v: %s", err, out)
	}
	id, found := strings.CutPrefix(out, "terminal_")
	if !found {
		t.Fatalf("zellij run answered %q, not a terminal pane id", out)
	}
	number, err := strconv.Atoi(id)
	if err != nil {
		t.Fatalf("zellij run answered %q: %v", out, err)
	}
	return number
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

func titles(t *testing.T, zellij Zellij, name string) (map[int]string, map[int]string) {
	t.Helper()
	panes, err := zellij.Panes(name)
	if err != nil {
		t.Fatalf("Panes: %v", err)
	}
	pane, tab := map[int]string{}, map[int]string{}
	for _, one := range panes {
		if one.Plugin {
			continue
		}
		pane[one.ID] = one.Title
		tab[one.TabID] = one.TabName
	}
	return pane, tab
}

func tabOf(t *testing.T, zellij Zellij, name string, id int) int {
	t.Helper()
	panes, err := zellij.Panes(name)
	if err != nil {
		t.Fatalf("Panes: %v", err)
	}
	for _, one := range panes {
		if !one.Plugin && one.ID == id {
			return one.TabID
		}
	}
	t.Fatalf("pane %d is not in session %q", id, name)
	return 0
}

// TestTheParseDecidesNotTheExitCode pins the rule the whole read-before-write
// shape rests on, with no zellij involved at all: /bin/echo is a program that
// exits zero and does not answer with JSON, which is precisely the failure
// being guarded against.
func TestTheParseDecidesNotTheExitCode(t *testing.T) {
	liar := Zellij{Binary: "/bin/echo", Timeout: 5 * time.Second}
	if _, err := liar.Panes("home"); err == nil {
		t.Fatal("a program that exits 0 and answers with prose was believed")
	} else if !strings.Contains(err.Error(), "is not there") {
		t.Errorf("the error does not say what happened: %v", err)
	}
}

// TestAMissingSessionFails is the same rule against the real thing, and the
// reason it asserts nothing about the exit code is worth writing down.
//
// Measured on zellij 0.45.1, asking a session that does not exist:
//
//	another detached session is alive   exit 0, and the list of live sessions
//	                                    written where the JSON should be
//	no other session is alive           exit 1
//
// The status depends on the state of sessions that have nothing to do with the
// question. An exit code that is a function of unrelated state is not a signal,
// which is why the parse decides and why a read that fails means this zellij
// session is skipped entirely rather than written to.
func TestAMissingSessionFails(t *testing.T) {
	zellij := realZellij(t)
	const absent = "an-no-such"

	if _, err := zellij.Panes(absent); err == nil {
		t.Fatal("asking a session that is not there succeeded")
	}

	command := exec.Command(zellij.Binary, "--session", absent, "action", "list-panes", "--json")
	command.Env = stripped(os.Environ())
	t.Logf("this zellij answered a missing session with: %v", command.Run())
}

func TestPaintingARealZellij(t *testing.T) {
	zellij := realZellij(t)
	name, first, second := zellijSession(t, zellij)
	tab := tabOf(t, zellij, name, first)

	_, before := titles(t, zellij, name)
	original := before[tab]

	display := &Display{Zellij: zellij, Glyphs: testGlyphs,
		Logger: slog.New(slog.DiscardHandler)}

	working := placed("alpha", session.Working, name, first)
	blocked := placed("beta", session.BlockedOnYou, name, second)

	if err := display.Render(subscribe.View{Sessions: []session.Record{working, blocked}}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	panes, tabs := titles(t, zellij, name)
	if panes[first] != "* alpha" {
		t.Errorf("pane %d says %q, want %q", first, panes[first], "* alpha")
	}
	if panes[second] != "! beta" {
		t.Errorf("pane %d says %q, want %q", second, panes[second], "! beta")
	}
	if want := "! " + original; tabs[tab] != want {
		t.Errorf("tab %d says %q, want %q — the most urgent agent in it", tab, tabs[tab], want)
	}

	// The blocked one is answered and goes back to work. The tab follows the
	// aggregate down, which is the half of max(rank) that is easy to get wrong.
	answered := placed("beta", session.Working, name, second)
	if err := display.Render(subscribe.View{Sessions: []session.Record{working, answered}}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	_, tabs = titles(t, zellij, name)
	if want := "* " + original; tabs[tab] != want {
		t.Errorf("tab %d says %q, want %q", tab, tabs[tab], want)
	}

	// One ends. Its pane is handed back — to zellij's own title, not to a
	// leftover of ours — and the tab keeps the other one's glyph.
	ended := placed("beta", session.Ended, name, second)
	if err := display.Render(subscribe.View{Sessions: []session.Record{working, ended}}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	panes, tabs = titles(t, zellij, name)
	if panes[second] == "! beta" || panes[second] == "* beta" {
		t.Errorf("pane %d still says %q after its session ended", second, panes[second])
	}
	if want := "* " + original; tabs[tab] != want {
		t.Errorf("tab %d says %q, want %q", tab, tabs[tab], want)
	}

	// Both end: the tab is the user's again, exactly as it was.
	done := placed("alpha", session.Ended, name, first)
	if err := display.Render(subscribe.View{Sessions: []session.Record{done, ended}}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	_, tabs = titles(t, zellij, name)
	if tabs[tab] != original {
		t.Errorf("tab %d says %q, want the name it had before any of this: %q",
			tab, tabs[tab], original)
	}
}

// TestRenderingTwiceRunsNothingTheSecondTime is the property that lets this be
// called on every change: a render is the difference, not the state.
func TestRenderingTwiceRunsNothingTheSecondTime(t *testing.T) {
	zellij := realZellij(t)
	name, first, _ := zellijSession(t, zellij)

	records := []session.Record{placed("alpha", session.Working, name, first)}
	display := &Display{Zellij: zellij, Glyphs: testGlyphs, Logger: slog.New(slog.DiscardHandler)}
	if err := display.Render(subscribe.View{Sessions: records}); err != nil {
		t.Fatalf("Render: %v", err)
	}

	panes, err := zellij.Panes(name)
	if err != nil {
		t.Fatalf("Panes: %v", err)
	}
	if plan := Plan(name, records, panes, testGlyphs); len(plan) != 0 {
		t.Errorf("a second render would run %v", ran(plan))
	}
}

// TestPanesCarryLiveStateGlyphs is M10's "done when", with the agent replaced
// by a script and nothing else replaced at all: a real zellij, real panes, the
// real fan-out, the real subscriber lifecycle, the real render function.
func TestPanesCarryLiveStateGlyphs(t *testing.T) {
	zellij := realZellij(t)
	name, first, second := zellijSession(t, zellij)

	root, err := os.MkdirTemp("/tmp", "an-zellij")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })

	fake, err := subscribe.StartFake(root, nil)
	if err != nil {
		t.Fatalf("StartFake: %v", err)
	}
	defer fake.Stop()

	display := &Display{Zellij: zellij, Glyphs: testGlyphs, Logger: slog.New(slog.DiscardHandler)}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	// This is the ten lines an author writes.
	go subscribe.Run(ctx, subscribe.Integration{
		Name: Name, Roles: []string{"display"}, WantEnded: true, Root: root,
		OnChange: display.Render,
	})
	waitUntil(t, "the display connects", func() bool { return len(fake.Connected()) == 1 })

	fake.Publish(placed("alpha", session.Working, name, first))
	waitUntil(t, "the pane to say it is working", func() bool {
		panes, _ := titles(t, zellij, name)
		return panes[first] == "* alpha"
	})

	fake.Publish(placed("alpha", session.BlockedOnYou, name, first))
	waitUntil(t, "the pane to say it is blocked", func() bool {
		panes, _ := titles(t, zellij, name)
		return panes[first] == "! alpha"
	})

	fake.Publish(placed("beta", session.FinishedATurn, name, second))
	waitUntil(t, "the second pane", func() bool {
		panes, _ := titles(t, zellij, name)
		return panes[second] == "> beta"
	})

	fake.Publish(placed("alpha", session.Ended, name, first))
	waitUntil(t, "the first pane to be handed back", func() bool {
		panes, _ := titles(t, zellij, name)
		return panes[first] != "! alpha"
	})
}

func waitUntil(t *testing.T, what string, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("waited for %s and it never happened", what)
}
