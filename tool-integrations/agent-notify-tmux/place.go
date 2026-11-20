package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/lassoColombo/agent-notify/session"
)

// The two variables tmux puts in every pane's environment. This is the only
// place they are written down.
//
// `TMUX` is `<socket>,<server pid>,<session index>`. The socket names the
// server, and every invocation here passes it as `-S`. The pid says which
// server: one started again on the same socket numbers its panes from %0
// again, so a pane id means something only beside it. The session index is
// the session the pane was STARTED in, which `move-pane` makes stale, and it
// is read only to find the terminal a focus was asked from (focus.go).
// `TMUX_PANE` is `%<n>`, unique per server for the life of the pane.
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
	return Captured{Server: os.Getenv(serverVariable), Pane: os.Getenv(paneVariable)}, nil
}

// Place is where a session lives: one pane of one tmux server. It is also
// the coordinates, because there is nothing to add to it that would still be
// true after the pane moved — which session and window hold it is asked of
// tmux at the moment of use (R17).
type Place struct {
	Socket string `json:"socket"`
	// Server is the server's pid.
	Server int    `json:"server"`
	Pane   string `json:"pane"`
}

// Place reads the capture. A process of tmux that is not in a pane — a
// popup, a `run-shell` — has `$TMUX` and no `$TMUX_PANE` [measured on 3.8],
// and is nowhere.
func (c Captured) Place() (Place, error) {
	if c.Server == "" {
		return Place{}, errors.New("this session was not started inside tmux")
	}
	if c.Pane == "" {
		return Place{}, errors.New("this session was started by tmux outside any pane, in a popup or a run-shell")
	}
	socket, pid, _, found := serverOf(c.Server)
	if !found {
		return Place{}, fmt.Errorf("%s was %q, which does not name a server", serverVariable, c.Server)
	}
	if !IsPaneID(c.Pane) {
		return Place{}, fmt.Errorf("%s was %q, which is not a pane id", paneVariable, c.Pane)
	}
	return Place{Socket: socket, Server: pid, Pane: c.Pane}, nil
}

// PlaceOf reads this program's capture off a record.
func PlaceOf(record session.Record) (Place, bool) {
	blob, present := record.CapturedContext.By[Name]
	if !present {
		return Place{}, false
	}
	var captured Captured
	if err := json.Unmarshal(blob, &captured); err != nil {
		return Place{}, false
	}
	place, err := captured.Place()
	return place, err == nil
}

// Interpret is the whole of placing, and asks tmux nothing: the capture
// already says which pane of which server, and anything tmux could add would
// be stale by the first `move-pane`. So it cannot fail for a server that is
// slow or busy, which would leave the session unplaced for good (D-109).
func Interpret(captured json.RawMessage) (any, error) {
	var given Captured
	if err := json.Unmarshal(captured, &given); err != nil {
		return nil, fmt.Errorf("what was captured is not this program's: %w", err)
	}
	return given.Place()
}

// placeIn reads coordinates core hands back.
func placeIn(coordinates json.RawMessage) (Place, bool) {
	var place Place
	if err := json.Unmarshal(coordinates, &place); err != nil {
		return Place{}, false
	}
	return place, place.Socket != "" && place.Server > 0 && IsPaneID(place.Pane)
}

// serverOf takes a `TMUX` value apart. It is cut from the right, because the
// two fields after the socket are numbers and the path is whatever the user's
// TMUX_TMPDIR made it.
// The session comes back spelled as tmux spells a session id.
func serverOf(value string) (socket string, pid int, sessionID string, found bool) {
	rest, index, found := cutLast(value, ",")
	if !found || !digits(index) {
		return "", 0, "", false
	}
	socket, number, found := cutLast(rest, ",")
	if !found || !digits(number) || socket == "" {
		return "", 0, "", false
	}
	pid, err := strconv.Atoi(number)
	if err != nil || pid <= 0 {
		return "", 0, "", false
	}
	return socket, pid, "$" + index, true
}

// IsPaneID is `%` and a number, the only form of pane id tmux hands out.
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
