package main

import (
	"testing"
)

// TestEveryCommandSaysWhatItIsFor and is filed under a heading.
//
// The help is the only documentation most people will read, and cobra will
// happily print a command as a bare word under "Additional Commands" — which is
// where a command goes to be missed.
func TestEveryCommandSaysWhatItIsForAndIsGrouped(t *testing.T) {
	described := 0
	for _, command := range theWholeCommandTree().Commands() {
		switch command.Name() {
		case "help", "completion":
			// Cobra's own, and it writes their descriptions itself.
			continue
		}
		if command.Short == "" {
			t.Errorf("%q has no one-line description", command.Name())
		}
		if command.GroupID == "" {
			t.Errorf("%q is in no group, so the help files it under Additional Commands",
				command.Name())
		}
		described++
	}
	if described < 10 {
		t.Errorf("only %d commands were checked, which is fewer than this program has",
			described)
	}
}

// TestTheThingsAnAgentCallsAreNotBuriedInTheSameListAsTheThingsAPersonCalls.
// `report-event` is plumbing: it is in the help so that somebody writing an
// adapter can find it, and under its own heading so that nobody else has to
// read past it.
func TestPlumbingIsInItsOwnGroup(t *testing.T) {
	for _, command := range theWholeCommandTree().Commands() {
		if command.Name() == "report-event" && command.GroupID != groupForWhatAnAgentCalls {
			t.Errorf("report-event is filed under %q", command.GroupID)
		}
	}
}
