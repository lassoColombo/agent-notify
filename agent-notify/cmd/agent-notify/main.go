// Command agent-notify is the command line half of core: a client of the same
// library an integration links, never a second implementation of anything
// (plan.md §A17 R14).
package main

import (
	"fmt"
	"os"
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
			logTheProblemBecauseTheHookMayNotSpeak(err.Error())
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "agent-notify: %v\n", err)
		os.Exit(2)
	}
}

// endTheProcessWith is how a command that has an exit code of its own ends.
//
// The three-valued answers in this system — focused says yes, no, or nobody can
// tell — are exit codes and not errors, and a RunE that returned one would have
// cobra print it as a failure. So a command with something to say beyond
// success says it here.
func endTheProcessWith(code int) {
	if code != 0 {
		os.Exit(code)
	}
}
