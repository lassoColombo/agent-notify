package agentnotify_test

import (
	"encoding/json"
	"strings"
	"testing"

	an "github.com/lassoColombo/agent-notify"
)

// The two things a session knows about itself that are not its state: what
// model it is using, and what is checked out where it is working (§A7.4.4).
//
// There were three. How much of the account's allowance was left was the
// third, and it left with the rest of what no display could be trusted to read
// (D-76).

func TestModelAndBranchAreReplacedAndNeverCleared(t *testing.T) {
	known := an.Apply(an.Record{}, an.Report{
		Key: key, Event: an.SessionStarted,
		Model: "claude-opus-5", Branch: "main",
	}, when)
	if known.Model != "claude-opus-5" || known.Branch != "main" {
		t.Fatalf("model = %q, branch = %q", known.Model, known.Branch)
	}

	// A hook that knows neither must not erase what a hook that knew both
	// established — the rule every label here keeps.
	quiet := an.Apply(known, an.Report{Key: key, Event: an.AgentProgressed}, later)
	if quiet.Model != "claude-opus-5" || quiet.Branch != "main" {
		t.Errorf("a report carrying neither cleared them: model = %q, branch = %q", quiet.Model, quiet.Branch)
	}

	// And both move when they move: a `/model` mid-session and a checkout
	// mid-session are both ordinary.
	moved := an.Apply(quiet, an.Report{
		Key: key, Event: an.AgentProgressed,
		Model: "claude-haiku-4-5", Branch: "release/2.1",
	}, later)
	if moved.Model != "claude-haiku-4-5" || moved.Branch != "release/2.1" {
		t.Errorf("model = %q, branch = %q, want both to have followed", moved.Model, moved.Branch)
	}
}

// TestNeitherMovesStateSince: a session that switched branch has not been
// waiting for a shorter time. The rule Name and the annotations already keep.
func TestNeitherMovesStateSince(t *testing.T) {
	blocked := an.Apply(an.Record{}, an.Report{Key: key, Event: an.BlockedOnHuman}, when)
	after := an.Apply(blocked, an.Report{
		Key: key, Event: an.ContextChanged,
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
	before := an.Apply(an.Record{}, an.Report{Key: key, Event: an.AgentProgressed}, when)
	for _, one := range []struct {
		why    string
		report an.Report
	}{
		{"the model changed", an.Report{Key: key, Event: an.AgentProgressed, Model: "gpt-5.6-terra"}},
		{"the branch changed", an.Report{Key: key, Event: an.AgentProgressed, Branch: "spike"}},
	} {
		if !an.Differs(before, an.Apply(before, one.report, later), nil) {
			t.Errorf("%s and no display was told", one.why)
		}
	}
}

// TestNothingIsWrittenUntilThereIsSomething keeps the stored record readable by
// anybody with cat: a session in no repository must not carry an empty branch.
func TestNothingIsWrittenUntilThereIsSomething(t *testing.T) {
	record := an.Apply(an.Record{}, an.Report{Key: key, Event: an.SessionStarted}, when)
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, absent := range []string{`"branch"`, `"model"`} {
		if strings.Contains(string(encoded), absent) {
			t.Errorf("a session that knows no %s wrote one: %s", absent, encoded)
		}
	}

	full := an.Apply(record, an.Report{
		Key: key, Event: an.AgentProgressed,
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
