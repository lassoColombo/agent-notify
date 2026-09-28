// Package capture is the one thing an integration can only do inside the agent:
// read its environment, which exists nowhere but the agent's own process, so
// only a child of the hook can (plan.md D-27). The contract that follows is
// absolute: read local state and return. Never ask your tool anything, never
// open a socket, never wait.
package capture

import (
	"encoding/json"
	"fmt"
	"os"
)

// Command is the subcommand core runs on every integration it can run at all.
// Nobody declares it; one that reads nothing answers an empty object.
const Command = "capture-environment"

// Reads is what an integration implements. Whatever it returns is stored
// verbatim under that integration's name, opaque to core (R7).
type Reads func() (any, error)

// Main answers the subcommand: one JSON object on stdout and exit 0, or the
// reason on stderr and exit 1, which costs the hook nothing (R13).
func Main(name string, read Reads) int {
	if read == nil {
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
