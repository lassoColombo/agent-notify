package main

import (
	"encoding/json"
	"os"
	"strconv"

	"github.com/lassoColombo/agent-notify/session"
)

// The two variables zellij puts in every pane's environment. This is the only
// place they are written down.
const (
	sessionVariable = "ZELLIJ_SESSION_NAME"
	paneVariable    = "ZELLIJ_PANE_ID"
)

// Captured is what `capture-environment` returns: exactly what the environment
// said. Nothing here asks zellij anything: this runs inside the agent's
// process tree, on the path the agent waits on (R1, R3).
type Captured struct {
	Session string `json:"ZELLIJ_SESSION_NAME"`
	Pane    string `json:"ZELLIJ_PANE_ID"`
}

// Capture reads the two variables. A session outside zellij captures two empty
// strings, which is not an error.
func Capture() (any, error) {
	return Captured{
		Session: os.Getenv(sessionVariable),
		Pane:    os.Getenv(paneVariable),
	}, nil
}

// Place is where a session lives, as its own hook captured it.
type Place struct {
	Session string
	Pane    int
}

// PlaceOf reads this program's captured blob off a record. The session name is
// the discriminator, not the pane: pane 0 is a real pane.
func PlaceOf(record session.Record) (Place, bool) {
	blob, present := record.CapturedContext.By[Name]
	if !present {
		return Place{}, false
	}
	var captured Captured
	if err := json.Unmarshal(blob, &captured); err != nil {
		return Place{}, false
	}
	pane, err := strconv.Atoi(captured.Pane)
	if captured.Session == "" || err != nil {
		return Place{}, false
	}
	return Place{Session: captured.Session, Pane: pane}, true
}
