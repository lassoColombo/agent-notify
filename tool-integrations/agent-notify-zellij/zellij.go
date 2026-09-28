package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/lassoColombo/agent-notify/tool"
)

// Zellij is the only thing here that runs anything. `zellij action` is the
// supported external interface, and every invocation asks zellij afresh (R17).
type Zellij struct {
	Binary  string
	Timeout time.Duration
}

// Pane is the part of `list-panes --json` this needs. The rest is ignored so
// that a zellij release adding a field breaks nothing.
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
	// person is looking at.
	FloatingVisible bool `json:"are_floating_panes_visible"`
}

// Client is one row of `list-clients`: somebody actually looking at this
// session, and the pane they are on. Nothing in the pane or tab JSON says
// whether a session is being looked at, only that it has a stored focus.
type Client struct {
	ID int
	// Pane is spelled as zellij prints it — "terminal_7" — because that is
	// the form it wants back.
	Pane string
}

// Address is how zellij is asked for one pane: ids are unique per kind, so
// terminal_0 and plugin_0 are two different panes.
func Address(id int) string { return "terminal_" + strconv.Itoa(id) }

// Panes asks one zellij session what it is holding. The parse is the test of
// whether the session exists: measured on 0.45.1, asking for a session that is
// not there exits 0 when another detached session is alive, writing the
// session list where the JSON should be.
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

// Clients is who is looking at this session. `list-clients` has no --json, so
// the table it prints is parsed; no clients is the header alone, which is an
// ordinary answer.
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

// ParseClients is the pure half. The header is load-bearing: a session that
// does not exist exits 0 and writes the session list where the table should
// be, so its absence is how a missing session is told from an empty one.
func ParseClients(output []byte) ([]Client, error) {
	lines := strings.Split(strings.TrimRight(string(output), "\n"), "\n")
	if len(lines) == 0 || !strings.HasPrefix(strings.TrimSpace(lines[0]), "CLIENT_ID") {
		return nil, fmt.Errorf("this is not what `list-clients` prints")
	}

	var clients []Client
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		// RUNNING_COMMAND can be empty or contain spaces, so only the first
		// two fields are read.
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

func (z Zellij) GoToTab(session string, tab int) error {
	_, err := z.run(session, "action", "go-to-tab-by-id", strconv.Itoa(tab))
	return err
}

func (z Zellij) FocusPane(session string, pane int) error {
	_, err := z.run(session, "action", "focus-pane-id", Address(pane))
	return err
}

// SwitchSession takes THIS terminal to another session, landing on one pane.
// The missing `--session` is why it works: zellij routes the action with the
// client id of whoever sent it, and without the flag that is the terminal this
// program was started from. Run from the session-watcher or launchd it moves
// nobody, which is why [Focus] reads its own ZELLIJ_SESSION_NAME first.
func (z Zellij) SwitchSession(target string, pane int) error {
	_, err := tool.Run(z.Binary, z.Timeout,
		"action", "switch-session", target, "--pane-id", Address(pane))
	return err
}

// Do performs one planned rename.
func (z Zellij) Do(command Command) error {
	_, err := z.run(command.Session, command.Args...)
	return err
}

// run is `zellij --session <session> …`: the flag belongs to zellij itself,
// not to `action`, and without it a machine with two sessions acts on
// whichever one zellij picks.
func (z Zellij) run(session string, args ...string) ([]byte, error) {
	whole := append([]string{"--session", session}, args...)
	out, err := tool.Run(z.Binary, z.Timeout, whole...)
	return out.Stdout, err
}
