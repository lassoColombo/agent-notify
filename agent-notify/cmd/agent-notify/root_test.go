package main

import (
	"testing"

	agentnotify "github.com/lassoColombo/agent-notify"
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

// TestWakeOnOffersOnlyFieldsARecordHas. The list is a judgement — it leaves out
// the stamps, because waking on `sequence` is waking always — but a judgement
// is not a guess: a field renamed in the record would otherwise go on being
// offered here, and the session-watcher matches these names by string and
// silently never wakes for one that is spelled wrong.
func TestWakeOnOffersOnlyFieldsARecordHas(t *testing.T) {
	offered, _ := completeTheRecordFieldsWorthWakingFor(nil, nil, "")
	if len(offered) == 0 {
		t.Fatal("--wake-on completes to nothing")
	}
	// Asked of core rather than of reflection here, because core now refuses a
	// wake-on it does not recognise (D-70) and this list must not be able to
	// offer something the SDK would then reject.
	if err := agentnotify.ReasonTheseFieldsCannotBeWokenOn(offered); err != nil {
		t.Errorf("--wake-on offers something core refuses: %v", err)
	}
	for _, field := range offered {
		// The two stamps that mean nothing to a renderer. Either one offered
		// here is a subscriber woken by every write to render the same thing.
		//
		// `usage` is a stamp too and is deliberately not in this check: it
		// means something, it is a stamp so that it does not wake a display
		// that never asked about tokens, and naming it here is how a display
		// asks (§A7.4.3).
		if field == "sequence" || field == "updated_at" {
			t.Errorf("--wake-on offers %q, which moves on every write", field)
		}
	}
}
