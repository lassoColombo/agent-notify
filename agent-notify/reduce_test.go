package agentnotify_test

import (
	"encoding/json"
	"testing"
	"time"

	an "github.com/lassoColombo/agent-notify"
)

var (
	when  = time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	later = when.Add(time.Hour)
	key   = an.Key{Host: "mac", Agent: "claude", SessionID: "abc"}
)

// TestReducerTable is plan.md §A5.7's table, as a test.
//
// Every event is exercised against a live session, an ended one, and one that
// does not exist yet, because the three columns are where the rules the table
// does not print actually live.
func TestReducerTable(t *testing.T) {
	const absent = an.Kernel("")

	cases := []struct {
		previous an.Kernel
		event    an.Event
		want     an.Kernel
		why      string
	}{
		// The table itself, from a live session mid-work.
		{an.Working, an.UserSentPrompt, an.Working, "you asked for something"},
		{an.Idle, an.UserSentPrompt, an.Working, "you asked for something"},
		{an.FinishedATurn, an.UserSentPrompt, an.Working, "answering is how a finished turn is cleared"},
		{an.BlockedOnYou, an.UserSentPrompt, an.Working, "you answered the block"},
		{an.Working, an.AgentProgressed, an.Working, "still going"},
		{an.Idle, an.AgentProgressed, an.Working, "a subagent finished; the parent is working"},
		{an.Working, an.TurnFinished, an.FinishedATurn, "your move"},
		{an.Working, an.TurnFailed, an.Broke, "the turn died"},
		{an.Working, an.TurnInterrupted, an.Idle, "you stopped it; nothing is owed and nobody needs telling"},
		{an.BlockedOnYou, an.TurnInterrupted, an.Idle, "interrupting a permission prompt answers it too"},
		{an.Working, an.BlockedOnHuman, an.BlockedOnYou, "it cannot continue"},
		{an.Working, an.SessionEnded, an.Ended, "gone"},
		{an.BlockedOnYou, an.SessionEnded, an.Ended, "gone while blocked"},

		// session-started, and its one condition: it fires on compaction and
		// on resume, so it must not disturb a session already live.
		{an.Working, an.SessionStarted, an.Working, "compaction must not clear working"},
		{an.BlockedOnYou, an.SessionStarted, an.BlockedOnYou, "compaction must not clear a block"},
		{absent, an.SessionStarted, an.Idle, "a genuinely new session"},
		{an.Ended, an.SessionStarted, an.Idle, "resume"},

		// context-changed is metadata, and the same rule.
		{an.Working, an.ContextChanged, an.Working, "metadata never moves a state"},
		{an.BlockedOnYou, an.ContextChanged, an.BlockedOnYou, "metadata never moves a state"},
		{absent, an.ContextChanged, an.Idle, "first we ever heard of it"},
		{an.Ended, an.ContextChanged, an.Idle, "any event resurrects"},

		// Resurrection is general, not a property of session-started.
		{an.Ended, an.UserSentPrompt, an.Working, "resume with no session-start hook at all"},
		{an.Ended, an.AgentProgressed, an.Working, "it is evidently alive"},
		{an.Ended, an.TurnFinished, an.FinishedATurn, "it evidently finished something"},
		{an.Ended, an.SessionEnded, an.Ended, "ending twice is still ended"},
		{an.Ended, an.TurnInterrupted, an.Idle, "an interrupt on a filed session revives it, quietly"},

		// An event from a newer adapter within the same major version.
		{an.Working, an.Event("agent-yawned"), an.Working, "never guess at an unknown event"},
		{absent, an.Event("agent-yawned"), an.Idle, "but do learn the session exists"},

		// A kernel from a newer core, read back by this one.
		{an.Kernel("hibernating"), an.SessionStarted, an.Kernel("hibernating"), "unknown kernels are live"},
		{an.Kernel("hibernating"), an.TurnFinished, an.FinishedATurn, "and still reducible"},
	}

	for _, c := range cases {
		if got := an.Reduce(c.previous, c.event); got != c.want {
			t.Errorf("Reduce(%q, %q) = %q, want %q — %s", c.previous, c.event, got, c.want, c.why)
		}
	}
}

