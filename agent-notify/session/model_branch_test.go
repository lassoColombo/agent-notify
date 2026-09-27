package session_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lassoColombo/agent-notify/session"
)

// The two things a session knows about itself that are not its state: what
// model it is using, and what is checked out where it is working (§A7.4.4).
//
// There were three. How much of the account's allowance was left was the
// third, and it left with the rest of what no display could be trusted to read
// (D-76).

func TestModelAndBranchAreReplacedAndNeverCleared(t *testing.T) {
	known := session.Apply(session.Record{}, session.Report{
		Key: key, Event: session.SessionStarted,
		Model: "claude-opus-5", Branch: "main",
	}, when)
	if known.Model != "claude-opus-5" || known.Branch != "main" {
		t.Fatalf("model = %q, branch = %q", known.Model, known.Branch)
	}

	// A hook that knows neither must not erase what a hook that knew both
	// established — the rule every label here keeps.
	quiet := session.Apply(known, session.Report{Key: key, Event: session.AgentProgressed}, later)
	if quiet.Model != "claude-opus-5" || quiet.Branch != "main" {
		t.Errorf("a report carrying neither cleared them: model = %q, branch = %q", quiet.Model, quiet.Branch)
	}

	// And both move when they move: a `/model` mid-session and a checkout
	// mid-session are both ordinary.
	moved := session.Apply(quiet, session.Report{
		Key: key, Event: session.AgentProgressed,
		Model: "claude-haiku-4-5", Branch: "release/2.1",
	}, later)
	if moved.Model != "claude-haiku-4-5" || moved.Branch != "release/2.1" {
		t.Errorf("model = %q, branch = %q, want both to have followed", moved.Model, moved.Branch)
	}
}

// TestNeitherMovesStateSince: a session that switched branch has not been
// waiting for a shorter time. The rule Name and the annotations already keep.
func TestNeitherMovesStateSince(t *testing.T) {
	blocked := session.Apply(session.Record{}, session.Report{Key: key, Event: session.BlockedOnHuman}, when)
	after := session.Apply(blocked, session.Report{
		Key: key, Event: session.ContextChanged,
		Model: "gpt-5.6-terra", Branch: "spike",
	}, later)

	if !after.StateSince.Equal(blocked.StateSince) {
		t.Errorf("state_since = %s, want it left at %s", after.StateSince, blocked.StateSince)
	}
}

// TestBothWakeADisplay. Unlike usage, neither moves as often as the agent
// thinks: a model and a branch change rarely and visibly, so a display that
// named no fields still wants to hear about them.
func TestBothWakeADisplay(t *testing.T) {
	before := session.Apply(session.Record{}, session.Report{Key: key, Event: session.AgentProgressed}, when)
	for _, one := range []struct {
		why    string
		report session.Report
	}{
		{"the model changed", session.Report{Key: key, Event: session.AgentProgressed, Model: "gpt-5.6-terra"}},
		{"the branch changed", session.Report{Key: key, Event: session.AgentProgressed, Branch: "spike"}},
	} {
		if !session.Differs(before, session.Apply(before, one.report, later), nil) {
			t.Errorf("%s and no display was told", one.why)
		}
	}
}

// TestNothingIsWrittenUntilThereIsSomething keeps the stored record readable by
// anybody with cat: a session in no repository must not carry an empty branch.
func TestNothingIsWrittenUntilThereIsSomething(t *testing.T) {
	record := session.Apply(session.Record{}, session.Report{Key: key, Event: session.SessionStarted}, when)
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, absent := range []string{`"branch"`, `"model"`} {
		if strings.Contains(string(encoded), absent) {
			t.Errorf("a session that knows no %s wrote one: %s", absent, encoded)
		}
	}

	full := session.Apply(record, session.Report{
		Key: key, Event: session.AgentProgressed,
		Model: "claude-opus-5", Branch: "main",
	}, later)
	encoded, err = json.Marshal(full)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, present := range []string{`"branch":"main"`, `"model":"claude-opus-5"`} {
		if !strings.Contains(string(encoded), present) {
			t.Errorf("%s is not where a reader would look for it: %s", present, encoded)
		}
	}
}
