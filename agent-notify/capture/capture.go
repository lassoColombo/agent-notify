// Package capture is the one thing an integration can only do inside the agent.
//
// An agent's environment exists nowhere but the agent's own process, so the only
// program that can read it is a descendant of it — which means a child of the
// hook, and nothing else (plan.md D-27). Core therefore runs this one subcommand
// on the path the agent is waiting on, and the contract that follows from that
// is absolute: **read local state and return**. Never ask your tool anything,
// never open a socket, never wait.
//
// It is its own package because it belongs to no role. A container needs it, and
// so does a display: agent-notify-zellij-display paints a pane's title and
// cannot know which pane without it. It lived in the container SDK until D-59,
// which meant a display imported a package named for something it is not, and
// read a doc explaining a dichotomy it broke.
//
// A display adds one case to the switch it already has:
//
//	case capture.Command:
//	    os.Exit(capture.Main(Name, readTheTwoVariablesWeNeed))
//
// A container does not use this package directly. It fills in Capture on a
// container.Integration along with its other three functions, and the container
// SDK dispatches here — one implementation, so the two cannot answer differently.
package capture

import (
	"encoding/json"
	"fmt"
	"os"
)

// Command is the subcommand core runs, on every integration it can run at all.
//
// Nobody declares it and nobody is selected for it. It used to be asked of the
// integrations whose table said `capture-environment = true`, because the hook
// has no socket and cannot ask anything (D-39) — and that was a bit in the
// user's file standing in for a property of the program, with the usual
// consequence: a table that says yes for an integration that reads nothing, or
// no for one that reads something, and no error either way.
//
// Asking everybody costs what it looks like it costs and no more: the hook only
// captures when there is something new to capture, and runs all of them
// concurrently under one shared timeout when it does.
const Command = "capture-environment"

// Reads is what an integration implements. Whatever it returns is encoded as
// JSON and stored verbatim under that integration's name, opaque to core in both
// directions (R7) — so its shape is the integration's own business, and the only
// thing that has to agree about it is the code that reads it back.
type Reads func() (any, error)

// Main answers the subcommand and returns the exit code.
//
// The contract with core, in one line: one JSON object on stdout and exit 0.
// Anything else means "I could not answer", and stderr is the reason. Core never
// parses stderr and reads no meaning into which non-zero code came back — a
// program that crashed, hung or was deleted all arrive as the same thing, which
// is the only honest way to treat one that did not run.
//
// Failing is cheap and safe: the hook writes its record and exits regardless,
// this integration contributes nothing, and the next capture tries again.
func Main(name string, read Reads) int {
	if read == nil {
		// Reading nothing is an ordinary thing for an integration to do, now
		// that all of them are asked, so it is answered rather than failed:
		// an empty object, which core stores as no entry at all. Exiting 1
		// here would fill the log with a complaint about a program behaving
		// exactly as intended.
		fmt.Println("{}")
		return 0
	}
	answer, err := read()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s %s: %v\n", name, Command, err)
		return 1
	}
	encoded, err := json.Marshal(answer)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s %s: %v\n", name, Command, err)
		return 1
	}
	fmt.Println(string(encoded))
	return 0
}