// TestEveryEventIsInTheTable keeps the table above honest when a tenth event is
// added: the test has to be extended before the build goes green again. It did
// its job once already — `turn-interrupted` arrived with codex in M11 (D-36).
func TestEveryEventIsInTheTable(t *testing.T) {
	if len(an.Events()) != 9 {
		t.Errorf("Events() has %d entries, want the 9 of §A5.7", len(an.Events()))
	}
	for _, event := range an.Events() {
		if !event.Known() {
			t.Errorf("%q is listed by Events() but Known() denies it", event)
		}
	}
	if an.Event("agent-yawned").Known() {
		t.Error("an invented event reported itself as known")
	}
}

func TestApplyCreatesASession(t *testing.T) {
	message := "reading the plan"
	got := an.Apply(an.Record{}, an.Report{
		Key: key, Event: an.SessionStarted, Cwd: "/tmp/x", Message: &message,
	}, when)

	if got.Key != key {
		t.Errorf("Key = %v, want %v", got.Key, key)
	}
	if got.Kernel != an.Idle || got.Rank != an.RankIdle {
		t.Errorf("kernel/rank = %q/%d, want idle/%d", got.Kernel, got.Rank, an.RankIdle)
	}
	if got.Sequence != 1 {
		t.Errorf("Sequence = %d, want 1", got.Sequence)
	}
	for name, stamp := range map[string]time.Time{
		"CreatedAt": got.CreatedAt, "UpdatedAt": got.UpdatedAt, "StateSince": got.StateSince,
	} {
		if !stamp.Equal(when) {
			t.Errorf("%s = %v, want %v", name, stamp, when)
		}
	}
	if got.Message != message || got.Cwd != "/tmp/x" {
		t.Errorf("message/cwd = %q/%q", got.Message, got.Cwd)
	}
}

// TestStateSinceSurvivesWhatIsNotAStateChange is the "waiting 12m" rule: an
// agent reporting continued progress, or metadata arriving, must not reset the
// clock a display is showing.
func TestStateSinceSurvivesWhatIsNotAStateChange(t *testing.T) {
	blocked := an.Apply(an.Record{}, an.Report{Key: key, Event: an.BlockedOnHuman, Detail: "permission-prompt"}, when)

	for _, event := range []an.Event{an.ContextChanged, an.SessionStarted, an.Event("agent-yawned")} {
		got := an.Apply(blocked, an.Report{Key: key, Event: event, Detail: "ignored"}, later)
		if !got.StateSince.Equal(when) {
			t.Errorf("%q moved StateSince to %v", event, got.StateSince)
		}
		if got.Kernel != an.BlockedOnYou {
			t.Errorf("%q moved the kernel to %q", event, got.Kernel)
		}
		if got.Detail != "permission-prompt" {
			t.Errorf("%q overwrote the detail with %q", event, got.Detail)
		}
		if !got.UpdatedAt.Equal(later) || got.Sequence != 2 {
			t.Errorf("%q was not recorded as a write: seq %d at %v", event, got.Sequence, got.UpdatedAt)
		}
	}
}

// TestProgressUpdatesTheDetailWithoutResettingTheClock separates the two: the
// kernel did not move, so state_since holds, but the detail describes what it is
// doing right now and must follow.
func TestProgressUpdatesTheDetailWithoutResettingTheClock(t *testing.T) {
	working := an.Apply(an.Record{}, an.Report{Key: key, Event: an.UserSentPrompt, Detail: "running-tool"}, when)
	got := an.Apply(working, an.Report{Key: key, Event: an.AgentProgressed, Detail: "compacting"}, later)

	if got.Detail != "compacting" {
		t.Errorf("Detail = %q, want compacting", got.Detail)
	}
	if !got.StateSince.Equal(when) {
		t.Errorf("StateSince = %v, want it unmoved at %v", got.StateSince, when)
	}
}

// TestDetailDoesNotOutliveItsState: a state change with no detail clears the
// old one rather than carrying "compacting" into a finished turn.
func TestDetailDoesNotOutliveItsState(t *testing.T) {
	working := an.Apply(an.Record{}, an.Report{Key: key, Event: an.AgentProgressed, Detail: "compacting"}, when)
	got := an.Apply(working, an.Report{Key: key, Event: an.TurnFinished}, later)
	if got.Detail != "" {
		t.Errorf("Detail = %q, want it cleared by the new state", got.Detail)
	}
}

