package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/lassoColombo/agent-notify/tool"
)

// Zellij is the only thing in this program that runs anything.
//
// It talks to zellij the way everything talks to zellij: by running it. There
// is no library and no socket protocol to link against — `zellij action` is the
// supported external interface, and that is what D-27 sanctioned shelling out
// for. The half that talks to the tool is the dangerous half, and it lives
// here, in a connected daemon, off the path any agent waits on (R3).
type Zellij struct {
	Binary  string
	Timeout time.Duration
}

// Pane is the part of `zellij action list-panes --json --tab` this needs.
//
// The rest of that record — geometry, cursor position, exit status — is real
// and ignored: a display that decoded every field would break on the next
// zellij release that adds one.
type Pane struct {
	ID      int    `json:"id"`
	Plugin  bool   `json:"is_plugin"`
	Title   string `json:"title"`
	TabID   int    `json:"tab_id"`
	TabName string `json:"tab_name"`
}

// Address is how zellij is asked for one pane: ids are unique per kind, not
// globally, so `terminal_0` and `plugin_0` are two different panes.
func Address(id int) string { return "terminal_" + strconv.Itoa(id) }

// Panes asks one zellij session what it is holding.
//
// The parse is the test of whether that session exists, and that is not
// fastidiousness. Measured on zellij 0.45.1, asking for a session that is not
// there exits 0 when some other detached session happens to be alive — writing
// the list of sessions that do exist where the JSON should be — and exits 1
// when none is. The status is a function of state that has nothing to do with
// the question, which makes it not a signal. (A missing PANE inside a live
// session does exit 2, reliably; that is a different question.)
//
// This is why every render reads before it writes: a failed read means the
// whole zellij session is skipped and not one rename is attempted against it.
func (z Zellij) Panes(zellijSession string) ([]Pane, error) {
	out, err := z.run(zellijSession, "action", "list-panes", "--json", "--tab")
	if err != nil {
		return nil, err
	}
	var panes []Pane
	if err := json.Unmarshal(out, &panes); err != nil {
		return nil, fmt.Errorf("zellij session %q is not there (it answered %q)",
			zellijSession, tool.Summarise(out))
	}
	return panes, nil
}

// Do performs one planned command.
func (z Zellij) Do(command Command) error {
	_, err := z.run(command.Session, command.Args...)
	return err
}

// run is `zellij --session <session> …`, under this display's timeout.
//
// The running is [tool.Run], which is where the WaitDelay, the check order and
// the cleaning of whatever zellij printed all live (D-68). What is zellij's own
// and stays here is the flag placement.
func (z Zellij) run(zellijSession string, args ...string) ([]byte, error) {
	// --session before the subcommand: it is a flag of zellij itself, not of
	// `action`, and without it a machine with two sessions renames a pane in
	// whichever one zellij picks.
	whole := append([]string{"--session", zellijSession}, args...)
	out, err := tool.Run(z.Binary, z.Timeout, whole...)
	return out.Stdout, err
}
