package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/lassoColombo/agent-notify/tool"
)

// Tmux is the only thing here that runs anything. Every invocation names its
// server with `-S` and asks it afresh (R17): this program is run by the
// session-watcher, whose own $TMUX — if it has one — is whichever pane
// happened to start it, and says nothing about the server a session is in.
type Tmux struct {
	Binary  string
	Timeout time.Duration
}

// The two user options this program owns. A user option is invisible until a
// format names it, so drawing them is the tmux configuration's job (see the
// README); writing them never touches a name or a title anybody else set.
const (
	paneOption   = "@agent-notify-pane"
	windowOption = "@agent-notify-window"
)

// separator is what the formats below put between fields: the ASCII unit
// separator, which no window name, session name or glyph of ours contains, and
// which tmux passes through a format untouched — as long as it believes its
// output is UTF-8, which is what `-u` in run is for [measured on 3.8].
const separator = "\x1f"

// Row is one line of `list-panes -a`: a pane as one session sees it. A window
// linked into two sessions appears once for each, and `window_active` is per
// row, because "the window in front" is a property of a session, not of the
// window.
type Row struct {
	Pane        string
	SessionID   string
	SessionName string
	// Attached is how many clients are on this row's session.
	Attached     int
	WindowID     string
	WindowIndex  int
	WindowName   string
	WindowActive bool
	PaneActive   bool
	// PaneMark and WindowMark are what our two options say now. A pane's row
	// reads the window's option through tmux's own inheritance, which is why
	// the two have different names.
	PaneMark   string
	WindowMark string
}

var rowFormat = strings.Join([]string{
	"#{pane_id}", "#{session_id}", "#{session_name}", "#{session_attached}",
	"#{window_id}", "#{window_index}", "#{window_name}", "#{window_active}", "#{pane_active}",
	"#{" + paneOption + "}", "#{" + windowOption + "}",
}, separator)

const rowFields = 11

// Client is one row of `list-clients`: somebody actually looking at this
// server, and the session they are on.
type Client struct {
	Name      string
	SessionID string
	// Activity is when the client last did anything, in seconds; the most
	// recent one is the one tmux itself would pick for a command run with no
	// `-c`.
	Activity int64
}

var clientFormat = strings.Join([]string{"#{client_name}", "#{session_id}", "#{client_activity}"}, separator)

// Panes asks one tmux server what it is holding. The parse is the test of
// whether the server answered: a server that is not running exits 1 and says
// so on stderr, and anything that is not rows of this format is not believed
// whatever its exit code.
func (t Tmux) Panes(socket string) ([]Row, error) {
	out, err := t.run(socket, "list-panes", "-a", "-F", rowFormat)
	if err != nil {
		return nil, err
	}
	rows, err := ParseRows(out)
	if err != nil {
		return nil, fmt.Errorf("tmux server %q is not there (it answered %q)", socket, tool.Summarise(out))
	}
	return rows, nil
}

// ParseRows is the pure half of Panes.
func ParseRows(output []byte) ([]Row, error) {
	var rows []Row
	for _, line := range strings.Split(strings.TrimRight(string(output), "\n"), "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, separator, rowFields)
		if len(fields) != rowFields || !IsPaneID(fields[0]) {
			return nil, fmt.Errorf("this is not what `list-panes -F` was asked to print: %q", line)
		}
		attached, err := strconv.Atoi(fields[3])
		if err != nil {
			return nil, fmt.Errorf("session_attached is %q", fields[3])
		}
		index, err := strconv.Atoi(fields[5])
		if err != nil {
			return nil, fmt.Errorf("window_index is %q", fields[5])
		}
		rows = append(rows, Row{
			Pane: fields[0], SessionID: fields[1], SessionName: fields[2], Attached: attached,
			WindowID: fields[4], WindowIndex: index, WindowName: fields[6],
			WindowActive: fields[7] == "1", PaneActive: fields[8] == "1",
			PaneMark: fields[9], WindowMark: fields[10],
		})
	}
	if len(rows) == 0 {
		// A running server always has a session, and a session always has a
		// pane, so an empty answer is not an empty server.
		return nil, fmt.Errorf("no panes at all")
	}
	return rows, nil
}

// Clients is who is looking at this server. No clients is an empty answer, and
// an ordinary one.
func (t Tmux) Clients(socket string) ([]Client, error) {
	out, err := t.run(socket, "list-clients", "-F", clientFormat)
	if err != nil {
		return nil, err
	}
	var clients []Client
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, separator)
		if len(fields) != 3 {
			return nil, fmt.Errorf("tmux server %q answered %q to list-clients", socket, tool.Summarise(out))
		}
		activity, _ := strconv.ParseInt(fields[2], 10, 64)
		clients = append(clients, Client{Name: fields[0], SessionID: fields[1], Activity: activity})
	}
	return clients, nil
}

// SelectWindow makes a window the one in front of one session. The session is
// named because a linked window is in several, and a bare window id would
// leave tmux to choose.
func (t Tmux) SelectWindow(socket, sessionID, windowID string) error {
	_, err := t.run(socket, "select-window", "-t", sessionID+":"+windowID)
	return err
}

// SelectPane makes a pane the active one of its window. In a zoomed window it
// also unzooms, which is what puts the pane on screen [measured on 3.8].
func (t Tmux) SelectPane(socket, pane string) error {
	_, err := t.run(socket, "select-pane", "-t", pane)
	return err
}

// SwitchClient takes one terminal to a pane: its session, its window and the
// pane itself, in one command [measured on 3.8]. The client is named rather
// than left to tmux, because this program is not always run from the terminal
// that is to move.
func (t Tmux) SwitchClient(socket, client, pane string) error {
	_, err := t.run(socket, "switch-client", "-c", client, "-t", pane)
	return err
}

// Do performs one planned option write.
func (t Tmux) Do(command Command) error {
	_, err := t.run(command.Socket, command.Args...)
	return err
}

// run is `tmux -u -S <socket> …`. Both flags belong to tmux itself, not to
// the command. Without -S the server is whichever one $TMUX or the default
// socket happens to name. Without -u, a tmux whose environment names no UTF-8
// locale — the session-watcher's, started by launchd with no LANG — prints
// every control and non-ASCII character as `_`: the separator, the glyphs read
// back off the options, and any accented window name [measured on 3.8].
func (t Tmux) run(socket string, args ...string) ([]byte, error) {
	whole := append([]string{"-u", "-S", socket}, args...)
	out, err := tool.Run(t.Binary, t.Timeout, whole...)
	return out.Stdout, err
}
