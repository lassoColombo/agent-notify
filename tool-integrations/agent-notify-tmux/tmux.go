package main

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
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

// The three options this program writes. They go on panes, and only on a
// pane holding a live agent. Nothing is written on a window: what a window
// shows is looped out of its panes by tmux as it draws (`#{P:…}`), and pane
// options go wherever the pane goes, so a mark is never left behind on a
// window an agent was moved out of (D-126). An option shows nowhere until a
// format names it, which is the tmux configuration's half (README).
const (
	stateOption = "@agent-notify-state"
	glyphOption = "@agent-notify-glyph"
	nameOption  = "@agent-notify-name"
)

var options = [3]string{stateOption, glyphOption, nameOption}

// Mark is what one pane says, one field per option; empty is unset.
type Mark struct {
	State, Glyph, Name string
}

// values is the fields in the order of `options`.
func (m Mark) values() [3]string { return [3]string{m.State, m.Glyph, m.Name} }

// separator is what the formats below put between fields: the ASCII unit
// separator, which no name of ours contains (core strips control characters
// out of every line it stores), and which tmux passes through untouched — as
// long as it believes its output is UTF-8, which is what `-u` in run is for
// [measured on 3.8].
const separator = "\x1f"

// Server is one tmux server as one look found it.
type Server struct {
	// PID is the server's process.
	PID int
	// Panes is every pane, and what our options say on it.
	Panes map[string]Mark
	// Views is, for every pane, its window as each session it is linked into
	// holds it.
	Views map[string][]View
	// Clients is every terminal attached.
	Clients []Client
}

// View is a pane's window as one session holds it.
type View struct {
	Session, Window string
	// LastAttached is when a terminal was last attached to the session, in
	// seconds; zero for one nobody ever was, as a hidden session is.
	LastAttached int64
}

// Client is one terminal attached to the server.
type Client struct {
	Name    string
	Session string
	// Pane is what the terminal shows: its session's current window's active
	// pane. Empty for a client that is attached to nothing yet.
	Pane string
	// Activity is when it last did anything, in seconds.
	Activity int64
	// Focused is tmux's word that the terminal itself has the focus. tmux
	// hears it from the terminal's focus reports, which need `focus-events
	// on`; with them off it never hears the focus leave, and keeps saying
	// yes [measured on 3.8].
	Focused bool
}

// The two halves of one look. Each line says which it is, because the two
// commands answer as one output.
var (
	paneFormat = strings.Join([]string{
		"pane", "#{pid}", "#{pane_id}",
		"#{" + stateOption + "}", "#{" + glyphOption + "}", "#{" + nameOption + "}",
		"#{session_id}", "#{window_id}", "#{session_last_attached}",
	}, separator)
	clientFormat = strings.Join([]string{
		"client", "#{client_name}", "#{session_id}", "#{pane_id}", "#{client_activity}", "#{client_flags}",
	}, separator)
)

const paneFields, clientFields = 9, 6

// ErrNotThere is a server that is not running: tmux said so as it exited.
// There is nothing on it to draw or to bring, and a render that finds it so
// has not failed. Any other way a look goes wrong — tmux too slow to answer,
// or answering something that is not what it was asked — is a failure, and
// the render that met it is drawn again (D-130).
var ErrNotThere = errors.New("not there")

// Look asks a server, in one invocation, what its panes say and who is
// looking at it. The parse is the test of whether the server answered: one
// that is not running exits 1 and says so on stderr, and anything that is not
// lines of these formats is not believed, whatever the exit code.
func (t Tmux) Look(socket string) (Server, error) {
	out, err := t.run(socket, "list-panes", "-a", "-F", paneFormat, ";", "list-clients", "-F", clientFormat)
	if err != nil {
		return Server{}, notThere(socket, err)
	}
	server, err := ParseServer(out)
	if err != nil {
		return Server{}, fmt.Errorf("tmux server %q answered what it was not asked for (%q)", socket, tool.Summarise(out))
	}
	return server, nil
}

// notThere names the error of a tmux that found no server as ErrNotThere,
// and only that one. [measured on 3.8] A socket that is not there, or not a
// socket, says `error connecting to`; a socket nobody is listening on says
// `no server running on`. Anything else a running server refuses — a command
// line too long, a target it does not know — is a failure, logged and drawn
// again (D-130).
func notThere(socket string, err error) error {
	var said *tool.SaidNo
	if errors.As(err, &said) && (bytes.Contains(said.Stderr, []byte("no server running on")) ||
		bytes.Contains(said.Stderr, []byte("error connecting to"))) {
		return fmt.Errorf("tmux server %q is %w (%v)", socket, ErrNotThere, err)
	}
	return err
}

// ParseServer is the pure half of Look. A pane is listed once for every
// session its window is linked into, with the same options each time.
func ParseServer(output []byte) (Server, error) {
	server := Server{Panes: map[string]Mark{}, Views: map[string][]View{}}
	for _, line := range strings.Split(strings.TrimRight(string(output), "\n"), "\n") {
		if line == "" {
			continue
		}
		// A name of ours never holds the separator: core strips control
		// characters out of every line it stores.
		field := strings.Split(line, separator)
		switch {
		case field[0] == "pane" && len(field) == paneFields:
			pid, err := strconv.Atoi(field[1])
			if err != nil || pid <= 0 || !IsPaneID(field[2]) {
				return Server{}, fmt.Errorf("not a pane: %q", line)
			}
			server.PID = pid
			server.Panes[field[2]] = Mark{State: field[3], Glyph: field[4], Name: field[5]}
			attached, _ := strconv.ParseInt(field[8], 10, 64)
			server.Views[field[2]] = append(server.Views[field[2]],
				View{Session: field[6], Window: field[7], LastAttached: attached})
		case field[0] == "client" && len(field) == clientFields:
			if field[3] != "" && !IsPaneID(field[3]) {
				return Server{}, fmt.Errorf("not a client: %q", line)
			}
			activity, _ := strconv.ParseInt(field[4], 10, 64)
			server.Clients = append(server.Clients, Client{
				Name: field[1], Session: field[2], Pane: field[3], Activity: activity,
				Focused: slices.Contains(strings.Split(field[5], ","), "focused"),
			})
		default:
			return Server{}, fmt.Errorf("not a line of either format: %q", line)
		}
	}
	if len(server.Panes) == 0 {
		// A running server always has a session, and a session always has a
		// pane, so an empty answer is not an empty server.
		return Server{}, fmt.Errorf("no panes at all")
	}
	return server, nil
}

// Do runs one invocation, which may be several commands joined by `;`. tmux
// stops at the first that fails and runs none after it [measured on 3.8].
func (t Tmux) Do(socket string, args []string) error {
	_, err := t.run(socket, args...)
	return notThere(socket, err)
}

// run is `tmux -u -S <socket> …`. Both flags belong to tmux itself, not to
// the command. Without -S the server is whichever one $TMUX or the default
// socket happens to name. Without -u, a tmux whose environment names no UTF-8
// locale — the session-watcher's, started by launchd with no LANG — prints
// every control and non-ASCII character as `_`: the separator, and the
// glyphs read back off the options (D-123).
func (t Tmux) run(socket string, args ...string) ([]byte, error) {
	whole := append([]string{"-u", "-S", socket}, args...)
	out, err := tool.Run(t.Binary, t.Timeout, whole...)
	return out.Stdout, err
}
