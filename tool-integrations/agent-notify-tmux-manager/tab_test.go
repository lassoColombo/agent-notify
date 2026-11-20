package main

import (
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
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

// idle is what every pane of a test runs, the tab's own two included: a test
// is about where panes are, not about what is in them.
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

// viewer attaches a real client to session home and returns its name: a pane
// of a second server running `tmux attach` to the first is an attached client
// like any other.
func viewer(t *testing.T, tmux Tmux) string {
	t.Helper()
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
		"env -u TMUX "+tmux.Binary+" -u -f /dev/null -S "+tmux.Socket+" attach -t home")
	deadline := time.Now().Add(15 * time.Second)
	for {
		if name, err := bare(tmux, "list-clients", "-F", "#{client_name}"); err == nil && name != "" {
			return name
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
	s.client = viewer(t, tmux)
	if err := Open(tmux, s.client, idle); err != nil {
		t.Fatalf("Open: %v", err)
	}
	list, found := s.see(t).List()
	if !found {
		t.Fatal("the tab was opened and has no list")
	}
	s.tab = Tab{Tmux: tmux, List: list.ID, Layout: Layout{StandIn: idle, ListWidth: 30}, Logger: quiet}
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

// TestAStandInThatDiesKeepsThePlace: its program is killed while it holds an
// agent's place. The pane stays, and is started again where it stands.
func TestAStandInThatDiesKeepsThePlace(t *testing.T) {
	s := aScene(t)
	world := s.show(t, Desire{Staged: s.a})
	standIn := world.Panes[s.a].StandIn
	home := world.Panes[standIn].Window
	before := s.shape(t, home)

	pid, err := strconv.Atoi(must(t, s.tmux, "display-message", "-p", "-t", standIn, "#{pane_pid}"))
	if err != nil {
		t.Fatalf("the stand-in's pid: %v", err)
	}
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		t.Fatalf("kill: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !s.see(t).Panes[standIn].Dead {
		if time.Now().After(deadline) {
			t.Fatal("the stand-in's pane never showed as dead")
		}
		time.Sleep(50 * time.Millisecond)
	}

	world = s.show(t, Desire{Staged: s.a})
	if pane, there := world.Panes[standIn]; !there || pane.Dead || pane.Window != home {
		t.Errorf("the stand-in is %+v (there = %v)", pane, there)
	}
	if after := s.shape(t, home); after != before {
		t.Errorf("the window changed shape:\n%s\nwas\n%s", after, before)
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

// TestAStandInLeftAloneGivesBack is what a stand-in does when the list is
// gone: the agent goes home, and the stand-in closes.
func TestAStandInLeftAloneGivesBack(t *testing.T) {
	s := aScene(t)
	world := s.show(t, Desire{Staged: s.a})
	standIn := world.Panes[s.a].StandIn
	home := world.Panes[standIn].Window
	if err := leave(s.tmux, standIn); err != nil {
		t.Fatalf("leave: %v", err)
	}
	world = s.see(t)
	if _, there := world.Panes[standIn]; there {
		t.Error("the stand-in is still there")
	}
	if got := world.Panes[s.a]; got.Window != home || got.StandIn != "" {
		t.Errorf("the agent was not given back: %+v", got)
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
