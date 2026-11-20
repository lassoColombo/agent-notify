package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lassoColombo/agent-notify/session"
)

// The glyphs under test are ASCII, on purpose: what is being asserted is the
// shape of the plan, and a test that carries private-use codepoints around in
// its own expectations proves nothing about the ones that ship.
var testGlyphs = session.NewPalette(map[session.Kernel]string{
	session.BlockedOnYou:  "!",
	session.Broke:         "x",
	session.FinishedATurn: ">",
	session.Working:       "*",
	session.Idle:          "",
	session.Ended:         "",
}, nil)

var nine = time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)

// home is the server the pure tests are about: a socket, and the pid of the
// server listening on it.
const (
	home = "/tmp/tmux-501/default"
	pid  = 4242
)

// placed is a record captured in pane `pane` of `home`.
func placed(name string, kernel session.Kernel, pane string) session.Record {
	return placedIn(name, kernel, home, pid, pane)
}

func placedIn(name string, kernel session.Kernel, socket string, server int, pane string) session.Record {
	blob, err := json.Marshal(Captured{Server: socket + "," + strconv.Itoa(server) + ",0", Pane: pane})
	if err != nil {
		panic(err)
	}
	return session.Record{
		Key:        session.Key{Host: "mac", Agent: "claude", SessionID: name},
		Name:       name,
		Kernel:     kernel,
		Rank:       kernel.Rank(),
		StateSince: nine,
		CapturedContext: session.CapturedContext{
			CapturedAt: nine,
			By:         map[string]json.RawMessage{Name: blob},
		},
	}
}

// ran is a plan as one line per command: the argv exactly as it would run.
func ran(commands []Command) []string {
	lines := make([]string, 0, len(commands))
	for _, command := range commands {
		lines = append(lines, strings.Join(command.Args, " "))
	}
	return lines
}

func same(t *testing.T, why string, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s:\n got: %s\nwant: %s", why, strings.Join(got, "\n      "), strings.Join(want, "\n      "))
	}
}

var working = Mark{State: "working", Glyph: "*", Name: "alpha"}

func TestPlan(t *testing.T) {
	for _, given := range []struct {
		why     string
		records []session.Record
		panes   map[string]Mark
		want    []string
	}{{
		why:     "an agent's pane says its state, its glyph and its name",
		records: []session.Record{placed("alpha", session.Working, "%3")},
		panes:   map[string]Mark{"%3": {}, "%4": {}},
		want: []string{
			"set-option -p -t %3 @agent-notify-state working",
			"set-option -p -t %3 @agent-notify-glyph *",
			"set-option -p -t %3 @agent-notify-name alpha",
		},
	}, {
		why:     "an idle agent has no glyph, and its name alone is shown",
		records: []session.Record{placed("alpha", session.Idle, "%3")},
		panes:   map[string]Mark{"%3": {}},
		want: []string{
			"set-option -p -t %3 @agent-notify-state idle",
			"set-option -p -t %3 @agent-notify-name alpha",
		},
	}, {
		why:     "what already says the right thing is not written again",
		records: []session.Record{placed("alpha", session.Working, "%3")},
		panes:   map[string]Mark{"%3": working},
	}, {
		why:     "a state that moves rewrites what moved with it, and not the name",
		records: []session.Record{placed("alpha", session.BlockedOnYou, "%3")},
		panes:   map[string]Mark{"%3": working},
		want: []string{
			"set-option -p -t %3 @agent-notify-state blocked-on-you",
			"set-option -p -t %3 @agent-notify-glyph !",
		},
	}, {
		why:     "an agent that ended gives its pane back",
		records: []session.Record{placed("alpha", session.Ended, "%3")},
		panes:   map[string]Mark{"%3": working},
		want: []string{
			"set-option -p -u -t %3 @agent-notify-state",
			"set-option -p -u -t %3 @agent-notify-glyph",
			"set-option -p -u -t %3 @agent-notify-name",
		},
	}, {
		why:   "a pane nobody claims is given back, whether or not a record remembers it",
		panes: map[string]Mark{"%3": {}, "%5": working},
		want: []string{
			"set-option -p -u -t %5 @agent-notify-state",
			"set-option -p -u -t %5 @agent-notify-glyph",
			"set-option -p -u -t %5 @agent-notify-name",
		},
	}, {
		why:     "a record of the server that was on this socket before is not this server's",
		records: []session.Record{placedIn("alpha", session.Working, home, pid-1, "%3")},
		panes:   map[string]Mark{"%3": {}},
	}, {
		why:     "a record of another server is not this one's",
		records: []session.Record{placedIn("alpha", session.Working, "/tmp/tmux-501/other", pid, "%3")},
		panes:   map[string]Mark{"%3": {}},
	}, {
		why:     "a pane that is gone is nobody's business here (R4)",
		records: []session.Record{placed("alpha", session.Working, "%9")},
		panes:   map[string]Mark{"%3": {}},
	}, {
		why: "of two records claiming one pane, the more urgent is shown",
		records: []session.Record{
			placed("old", session.Working, "%3"),
			placed("new", session.BlockedOnYou, "%3"),
		},
		panes: map[string]Mark{"%3": {}},
		want: []string{
			"set-option -p -t %3 @agent-notify-state blocked-on-you",
			"set-option -p -t %3 @agent-notify-glyph !",
			"set-option -p -t %3 @agent-notify-name new",
		},
	}, {
		why:     "panes are taken in the order of their numbers",
		records: []session.Record{placed("ten", session.Idle, "%10"), placed("nine", session.Idle, "%9")},
		panes:   map[string]Mark{"%10": {}, "%9": {}},
		want: []string{
			"set-option -p -t %9 @agent-notify-state idle",
			"set-option -p -t %9 @agent-notify-name nine",
			"set-option -p -t %10 @agent-notify-state idle",
			"set-option -p -t %10 @agent-notify-name ten",
		},
	}, {
		why:     "a name ending in ; is handed over so that tmux does not end the command there",
		records: []session.Record{placed("fix it;", session.Idle, "%3")},
		panes:   map[string]Mark{"%3": {State: "idle"}},
		want:    []string{`set-option -p -t %3 @agent-notify-name fix it\;`},
	}} {
		server := Server{PID: pid, Panes: given.panes}
		same(t, given.why, ran(Plan(home, given.records, server, testGlyphs)), given.want)
	}
}

