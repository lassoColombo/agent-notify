// Command agent-notify is the command line half of core: a client of the same
// library an integration links, never a second implementation of anything
// (plan.md §A17 R14).
//
// It is at the root of the module because it is the root of the dependency
// graph: the one package nothing imports, sitting above everything that does
// the work. The six directories beside it that an integration may import —
// session, hook, subscribe, container, capture, tool, and the shared log — are
// the other doors into the same library, and `internal/` holds what is behind
// them.
package main

import (
	"fmt"
	"os"

	"github.com/lassoColombo/agent-notify/command/reportevent"
)

func main() {
	// ExecuteC rather than Execute, because it hands back the command that
	// failed: `report-event` is exempt from every part of this, including the
	// exit code, and asking cobra which command ran beats reading the argument
	// list a second time to guess.
	ran, err := theWholeCommandTree().ExecuteC()
	if err != nil {
		// It is called by an agent's hook, an agent reads the exit code, and a
		// hook that failed would reach back into the session it is trying to
		// describe (R2).
		if ran.Name() == "report-event" {
			reportevent.LogTheProblemBecauseTheHookMayNotSpeak(err.Error())
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "agent-notify: %v\n", err)
		os.Exit(2)
	}
}
