package main

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/lassoColombo/agent-notify/session"
)

// The two variables tmux puts in every pane's environment. This is the only
// place they are written down.
//
// `TMUX` is `<socket>,<server pid>,<session index>`, and only the socket is
// read back out of it: it is what names the server, and every invocation here
// passes it as `-S`. The session index is the session the pane was STARTED in,
// which `move-pane` and `break-pane` make stale, so the session is always asked
// of tmux rather than read off the variable. `TMUX_PANE` is `%<n>`, unique per
// server for the life of the pane, which is what makes it the coordinate.
const (
	serverVariable = "TMUX"
	paneVariable   = "TMUX_PANE"
)

// Captured is what `capture-environment` returns: exactly what the environment
// said. Nothing here asks tmux anything: this runs inside the agent's process
// tree, on the path the agent waits on (R1, R3).
type Captured struct {
	Server string `json:"TMUX"`
	Pane   string `json:"TMUX_PANE"`
}

// Capture reads the two variables. A session outside tmux captures two empty
// strings, which is not an error.
func Capture() (any, error) {
	return Captured{
		Server: os.Getenv(serverVariable),
		Pane:   os.Getenv(paneVariable),
	}, nil
}

// Place is where a session lives, as its own hook captured it: a pane of one
// tmux server.
type Place struct {
	Socket string
	Pane   string
}

// PlaceOf reads this program's captured blob off a record. Both halves have to
// be there and well formed: a socket with no pane is a process started by
// tmux that is not in a pane (a `run-shell`, a popup), and is nowhere.
func PlaceOf(record session.Record) (Place, bool) {
	blob, present := record.CapturedContext.By[Name]
	if !present {
		return Place{}, false
	}
	var captured Captured
	if err := json.Unmarshal(blob, &captured); err != nil {
		return Place{}, false
	}
	socket, found := SocketOf(captured.Server)
	if !found || !IsPaneID(captured.Pane) {
		return Place{}, false
	}
	return Place{Socket: socket, Pane: captured.Pane}, true
}

// SocketOf takes the socket path out of a `TMUX` value. It is cut from the
// right, because the two fields after it are numbers and the path is whatever
// the user's TMUX_TMPDIR made it.
func SocketOf(server string) (string, bool) {
	rest, index, found := cutLast(server, ",")
	if !found || !digits(index) {
		return "", false
	}
	socket, pid, found := cutLast(rest, ",")
	if !found || !digits(pid) || socket == "" {
		return "", false
	}
	return socket, true
}

// SessionOf is the session index out of a `TMUX` value, spelled the way tmux
// spells a session id. It is read for one purpose only — which session THIS
// program's own terminal is on — where the staleness that keeps it out of a
// coordinate does not apply: it is read the moment it is used.
func SessionOf(server string) (string, bool) {
	_, index, found := cutLast(server, ",")
	if !found || !digits(index) {
		return "", false
	}
	return "$" + index, true
}

// IsPaneID is `%` and a number, which is the only form of pane id tmux hands
// out.
func IsPaneID(pane string) bool {
	number, found := strings.CutPrefix(pane, "%")
	return found && digits(number)
}

func cutLast(s, separator string) (string, string, bool) {
	at := strings.LastIndex(s, separator)
	if at < 0 {
		return s, "", false
	}
	return s[:at], s[at+len(separator):], true
}

func digits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