// TestSequenceContinuesAcrossResurrection is the second of the three rules the
// table does not show. A display must never see a sequence go backwards.
func TestSequenceContinuesAcrossResurrection(t *testing.T) {
	record := an.Record{}
	for _, event := range []an.Event{an.SessionStarted, an.UserSentPrompt, an.TurnFinished, an.SessionEnded} {
		record = an.Apply(record, an.Report{Key: key, Event: event}, when)
	}
	if record.Kernel != an.Ended || record.Sequence != 4 {
		t.Fatalf("setup: kernel %q at sequence %d", record.Kernel, record.Sequence)
	}
	if !record.EndedAt.Equal(when) {
		t.Errorf("EndedAt = %v, want %v", record.EndedAt, when)
	}

	resumed := an.Apply(record, an.Report{Key: key, Event: an.SessionStarted}, later)
	if resumed.Sequence != 5 {
		t.Errorf("Sequence = %d, want 5 — it must never restart", resumed.Sequence)
	}
	if resumed.Kernel != an.Idle {
		t.Errorf("Kernel = %q, want idle", resumed.Kernel)
	}
	if !resumed.EndedAt.IsZero() {
		t.Errorf("EndedAt = %v, want it cleared by resurrection", resumed.EndedAt)
	}
	if !resumed.CreatedAt.Equal(when) {
		t.Errorf("CreatedAt = %v, want the original %v — a resumed session is not a new one",
			resumed.CreatedAt, when)
	}
	if !resumed.StateSince.Equal(later) {
		t.Errorf("StateSince = %v, want %v — it did change state", resumed.StateSince, later)
	}
}

// TestEndedAtIsNotMovedByEndingTwice: the first end is the end.
func TestEndedAtIsNotMovedByEndingTwice(t *testing.T) {
	ended := an.Apply(an.Record{}, an.Report{Key: key, Event: an.SessionEnded, Detail: "exited"}, when)
	again := an.Apply(ended, an.Report{Key: key, Event: an.SessionEnded, Detail: "process-gone"}, later)
	if !again.EndedAt.Equal(when) {
		t.Errorf("EndedAt = %v, want the first end at %v", again.EndedAt, when)
	}
	if again.Detail != "process-gone" {
		t.Errorf("Detail = %q, want the newer reason", again.Detail)
	}
}

// TestAbsentFieldsLeaveWhatIsThere: a hook that knows less must not erase what
// a hook that knew more established.
func TestAbsentFieldsLeaveWhatIsThere(t *testing.T) {
	full := "the full answer"
	first := an.Apply(an.Record{}, an.Report{
		Key: key, Event: an.TurnFinished, Message: &full, Cwd: "/repo",
		Model: "claude-opus-5", Process: an.Process{PID: 42, BootID: "b"},
	}, when)

	second := an.Apply(first, an.Report{Key: key, Event: an.UserSentPrompt}, later)
	if second.Message != full {
		t.Errorf("Message = %q, want it kept", second.Message)
	}
	if second.Cwd != "/repo" {
		t.Errorf("Cwd = %q, want it kept", second.Cwd)
	}
	if second.Model != "claude-opus-5" {
		t.Errorf("Model = %q, want it kept", second.Model)
	}
	if second.Process.PID != 42 {
		t.Errorf("Process.PID = %d, want it kept", second.Process.PID)
	}

	cleared := ""
	third := an.Apply(second, an.Report{Key: key, Event: an.SessionStarted, Message: &cleared}, later)
	if third.Message != "" {
		t.Errorf("Message = %q, want an explicit empty to clear it", third.Message)
	}
}

