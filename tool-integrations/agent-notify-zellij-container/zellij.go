package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/lassoColombo/agent-notify/tool"
)

// Zellij is the only thing here that runs anything.
//
// Unlike the display half, this program is run rather than connected: core
// spawns it with a subcommand when it needs an answer (D-38). So there is no
// daemon, no cache and no state — every invocation asks zellij afresh, which is
// also what R17 requires of coordinates at the moment of use.
type Zellij struct {
	Binary  string
	Timeout time.Duration
}

// Pane is the part of `list-panes --json --all` this needs.
type Pane struct {
	ID       int    `json:"id"`
	Plugin   bool   `json:"is_plugin"`
	Title    string `json:"title"`
	Focused  bool   `json:"is_focused"`
	Floating bool   `json:"is_floating"`
	TabID    int    `json:"tab_id"`
	TabName  string `json:"tab_name"`
}

// Tab is the part of `list-tabs --json --all` this needs.
type Tab struct {
	ID     int    `json:"tab_id"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
	// FloatingVisible decides which of a tab's two focused panes is the one a
	// person is actually looking at. See Focused in place.go.
	FloatingVisible bool `json:"are_floating_panes_visible"`
}

// Client is one row of `list-clients`: somebody who is actually looking at this
// session, and the pane they are looking at.
//
// It is the only thing zellij will tell you about whether a session is being
// LOOKED at, as opposed to merely running. Nothing in the pane or tab JSON
// carries it: `active`, `is_focused` and the rest describe a session's stored
// focus, which exists whether or not a single terminal is rendering it.
type Client struct {
	ID int
	// Pane is spelled as zellij prints it — "terminal_7" — because that is the
	// form it also wants back, and re-deriving it from an int is a place to get
	// terminal_ and plugin_ the wrong way round.
	Pane string
}

// Address is how zellij is asked for one pane: ids are unique per kind, so
// terminal_0 and plugin_0 are two different panes.
func Address(id int) string { return "terminal_" + strconv.Itoa(id) }

func (z Zellij) Panes(session string) ([]Pane, error) {
	out, err := z.run(session, "action", "list-panes", "--json", "--tab")
	if err != nil {
		return nil, err
	}
	var panes []Pane
	if err := json.Unmarshal(out, &panes); err != nil {
		return nil, fmt.Errorf("zellij session %q is not there (it answered %q)", session, tool.Summarise(out))
	}
	return panes, nil
}

func (z Zellij) Tabs(session string) ([]Tab, error) {
	out, err := z.run(session, "action", "list-tabs", "--json", "--all")
	if err != nil {
		return nil, err
	}
	var tabs []Tab
	if err := json.Unmarshal(out, &tabs); err != nil {
		return nil, fmt.Errorf("zellij session %q is not there (it answered %q)", session, tool.Summarise(out))
	}
	return tabs, nil
}

// Clients is who is looking at this session, and it is the question that
// decides whether focusing anything here can move a person at all.
//
// `list-clients` has no --json, so this parses the table it prints: a
// CLIENT_ID / ZELLIJ_PANE_ID / RUNNING_COMMAND header and one space-padded row
// per attached client. No clients is the header on its own, and is an ordinary
// answer rather than an error — a session can run for days with nobody in it.
func (z Zellij) Clients(session string) ([]Client, error) {
	out, err := z.run(session, "action", "list-clients")
	if err != nil {
		return nil, err
	}
	clients, err := ParseClients(out)
	if err != nil {
		return nil, fmt.Errorf("zellij session %q is not there (it answered %q)", session, tool.Summarise(out))
	}
	return clients, nil
}

// ParseClients is the pure half, because the table is the part that can be
// wrong and a real zellij is not needed to say so.
//
// The header is load-bearing: asking a session that does not exist exits **0**
// and writes the list of sessions that do where the table should be, so its
// absence is how a missing session is detected here (the same trap `Panes` and
// `Tabs` sidestep by parsing JSON).
func ParseClients(output []byte) ([]Client, error) {
	lines := strings.Split(strings.TrimRight(string(output), "\n"), "\n")
	if len(lines) == 0 || !strings.HasPrefix(strings.TrimSpace(lines[0]), "CLIENT_ID") {
		return nil, fmt.Errorf("this is not what `list-clients` prints")
	}

	var clients []Client
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		// A row is at least an id and a pane; RUNNING_COMMAND can be empty, and
		// it can also contain spaces, which is why only the first two fields
		// are ever read.
		if len(fields) < 2 {
			continue
		}
		id, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		clients = append(clients, Client{ID: id, Pane: fields[1]})
	}
	return clients, nil
}

// GoToTab and FocusPane are the two halves of arriving somewhere. Both are
// issued even though focusing a pane may switch tabs on its own: saying what
// you mean costs one subprocess and does not depend on a behaviour nobody
// wrote down.
func (z Zellij) GoToTab(session string, tab int) error {
	_, err := z.run(session, "action", "go-to-tab-by-id", strconv.Itoa(tab))
	return err
}

func (z Zellij) FocusPane(session string, pane int) error {
	_, err := z.run(session, "action", "focus-pane-id", Address(pane))
	return err
}

// SwitchSession takes THIS terminal to another session, landing it on one pane.
//
// It is the only thing here that is not addressed to a session by name, and the
// missing `--session` is the whole of why it works. zellij routes an action with
// the client id of whoever sent it: with `--session` this program is its own
// throwaway client and switches that, which exits 0 and moves nobody. Without
// it, the CLI is the one zellij already associates with the terminal this
// program was started from, and that terminal is what moves.
//
// So this can only be asked by something a person started from the terminal
// they are looking at — the picker under its keybinding, or a command typed at
// a prompt. Run from the session-watcher or from a launchd job it is the same
// silent nothing as any other spelling, which is why [Focus] reads its own
// ZELLIJ_SESSION_NAME before it ever asks zellij to do this.
//
// One command, not two: `--pane-id` makes the pane's tab active on the way in,
// so there is no go-to-tab to pair with it. The pane is named the way zellij
// names it, because that is the form it wants back.
func (z Zellij) SwitchSession(target string, pane int) error {
	_, err := tool.Run(z.Binary, z.Timeout,
		"action", "switch-session", target, "--pane-id", Address(pane))
	return err
}

// run is `zellij --session <session> …`, under this program's timeout.
//
// The running is [tool.Run], which is where the WaitDelay, the check order and
// the cleaning of whatever zellij printed all live (D-68). What is zellij's own
// and stays here is the flag placement.
func (z Zellij) run(session string, args ...string) ([]byte, error) {
	// --session before the subcommand: it is a flag of zellij itself, not of
	// `action`. Without it a machine with two sessions acts on whichever one
	// zellij picks, which for a focus means taking somebody somewhere wrong.
	whole := append([]string{"--session", session}, args...)
	out, err := tool.Run(z.Binary, z.Timeout, whole...)
	return out.Stdout, err
}

// TheZellijToRun is the path out of the configuration, checked.
//
// There is no search here and no PATH fallback, and the absence is the design
// (D-67). This program is run by the session-watcher and by the hook, whose
// PATH is not your shell's — a launchd job's is /usr/bin:/bin and nothing else
// — so a lookup performed HERE is performed in the one context that cannot
// answer it. The list of Homebrew prefixes that used to sit at this line was an
// attempt to guess what the environment would not say.
//
// The lookup happens at install instead, which runs in your shell, where the
// answer is simply available.
func TheZellijToRun(given string) (string, error) {
	const key = "[integration." + Name + ".settings] zellij"
	switch {
	case given == "":
		return "", fmt.Errorf("%s is not set — run `agent-notify install %s`, which finds "+
			"zellij in your own shell and prints the line to add", key, Name)
	case !filepath.IsAbs(given):
		return "", fmt.Errorf("%s = %q must be an absolute path: a supervised child's PATH "+
			"is not yours, so a bare name means something different here than it does to you",
			key, given)
	}
	if _, err := os.Stat(given); err != nil {
		return "", fmt.Errorf("%s = %q: %w", key, given, err)
	}
	return given, nil
}
