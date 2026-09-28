// Package subcommand runs one of an integration's subcommands and reads its
// answer.
//
// It is one function, and it is here rather than beside either of its callers
// because it has two: the hook runs `capture-environment` on the path the agent
// waits on, and the session-watcher runs a container's `interpret-environment`,
// `focus` and `focused` off it. The contract is the same either way — one JSON
// object on stdout and exit 0, anything else meaning "I could not answer" — and
// having it in one place is what keeps it so.
//
// The running itself is [tool.RunWithInput], which is the same code every
// integration uses to drive zellij or aerospace (D-68). What is left here is
// the part that is about the subcommand protocol rather than about running a
// program: something must arrive on stdin, and what comes back must be JSON.
//
// Core deliberately reads no meaning into HOW a call failed. A binary that is
// not there, one that crashed, one that hung and one that printed something
// that is not JSON all arrive as one error, because a program that did not run
// is a program that did not run (D-38). The tool package distinguishes them by
// type and core declines to look — the distinction is there for an integration
// deciding which [container.Problem] to report about its OWN tool, which is a
// different question from what core does about an integration that went quiet.
package subcommand

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/lassoColombo/agent-notify/tool"
)

// Ask runs one subcommand and returns what it printed.
//
// Everything that can go wrong arrives here as one error: a binary that is not
// there, one that crashed, one that hung, one that printed something that is
// not JSON (D-38).
func Ask(binary, command string, input json.RawMessage, timeout time.Duration) (json.RawMessage, error) {
	out, err := tool.RunWithInput(binary, timeout, input, command)
	if err != nil {
		return nil, err
	}
	answer := bytes.TrimSpace(out.Stdout)
	if !json.Valid(answer) {
		return nil, fmt.Errorf("%s %s answered %s, which is not JSON",
			binary, command, tool.Summarise(answer))
	}
	return json.RawMessage(answer), nil
}