// TestANameRidesOnAnEventWithoutBeingOne is §A7.4.2 after D-60: the name comes
// from the agent now, so it arrives on whatever event the adapter was sending
// anyway — and it must behave like a label the whole way. It never moves the
// clock, an event that does not carry one never clears it, and the last one to
// say something wins.
func TestANameRidesOnAnEventWithoutBeingOne(t *testing.T) {
	blocked := an.Apply(an.Record{}, an.Report{
		Key: key, Event: an.BlockedOnHuman, Detail: "permission-prompt", Name: "the-store",
	}, when)
	if blocked.Name != "the-store" {
		t.Fatalf("Name = %q, want the one the event carried", blocked.Name)
	}

	// An event that knows nothing about names is most events.
	quiet := an.Apply(blocked, an.Report{Key: key, Event: an.ContextChanged}, later)
	if quiet.Name != "the-store" {
		t.Errorf("Name = %q, want an event with no name to leave it alone", quiet.Name)
	}
	if !quiet.StateSince.Equal(when) {
		t.Errorf("StateSince = %v, want it unmoved at %v", quiet.StateSince, when)
	}

	// A rename mid-session, on an event that asserts nothing.
	renamed := an.Apply(quiet, an.Report{Key: key, Event: an.ContextChanged, Name: "  renamed  "}, later)
	if renamed.Name != "renamed" {
		t.Errorf("Name = %q, want the new one, cleaned", renamed.Name)
	}
	if !renamed.StateSince.Equal(when) {
		t.Errorf("StateSince = %v: naming is not a state change", renamed.StateSince)
	}
	if renamed.Kernel != an.BlockedOnYou {
		t.Errorf("Kernel = %q, want it untouched", renamed.Kernel)
	}
}

// TestApplyDoesNotMutateItsInput is what lets the store read once, apply, and
// still have the original to compare against.
func TestApplyDoesNotMutateItsInput(t *testing.T) {
	before := an.Apply(an.Record{}, an.Report{
		Key: key, Event: an.UserSentPrompt,
		CapturedContext: &an.CapturedContext{
			By:       map[string]json.RawMessage{"tmux": json.RawMessage(`{"pane":"%3"}`)},
			Ancestry: []an.Ancestor{{PID: 1, Command: "claude"}},
		},
	}, when)
	snapshot, err := json.Marshal(before)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	after := an.Apply(before, an.Report{Key: key, Event: an.TurnFinished}, later)
	after.CapturedContext.By["tmux"] = json.RawMessage(`{"pane":"%99"}`)

	again, err := json.Marshal(before)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(again) != string(snapshot) {
		t.Errorf("Apply's result shares state with its input:\n before %s\n after  %s", snapshot, again)
	}
}

// TestReplacingTheCapturedContextVoidsWhatWasDerivedFromIt is the rule that
// closes Q15 (D-27).
//
// A pane id worked out from an environment that no longer applies is not merely
// out of date. Pane 7 now belongs to somebody else, and a display that focuses
// it takes you confidently to the wrong place — which §A11.3 calls worse than
// not being able to go at all.
func TestReplacingTheCapturedContextVoidsWhatWasDerivedFromIt(t *testing.T) {
	placed := an.Apply(an.Record{}, an.Report{
		Key: key, Event: an.SessionStarted,
		CapturedContext: &an.CapturedContext{
			By: map[string]json.RawMessage{"zellij": json.RawMessage(`{"pane_env":"7"}`)},
		},
	}, when)
	placed.DerivedContext = map[string]json.RawMessage{"zellij": json.RawMessage(`{"pane":7,"tab":2}`)}
	placed.Annotations = map[string]json.RawMessage{"batch-runner": json.RawMessage(`{"task":"3 of 7"}`)}

	// An ordinary event touches neither.
	same := an.Apply(placed, an.Report{Key: key, Event: an.AgentProgressed}, later)
	if len(same.DerivedContext) != 1 {
		t.Errorf("an event that captured nothing cleared the derived context")
	}

	resumed := an.Apply(placed, an.Report{
		Key: key, Event: an.SessionStarted,
		CapturedContext: &an.CapturedContext{
			By: map[string]json.RawMessage{"zellij": json.RawMessage(`{"pane_env":"12"}`)},
		},
	}, later)

	if len(resumed.DerivedContext) != 0 {
		t.Errorf("DerivedContext = %v, want it void: it was worked out from an environment "+
			"that no longer applies", resumed.DerivedContext)
	}
	if len(resumed.Annotations) != 1 {
		t.Errorf("Annotations = %v, want them kept: nobody asked for them, so nothing we "+
			"did invalidates them", resumed.Annotations)
	}
	if !resumed.CapturedContext.CapturedAt.Equal(later) {
		t.Errorf("CapturedAt = %v, want %v", resumed.CapturedContext.CapturedAt, later)
	}
}
