// The half of this display that runs inside the agent.
//
// It is one file and about thirty lines because that is the entire budget: this
// code is a child of the hook, on the path the agent is blocked on, and R1 says
// what it may do — read local state and return. It may not ask zellij anything,
// open anything, or wait for anything.
package main

import "os"

// The two variables zellij puts in every pane's environment. They exist only
// inside the agent's own process, which is why reading them is a subcommand the
// hook runs as a child of the agent rather than anything this program does once
// it is connected (§A10.3, D-27).
//
// This is the only place they are written down. They were once also in
// agent-notify's config file, as a `capture` list the install wrote and the user
// could edit; D-57 removed that, because a second copy of a fact this program
// already holds is a copy that can disagree with it, and the disagreement is
// silent — PlaceOf in render.go simply finds nothing and paints nothing.
const (
	sessionVariable = "ZELLIJ_SESSION_NAME"
	paneVariable    = "ZELLIJ_PANE_ID"
)

// Captured is what `capture-environment` returns: exactly what the environment
// said, and nothing worked out from it.
//
// Nothing here asks zellij anything. That is the rule for this half and it is
// not a style preference: it runs inside the agent's process tree, on the path
// the agent waits on, and `zellij action` against a wedged server is precisely
// what must never be there (R1, R3).
type Captured struct {
	Session string `json:"ZELLIJ_SESSION_NAME"`
	Pane    string `json:"ZELLIJ_PANE_ID"`
}

// Capture reads the two variables. It is the whole of this program's presence on
// the hook path, and it must stay that: two lookups and a return.
//
// It is a capture.Reads, handed to capture.Main by the one subcommand in main.go
// — a display answering the one question that can only be answered from inside
// the agent, without pretending to be a container to do it (D-59).
func Capture() (any, error) {
	return Captured{
		Session: os.Getenv(sessionVariable),
		Pane:    os.Getenv(paneVariable),
	}, nil
}