// TestOnePlanIsOneInvocation: the commands are joined by `;` as separate
// arguments, which is how tmux takes several commands from one argv.
func TestOnePlanIsOneInvocation(t *testing.T) {
	got := strings.Join(chain([]Command{
		{Args: []string{"set-option", "-p", "-t", "%3", stateOption, "idle"}},
		{Args: []string{"set-option", "-p", "-u", "-t", "%4", stateOption}},
	}), " ")
	if want := "set-option -p -t %3 @agent-notify-state idle ; set-option -p -u -t %4 @agent-notify-state"; got != want {
		t.Errorf("chain = %q, want %q", got, want)
	}
}

// TestRecordsAreGroupedByServer: one look and one write per server, however
// many agents it holds; a record of no server is in no group.
func TestRecordsAreGroupedByServer(t *testing.T) {
	grouped := Group([]session.Record{
		placed("alpha", session.Working, "%3"),
		placedIn("beta", session.Idle, "/tmp/tmux-501/other", pid, "%3"),
		placed("gamma", session.Idle, "%4"),
		{Name: "outside"},
	})
	if len(grouped) != 2 || len(grouped[home]) != 2 || len(grouped["/tmp/tmux-501/other"]) != 1 {
		t.Errorf("grouped = %v", grouped)
	}
}

// TestEveryFieldAMarkReadsIsOneItWakesFor asks the planner what it reads
// rather than trusting the list (D-72, D-73).
func TestEveryFieldAMarkReadsIsOneItWakesFor(t *testing.T) {
	server := Server{PID: pid, Panes: map[string]Mark{"%7": {}, "%8": {}}}
	base := placed("alpha", session.Working, "%7")
	base.Detail = "running-bash"
	base.Cwd = "/Users/somebody/projects/agent-notify"

	missing := session.FieldsRenderedButNotWokenFor(base, WhatToWakeFor(), func(record session.Record) string {
		return fmt.Sprintf("%+v", Plan(home, []session.Record{record}, server, testGlyphs))
	})
	if len(missing) > 0 {
		t.Errorf("a different plan comes out when %v move, and this display does not ask to be woken for them", missing)
	}
}
