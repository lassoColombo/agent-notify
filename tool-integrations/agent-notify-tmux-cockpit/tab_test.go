package main

import (
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// These run against a real tmux, for the reason agent-notify-tmux's do:
// everything this program knows about tmux is a fact about a program — that a
// swap keeps both layouts, that an agent outlives the window it was borrowed
// from — and a fake tmux would only ever confirm what this file already
// believes.
//
// Each test gets a server of its own on a socket of its own, started with no
// configuration file, so nothing here can touch, or be shaped by, the
// developer's own tmux. They skip themselves where there is no tmux.

var quiet = slog.New(slog.DiscardHandler)

// idle is what every pane of a test runs, the tab's list included: a test is
// about where panes are, not about what is in them.
var idle = []string{"sleep", "600"}

// aServer starts a private server and returns it. The directory is made by
// hand rather than by t.TempDir, because a unix socket path is limited to
// about a hundred bytes and t.TempDir's are longer than that on macOS.
func aServer(t *testing.T) Tmux {
	t.Helper()
	binary, err := exec.LookPath("tmux")
	if err != nil {
		t.Skipf("no tmux here: %v", err)
	}
	directory, err := os.MkdirTemp("", "an-tab-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	tmux := Tmux{Binary: binary, Socket: filepath.Join(directory, "s")}
	t.Cleanup(func() {
		_, _ = bare(tmux, "kill-server")
		_ = os.RemoveAll(directory)
	})
	if out, err := bare(tmux, "new-session", "-d", "-s", "home", "-x", "160", "-y", "40", "sleep", "600"); err != nil {
		t.Skipf("cannot start a tmux server (%v): %s", err, out)
	}
	return tmux
}

// bare runs tmux the way a person would set a server up: with no
// configuration, and without this process's own $TMUX, so that a suite run
// from inside tmux never reaches the developer's server by accident.
func bare(tmux Tmux, arguments ...string) (string, error) {
	command := exec.Command(tmux.Binary, append([]string{"-u", "-f", "/dev/null", "-S", tmux.Socket}, arguments...)...)
	for _, variable := range os.Environ() {
		if !strings.HasPrefix(variable, "TMUX") {
			command.Env = append(command.Env, variable)
		}
	}
	out, err := command.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func must(t *testing.T, tmux Tmux, arguments ...string) string {
	t.Helper()
	out, err := bare(tmux, arguments...)
	if err != nil {
		t.Fatalf("tmux %s: %v: %s", strings.Join(arguments, " "), err, out)
	}
	return out
}

// viewer attaches a real client to a session and returns its name: a pane of
// a second server running `tmux attach` to the first is an attached client
// like any other.
func viewer(t *testing.T, tmux Tmux, session string) string {
	t.Helper()
	before := len(strings.Fields(must(t, tmux, "list-clients", "-F", "#{client_name}")))
	directory, err := os.MkdirTemp("", "an-tab-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	host := Tmux{Binary: tmux.Binary, Socket: filepath.Join(directory, "h")}
	t.Cleanup(func() {
		_, _ = bare(host, "kill-server")
		_ = os.RemoveAll(directory)
	})
	must(t, host, "new-session", "-d", "-x", "160", "-y", "40",
		"env -u TMUX "+tmux.Binary+" -u -f /dev/null -S "+tmux.Socket+" attach -t '="+session+"'")
	deadline := time.Now().Add(15 * time.Second)
	for {
		// newest first, so the one just attached
		names, err := bare(tmux, "list-clients", "-O", "creation", "-r", "-F", "#{client_name}")
		if clients := strings.Fields(names); err == nil && len(clients) > before {
			return clients[0]
		}
		if time.Now().After(deadline) {
			t.Fatal("nothing ever attached to the server")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// scene is a server somebody is looking at, with the tab open in front of
// them: session home holds a window of two panes, the first of them agent a,
// and a window that is all agent b.
type scene struct {
	tmux   Tmux
	tab    Tab
	client string
	a, b   string
}

func aScene(t *testing.T) scene {
	t.Helper()
	tmux := aServer(t)
	s := scene{tmux: tmux}
	s.a = must(t, tmux, "display-message", "-p", "-t", "home", "#{pane_id}")
	must(t, tmux, "split-window", "-h", "-d", "-t", s.a, "sleep", "600")
	s.b = must(t, tmux, "new-window", "-d", "-t", "home:", "-P", "-F", "#{pane_id}", "sleep", "600")
	s.client = viewer(t, tmux, "home")
	if err := Open(tmux, s.client, idle); err != nil {
		t.Fatalf("Open: %v", err)
	}
	list, found := s.see(t).List()
	if !found {
		t.Fatal("the tab was opened and has no list")
	}
	s.tab = Tab{Tmux: tmux, List: list.ID, ListWidth: 30, Logger: quiet}
	return s
}

func (s scene) see(t *testing.T) World {
	t.Helper()
	world, err := s.tmux.Observe()
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	return world
}

// show asks for a pane on the stage and returns the server as it then is.
func (s scene) show(t *testing.T, desire Desire) World {
	t.Helper()
	if _, err := s.tab.Reconcile(desire); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	return s.see(t)
}

// shape is a window as geometry and focus, whichever panes fill it: one line
// per cell, its place, its size and whether it is the active one.
func (s scene) shape(t *testing.T, window string) string {
	t.Helper()
	cells := strings.Split(must(t, s.tmux, "list-panes", "-t", window, "-F",
		"#{pane_left},#{pane_top} #{pane_width}x#{pane_height} active=#{pane_active} zoomed=#{window_zoomed_flag}"), "\n")
	slices.Sort(cells)
	return strings.Join(cells, "\n")
}

func (s scene) tabWindow(t *testing.T) string {
	t.Helper()
	return s.see(t).Panes[s.tab.List].Window
}

func TestOpeningMakesTheTabAndPutsItInFront(t *testing.T) {
	s := aScene(t)
	world := s.see(t)
	if !world.Visible(s.tab.List) {
		t.Fatal("the tab was opened and nobody is looking at it")
	}
	if got := world.Where(s.tab.List); !strings.HasPrefix(got, "home:") {
		t.Errorf("the tab is at %q, and was to be linked into the session it was asked from", got)
	}
	if must(t, s.tmux, "display-message", "-p", "-t", s.tab.List, "#{"+tabOption+"}") != "1" {
		t.Errorf("the tab's window does not say it is the tab")
	}
}

// TestOpeningWhereUnattachedSessionsAreDestroyed: the tab lives in a session
// nobody attaches to, which a server set to destroy those would destroy the
// moment it was made.
func TestOpeningWhereUnattachedSessionsAreDestroyed(t *testing.T) {
	tmux := aServer(t)
	client := viewer(t, tmux, "home")
	// After somebody is attached: set first, it destroys the only session.
	must(t, tmux, "set-option", "-g", "destroy-unattached", "on")
	if err := Open(tmux, client, idle); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if out, err := bare(tmux, "has-session", "-t", "="+tabSession); err != nil {
		t.Errorf("the tab's session is gone: %s", out)
	}
}

// TestOpeningOnTheTabGoesBack, and opening again comes back to the same tab
// rather than making a second.
func TestOpeningOnTheTabGoesBack(t *testing.T) {
	s := aScene(t)
	before := s.tabWindow(t)
	if err := Open(s.tmux, s.client, idle); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if s.see(t).Visible(s.tab.List) {
		t.Fatal("the key pressed on the tab did not leave it")
	}
	if err := Open(s.tmux, s.client, idle); err != nil {
		t.Fatalf("Open: %v", err)
	}
	world := s.see(t)
	lists := 0
	for _, pane := range world.Panes {
		if pane.Role == roleList {
			lists++
		}
	}
	if lists != 1 || !world.Visible(s.tab.List) || s.tabWindow(t) != before {
		t.Errorf("%d lists, visible = %v", lists, world.Visible(s.tab.List))
	}
}

// TestTheWindowAnAgentLivesInKeepsItsShape is the reason the stage swaps
// rather than moves: with a stand-in where the agent was, the window is the
// same cells, the same sizes and the same active pane, before, during and
// after.
func TestTheWindowAnAgentLivesInKeepsItsShape(t *testing.T) {
	s := aScene(t)
	home := must(t, s.tmux, "display-message", "-p", "-t", s.a, "#{window_id}")
	before := s.shape(t, home)

	world := s.show(t, Desire{Staged: s.a})
	if world.Panes[s.a].Window != s.tabWindow(t) {
		t.Fatalf("%s was asked onto the stage and is in %s", s.a, world.Panes[s.a].Window)
	}
	if got := world.Panes[s.a]; !world.holding(got.StandIn) || world.Panes[got.StandIn].Window != home {
		t.Errorf("the borrowed pane says %+v", got)
	}
	if during := s.shape(t, home); during != before {
		t.Errorf("the window changed shape while its agent was away:\n%s\nwas\n%s", during, before)
	}
	if got := world.Where(s.a); got != "home:0" {
		t.Errorf("a borrowed pane is said to be at %q", got)
	}

	world = s.show(t, Desire{})
	if world.Panes[s.a].Window != home || len(world.Borrowed()) != 0 {
		t.Fatalf("%s was not given back: %+v", s.a, world.Panes[s.a])
	}
	if after := s.shape(t, home); after != before {
		t.Errorf("the window changed shape:\n%s\nwas\n%s", after, before)
	}
}

func TestAZoomedWindowStaysZoomed(t *testing.T) {
	s := aScene(t)
	home := must(t, s.tmux, "display-message", "-p", "-t", s.a, "#{window_id}")
	must(t, s.tmux, "resize-pane", "-Z", "-t", s.a)
	before := s.shape(t, home)
	s.show(t, Desire{Staged: s.a})
	if during := s.shape(t, home); during != before {
		t.Errorf("borrowing its zoomed pane changed the window:\n%s\nwas\n%s", during, before)
	}
	s.show(t, Desire{})
	if after := s.shape(t, home); after != before {
		t.Errorf("giving it back changed the window:\n%s\nwas\n%s", after, before)
	}
}

func TestMovingTheSelectionSendsOneHomeAndBringsTheNext(t *testing.T) {
	s := aScene(t)
	s.show(t, Desire{Staged: s.a})
	world := s.show(t, Desire{Staged: s.b})
	tab := s.tabWindow(t)
	if world.Panes[s.a].Window == tab || world.Panes[s.b].Window != tab || len(world.Borrowed()) != 1 {
		t.Errorf("a is in %s and b in %s, with the tab %s", world.Panes[s.a].Window, world.Panes[s.b].Window, tab)
	}
}

// TestNobodyLookingSendsEverythingHome: the tab is left for another window,
// and the next look gives the agent back without being asked to.
func TestNobodyLookingSendsEverythingHome(t *testing.T) {
	s := aScene(t)
	s.show(t, Desire{Staged: s.b})
	must(t, s.tmux, "select-window", "-t", "home:0")
	if world := s.show(t, Desire{Staged: s.b}); len(world.Borrowed()) != 0 {
		t.Errorf("nobody is looking at the tab and %v is still away", world.Borrowed())
	}
}

// TestAnAgentWhoseWindowWasKilledIsNotLost: its window goes while it is on the
// stage, and the stand-in with it. The agent stays, and when it leaves the
// stage it has a window to go to.
func TestAnAgentWhoseWindowWasKilledIsNotLost(t *testing.T) {
	s := aScene(t)
	world := s.show(t, Desire{Staged: s.b})
	must(t, s.tmux, "kill-pane", "-t", world.Panes[s.b].StandIn)

	world = s.show(t, Desire{Staged: s.b})
	if pane, alive := world.Panes[s.b]; !alive || pane.Window != s.tabWindow(t) || !world.holding(pane.StandIn) {
		t.Fatalf("the agent is not back on the stage with a place to return to: %+v (alive = %v)", pane, alive)
	}
	world = s.show(t, Desire{})
	if pane, alive := world.Panes[s.b]; !alive || pane.Window == s.tabWindow(t) || pane.StandIn != "" {
		t.Errorf("the agent did not get a window of its own: %+v (alive = %v)", pane, alive)
	}
	if got := world.Where(s.b); !strings.HasPrefix(got, "home:") {
		t.Errorf("it went to %q, and came from session home", got)
	}
}

// TestAnAgentThatClosesOnTheStageLeavesTheTabWhole: the stage's cell goes with
// it, and the stand-in comes back from the place it was holding.
func TestAnAgentThatClosesOnTheStageLeavesTheTabWhole(t *testing.T) {
	s := aScene(t)
	s.show(t, Desire{Staged: s.b})
	must(t, s.tmux, "kill-pane", "-t", s.b)

	world := s.show(t, Desire{Staged: s.b})
	stage, found := world.Stage(s.tab.List)
	if !found || world.Panes[stage].Role != roleStandIn {
		t.Fatalf("the stage holds %q (found = %v), and should hold the stand-in", stage, found)
	}
	if width := must(t, s.tmux, "display-message", "-p", "-t", s.tab.List, "#{pane_width}"); width != "30" {
		t.Errorf("the list is %s columns wide, and is given 30", width)
	}
}

// TestAWindowWhoseAgentClosedOnTheStageGetsItsNameBack: the hold on the
// window's naming was written on the agent's pane, which is gone, and on the
// stand-in, which comes back from the window and gives the name back as it
// leaves.
func TestAWindowWhoseAgentClosedOnTheStageGetsItsNameBack(t *testing.T) {
	s := aScene(t)
	home := must(t, s.tmux, "display-message", "-p", "-t", s.a, "#{window_id}")
	s.show(t, Desire{Staged: s.a})
	if got := must(t, s.tmux, "show-options", "-w", "-t", home, "automatic-rename"); got != "automatic-rename off" {
		t.Fatalf("while its agent is away the window has %q", got)
	}
	must(t, s.tmux, "kill-pane", "-t", s.a)

	world := s.show(t, Desire{Staged: s.a})
	if stage, found := world.Stage(s.tab.List); !found || world.Panes[stage].Role != roleStandIn ||
		world.Panes[stage].Renamed != "" {
		t.Fatalf("the stage holds %+v", world.Panes[stage])
	}
	if got := must(t, s.tmux, "show-options", "-w", "-t", home, "automatic-rename"); got != "" {
		t.Errorf("the window its agent closed in was left with %q", got)
	}
}

// TestGivingBackTwiceIsGivingBackOnce: the list, a stand-in left alone and
// `restore` may all send one pane home, and a swap done twice is a swap
// undone.
func TestGivingBackTwiceIsGivingBackOnce(t *testing.T) {
	s := aScene(t)
	world := s.show(t, Desire{Staged: s.a})
	home := world.Panes[world.Panes[s.a].StandIn].Window
	step := giveBack(world.Panes[s.a])
	for range 2 {
		if _, err := s.tmux.run(step...); err != nil {
			t.Fatalf("giving back: %v", err)
		}
	}
	if got := s.see(t).Panes[s.a].Window; got != home {
		t.Errorf("%s is in %s after being given back twice, and lives in %s", s.a, got, home)
	}
}

// TestTheStandInIsAnEmptyPane: no program runs in it, and what the list has
// it say is written into it.
func TestTheStandInIsAnEmptyPane(t *testing.T) {
	s := aScene(t)
	stage, found := s.show(t, Desire{}).Stage(s.tab.List)
	if !found {
		t.Fatal("the tab has no stage")
	}
	if pid := must(t, s.tmux, "display-message", "-p", "-t", stage, "#{pane_pid}"); pid != "" {
		t.Errorf("the stand-in runs a program, pid %s", pid)
	}
	if err := s.tmux.paint(stage, Frame(Showing{Title: "alpha", Lines: []string{"alpha is on screen in home:0"}}, 80, 10)); err != nil {
		t.Fatalf("paint: %v", err)
	}
	if got := must(t, s.tmux, "capture-pane", "-p", "-t", stage); !strings.Contains(got, "alpha is on screen in home:0") {
		t.Errorf("the stand-in shows %q", got)
	}
	if got := must(t, s.tmux, "display-message", "-p", "-t", stage, "#{pane_title} #{cursor_flag}"); got != "alpha 0" {
		t.Errorf("its title and cursor are %q", got)
	}
}

// TestLeavingTheTabSendsTheAgentHomeWithNobodyAsked is the rule everything
// leans on, kept by the server: the hooks the borrow left behind give the
// pane back the moment the tab is left, by whatever way, and nothing of this
// program runs in between.
func TestLeavingTheTabSendsTheAgentHomeWithNobodyAsked(t *testing.T) {
	for way, leave := range map[string]func(s scene){
		"another window":  func(s scene) { must(t, s.tmux, "select-window", "-t", "home:0") },
		"another session": func(s scene) { must(t, s.tmux, "switch-client", "-c", s.client, "-t", "=work") },
		"detaching":       func(s scene) { must(t, s.tmux, "detach-client", "-t", s.client) },
		// two windows trade places and the terminal stays where it was,
		// which is now the other one
		"the tab moved from under the terminal": func(s scene) {
			must(t, s.tmux, "swap-window", "-s", "home:"+s.tabWindow(t), "-t", "home:0")
		},
	} {
		t.Run(way, func(t *testing.T) {
			s := aScene(t)
			must(t, s.tmux, "new-session", "-d", "-s", "work", "sleep", "600")
			home := must(t, s.tmux, "display-message", "-p", "-t", s.b, "#{window_id}")
			if world := s.show(t, Desire{Staged: s.b}); world.Panes[s.b].Window != s.tabWindow(t) {
				t.Fatalf("%s did not reach the stage", s.b)
			}
			leave(s)
			world := s.see(t)
			if world.Visible(s.tab.List) {
				t.Fatal("the tab is still on screen")
			}
			if got := world.Panes[s.b]; got.Window != home || got.StandIn != "" || got.HomeSession != "" {
				t.Errorf("the tab was left and the agent is not home: %+v", got)
			}
			if stage, found := world.Stage(s.tab.List); !found || world.Panes[stage].Role != roleStandIn {
				t.Errorf("the stage holds %q", stage)
			}
		})
	}
}

// TestATerminalComingToItsWindowFindsTheAgentThere: with the tab on screen
// in one terminal, a second one goes to the window a borrowed agent lives in.
// The agent is back before that window is drawn, and is not taken again while
// the terminal stays.
func TestATerminalComingToItsWindowFindsTheAgentThere(t *testing.T) {
	s := aScene(t)
	agent := must(t, s.tmux, "new-session", "-d", "-s", "work", "-P", "-F", "#{pane_id}", "sleep", "600")
	home := must(t, s.tmux, "display-message", "-p", "-t", agent, "#{window_id}")
	must(t, s.tmux, "new-window", "-t", "work:", "sleep", "600")
	viewer(t, s.tmux, "work")
	if world := s.show(t, Desire{Staged: agent}); world.Panes[agent].Window != s.tabWindow(t) {
		t.Fatalf("%s did not reach the stage", agent)
	}

	must(t, s.tmux, "select-window", "-t", "work:"+home)
	world := s.see(t)
	if !world.Visible(s.tab.List) {
		t.Fatal("the tab is no longer on screen in the first terminal")
	}
	if got := world.Panes[agent]; got.Window != home || got.StandIn != "" {
		t.Fatalf("a terminal came to its window and the agent is not there: %+v", got)
	}
	if world := s.show(t, Desire{Staged: agent}); len(world.Borrowed()) != 0 {
		t.Errorf("the agent was taken from under the terminal looking at it: %v", world.Borrowed())
	}
	// and with a world read before the terminal came, tmux refuses the swap
	// as it runs it
	stale := s.see(t)
	stale.Panes[agent] = unseen(stale.Panes[agent])
	if step, more := Next(stale, s.tab.List, agent, s.tab.ListWidth); !more {
		t.Fatal("nothing was planned on the old read")
	} else {
		s.tab.do(step)
	}
	if got := s.see(t).Panes[agent]; got.Window != home {
		t.Errorf("a swap planned on an old read went through: %+v", got)
	}

	must(t, s.tmux, "select-window", "-t", "work:1")
	if world := s.show(t, Desire{Staged: agent}); world.Panes[agent].Window != s.tabWindow(t) {
		t.Errorf("the terminal left and the agent did not come back to the stage: %+v", world.Panes[agent])
	}
}

// unseen is a pane as it was read before anybody came to its window.
func unseen(pane Pane) Pane {
	pane.Views = slices.Clone(pane.Views)
	for i := range pane.Views {
		pane.Views[i].WindowActive = false
	}
	return pane
}

// TestTheListIsWokenWhenATerminalComesToTheTab, by the hooks, which type its
// key into its pane; and every key is kept until it is read, however many
// there are. The list here is a `sleep`, so its terminal shows what was typed.
func TestTheListIsWokenWhenATerminalComesToTheTab(t *testing.T) {
	s := aScene(t)
	s.show(t, Desire{})
	typed := func() int {
		t.Helper()
		return strings.Count(must(t, s.tmux, "capture-pane", "-p", "-J", "-t", s.tab.List), "^[[24~")
	}
	before := typed()
	for range 3 {
		must(t, s.tmux, "select-window", "-t", "home:0")
		if got := typed(); got != before {
			t.Fatalf("the tab was left and the list was woken %d times", got-before)
		}
		must(t, s.tmux, "select-window", "-t", "home:"+s.tabWindow(t))
		if got := typed(); got != before+1 {
			t.Fatalf("a terminal came to the tab and the list was woken %d times", got-before)
		}
		before++
	}
}

// TestHooksLeftBehindDoNothing: the tab is killed with its hooks still on the
// server, naming a window and a stand-in that are gone. An `if-shell` aimed
// at something that is gone tests the pane in front instead, so each test in
// them has to be false of every other pane: nothing moves, and nothing is
// typed into whatever pane is in front.
func TestHooksLeftBehindDoNothing(t *testing.T) {
	s := aScene(t)
	s.show(t, Desire{Staged: s.b})
	must(t, s.tmux, "select-window", "-t", "home:0")
	if world := s.see(t); len(world.Borrowed()) != 0 {
		t.Fatalf("the agent is not home: %v", world.Borrowed())
	}
	must(t, s.tmux, "set-hook", "-w", "-u", "-t", s.tabWindow(t), hookOf("window-layout-changed"))
	must(t, s.tmux, "kill-window", "-t", s.tabWindow(t))
	if hooks := must(t, s.tmux, "show-hooks", "-g"); !strings.Contains(hooks, hookName) {
		t.Fatal("the hooks went with the tab, and this is about the ones that stay")
	}

	before := must(t, s.tmux, "list-panes", "-a", "-F", "#{pane_id} #{window_id} #{pane_index}")
	for _, window := range []string{"home:1", "home:0", "home:1"} {
		must(t, s.tmux, "select-window", "-t", window)
	}
	if after := must(t, s.tmux, "list-panes", "-a", "-F", "#{pane_id} #{window_id} #{pane_index}"); after != before {
		t.Errorf("something moved:\n%s\nwas\n%s", after, before)
	}
	// every pane is a `sleep`, whose terminal shows what is typed into it
	for pane := range s.see(t).Panes {
		if got := must(t, s.tmux, "capture-pane", "-p", "-t", pane); strings.Contains(got, "^[") {
			t.Errorf("a key was typed into %s: %q", pane, got)
		}
	}
}

// TestAWindowKeepsItsNameWhileItsAgentIsOnTheStage: a window tmux names after
// its active pane would be named after the stand-in, so its naming is held
// off for as long as the agent is away, and given back by whatever sends the
// agent home: here, the hooks.
func TestAWindowKeepsItsNameWhileItsAgentIsOnTheStage(t *testing.T) {
	s := aScene(t)
	home := must(t, s.tmux, "display-message", "-p", "-t", s.b, "#{window_id}")
	naming := func() string {
		t.Helper()
		return must(t, s.tmux, "show-options", "-w", "-t", home, "automatic-rename")
	}
	if got := naming(); got != "" {
		t.Fatalf("the window starts with %q", got)
	}
	if world := s.show(t, Desire{Staged: s.b}); world.Panes[s.b].Renamed != home {
		t.Fatalf("the borrowed pane does not say its window's name is held: %+v", world.Panes[s.b])
	}
	if got := naming(); got != "automatic-rename off" {
		t.Errorf("while its agent is away the window has %q", got)
	}
	must(t, s.tmux, "select-window", "-t", "home:0")
	world := s.see(t)
	if got := naming(); got != "" || world.Panes[s.b].Renamed != "" || !world.Panes[s.b].AutoRename {
		t.Errorf("once its agent was home the window was left with %q: %+v", got, world.Panes[s.b])
	}

	// and a window somebody named is not touched at all
	must(t, s.tmux, "rename-window", "-t", home, "mine")
	must(t, s.tmux, "select-window", "-t", "home:"+s.tabWindow(t))
	world = s.show(t, Desire{Staged: s.b})
	if got := world.Panes[s.b]; got.Window != s.tabWindow(t) || got.Renamed != "" || naming() != "automatic-rename off" {
		t.Errorf("a window that was given its name: %+v, with %q", got, naming())
	}
	must(t, s.tmux, "select-window", "-t", "home:0")
	if got := naming(); got != "automatic-rename off" {
		t.Errorf("the window somebody named was left with %q", got)
	}
}

// TestATabWhoseListIsKilledClosesItself: the list is the tab's only program,
// and however it goes the server sends the agent home and closes what is
// left, with nothing of this program running.
func TestATabWhoseListIsKilledClosesItself(t *testing.T) {
	s := aScene(t)
	world := s.show(t, Desire{Staged: s.a})
	standIn := world.Panes[s.a].StandIn
	home := world.Panes[standIn].Window
	must(t, s.tmux, "kill-pane", "-t", s.tab.List)

	world = s.see(t)
	if got := world.Panes[s.a]; got.Window != home || got.StandIn != "" {
		t.Errorf("the agent was not given back: %+v", got)
	}
	if _, there := world.Panes[standIn]; there {
		t.Error("the stand-in is still there")
	}
	if out, err := bare(s.tmux, "has-session", "-t", "="+tabSession); err == nil {
		t.Errorf("the tab's session outlived the tab: %s", out)
	}
	if hooks := must(t, s.tmux, "show-hooks", "-g"); strings.Contains(hooks, hookName) {
		t.Errorf("the tab's hooks outlived the tab:\n%s", hooks)
	}
}

func TestClosingSendsEverythingHomeAndLeavesNothing(t *testing.T) {
	s := aScene(t)
	world := s.show(t, Desire{Staged: s.a})
	home := world.Panes[world.Panes[s.a].StandIn].Window
	if err := Close(s.tmux, quiet); err != nil {
		t.Fatalf("Close: %v", err)
	}
	world = s.see(t)
	for _, pane := range world.Panes {
		if pane.Role != "" || pane.StandIn != "" {
			t.Errorf("%s is left over: %+v", pane.ID, pane)
		}
	}
	if world.Panes[s.a].Window != home || world.Panes[s.b].ID == "" {
		t.Errorf("the agents are not both at home: %+v", world.Panes)
	}
	if out, err := bare(s.tmux, "has-session", "-t", "="+tabSession); err == nil {
		t.Errorf("the tab's session outlived the tab: %s", out)
	}
	if hooks := must(t, s.tmux, "show-hooks", "-g"); strings.Contains(hooks, hookName) {
		t.Errorf("the tab's hooks outlived the tab:\n%s", hooks)
	}
}

func TestEnterPutsTheFocusOnTheStage(t *testing.T) {
	s := aScene(t)
	world, err := s.tab.Enter(Desire{Staged: s.a})
	if err != nil {
		t.Fatalf("Enter: %v", err)
	}
	if !world.Panes[s.a].Active || !world.StageFocused(s.tab.List) {
		t.Errorf("the agent on the stage is not the tab's active pane: %+v", world.Panes[s.a])
	}
}

// TestEnterStaysOnTheListWhenTheStageIsEmpty: the stand-in takes no keys.
func TestEnterStaysOnTheListWhenTheStageIsEmpty(t *testing.T) {
	s := aScene(t)
	world, err := s.tab.Enter(Desire{})
	if err != nil {
		t.Fatalf("Enter: %v", err)
	}
	if !world.Panes[s.tab.List].Active {
		t.Error("the focus went to a stand-in")
	}
}

// TestGoingToAnAgentLeavesTheTabAndLandsOnIt: the agent is given back first,
// so the terminal lands in the window it lives in and not on the tab.
func TestGoingToAnAgentLeavesTheTabAndLandsOnIt(t *testing.T) {
	s := aScene(t)
	s.show(t, Desire{Staged: s.b})
	world, err := s.tab.GoTo(s.b)
	if err != nil {
		t.Fatalf("GoTo: %v", err)
	}
	if world.Visible(s.tab.List) || len(world.Borrowed()) != 0 {
		t.Errorf("the tab is still in front, or something is still away: %v", world.Borrowed())
	}
	if got := must(t, s.tmux, "display-message", "-p", "-c", s.client, "#{pane_id}"); got != s.b {
		t.Errorf("the terminal is on %s, and was sent to %s", got, s.b)
	}
}

// TestASessionThatClosesTakesNoAgentFromAnother: the tab was opened from a
// session that is then killed, with another session's agent on the stage. The
// tab lives in its own session, so the kill only unlinks it.
func TestASessionThatClosesTakesNoAgentFromAnother(t *testing.T) {
	s := aScene(t)
	other := must(t, s.tmux, "new-session", "-d", "-s", "work", "-P", "-F", "#{pane_id}", "sleep", "600")
	s.show(t, Desire{Staged: other})
	if s.see(t).Panes[other].Window != s.tabWindow(t) {
		t.Fatal("the other session's agent did not reach the stage")
	}
	must(t, s.tmux, "kill-session", "-t", "=home")
	world := s.see(t)
	if _, alive := world.Panes[other]; !alive {
		t.Fatal("killing the session the tab was shown in killed another session's agent")
	}
	if world = s.show(t, Desire{Staged: other}); world.Panes[other].StandIn != "" {
		t.Errorf("nobody is looking any more and the agent is still away: %+v", world.Panes[other])
	}
	if got := world.Where(other); got != "work:0" {
		t.Errorf("the agent went back to %q, and lives in work:0", got)
	}
}
