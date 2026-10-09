package session_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/lassoColombo/agent-notify/session"
)

var (
	when  = time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	later = when.Add(time.Hour)
	key   = session.Key{Host: "mac", Agent: "claude", SessionID: "abc"}
)

// TestReducerTable is plan.md §A5.7's table, as a test.
//
// Every event is exercised against a live session, an ended one, and one that
// does not exist yet, because the three columns are where the rules the table
// does not print actually live.
func TestReducerTable(t *testing.T) {
	const absent = session.Kernel("")

	cases := []struct {
		previous session.Kernel
		event    session.Event
		want     session.Kernel
		why      string
	}{
		// The table itself, from a live session mid-work.
		{session.Working, session.UserSentPrompt, session.Working, "you asked for something"},
		{session.Idle, session.UserSentPrompt, session.Working, "you asked for something"},
		{session.FinishedATurn, session.UserSentPrompt, session.Working, "answering is how a finished turn is cleared"},
		{session.BlockedOnYou, session.UserSentPrompt, session.Working, "you answered the block"},
		{session.Working, session.AgentProgressed, session.Working, "still going"},
		{session.Idle, session.AgentProgressed, session.Working, "a subagent finished; the parent is working"},
		{session.Working, session.TurnFinished, session.FinishedATurn, "your move"},
		{session.Working, session.TurnFailed, session.Broke, "the turn died"},
		{session.Working, session.TurnInterrupted, session.Idle, "you stopped it; nothing is owed and nobody needs telling"},
		{session.BlockedOnYou, session.TurnInterrupted, session.Idle, "interrupting a permission prompt answers it too"},
		{session.Working, session.BlockedOnHuman, session.BlockedOnYou, "it cannot continue"},
		{session.Working, session.SessionEnded, session.Ended, "gone"},
		{session.BlockedOnYou, session.SessionEnded, session.Ended, "gone while blocked"},

		// session-started, and its one condition: it fires on compaction and
		// on resume, so it must not disturb a session already live.
		{session.Working, session.SessionStarted, session.Working, "compaction must not clear working"},
		{session.BlockedOnYou, session.SessionStarted, session.BlockedOnYou, "compaction must not clear a block"},
		{absent, session.SessionStarted, session.Idle, "a genuinely new session"},
		{session.Ended, session.SessionStarted, session.Idle, "resume"},

		// context-changed is metadata, and the same rule.
		{session.Working, session.ContextChanged, session.Working, "metadata never moves a state"},
		{session.BlockedOnYou, session.ContextChanged, session.BlockedOnYou, "metadata never moves a state"},
		{absent, session.ContextChanged, session.Idle, "first we ever heard of it"},
		{session.Ended, session.ContextChanged, session.Idle, "any event resurrects"},

		// Resurrection is general, not a property of session-started.
		{session.Ended, session.UserSentPrompt, session.Working, "resume with no session-start hook at all"},
		{session.Ended, session.AgentProgressed, session.Working, "it is evidently alive"},
		{session.Ended, session.TurnFinished, session.FinishedATurn, "it evidently finished something"},
		{session.Ended, session.SessionEnded, session.Ended, "ending twice is still ended"},
		{session.Ended, session.TurnInterrupted, session.Idle, "an interrupt on a filed session revives it, quietly"},

		// An event from a newer adapter within the same major version.
		{session.Working, session.Event("agent-yawned"), session.Working, "never guess at an unknown event"},
		{absent, session.Event("agent-yawned"), session.Idle, "but do learn the session exists"},

		// A kernel from a newer core, read back by this one.
		{session.Kernel("hibernating"), session.SessionStarted, session.Kernel("hibernating"), "unknown kernels are live"},
		{session.Kernel("hibernating"), session.TurnFinished, session.FinishedATurn, "and still reducible"},
	}

	for _, c := range cases {
		if got := session.Reduce(c.previous, c.event); got != c.want {
			t.Errorf("Reduce(%q, %q) = %q, want %q — %s", c.previous, c.event, got, c.want, c.why)
		}
	}
}

// TestEveryEventIsInTheTable keeps the table above honest when a tenth event is
// added: the test has to be extended before the build goes green again. It did
// its job once already — `turn-interrupted` arrived with codex in M11 (D-36).
func TestEveryEventIsInTheTable(t *testing.T) {
	if len(session.Events()) != 9 {
		t.Errorf("Events() has %d entries, want the 9 of §A5.7", len(session.Events()))
	}
	for _, event := range session.Events() {
		if !event.Known() {
			t.Errorf("%q is listed by Events() but Known() denies it", event)
		}
	}
	if session.Event("agent-yawned").Known() {
		t.Error("an invented event reported itself as known")
	}
}

func TestApplyCreatesASession(t *testing.T) {
	message := "reading the plan"
	got := session.Apply(session.Record{}, session.Report{
		Key: key, Event: session.SessionStarted, Cwd: "/tmp/x", Message: &message,
	}, when)

	if got.Kernel != session.Idle || got.Rank != session.RankIdle {
		t.Errorf("kernel/rank = %q/%d, want idle/%d", got.Kernel, got.Rank, session.RankIdle)
	}
	if !got.StateSince.Equal(when) {
		t.Errorf("StateSince = %v, want %v", got.StateSince, when)
	}
	if got.Message != message || got.Cwd != "/tmp/x" {
		t.Errorf("message/cwd = %q/%q", got.Message, got.Cwd)
	}
}

// TestStateSinceSurvivesWhatIsNotAStateChange is the "waiting 12m" rule: an
// agent reporting continued progress, or metadata arriving, must not reset the
// clock a display is showing.
func TestStateSinceSurvivesWhatIsNotAStateChange(t *testing.T) {
	blocked := session.Apply(session.Record{}, session.Report{Key: key, Event: session.BlockedOnHuman, Detail: "permission-prompt"}, when)

	for _, event := range []session.Event{session.ContextChanged, session.SessionStarted, session.Event("agent-yawned")} {
		got := session.Apply(blocked, session.Report{Key: key, Event: event, Detail: "ignored"}, later)
		if !got.StateSince.Equal(when) {
			t.Errorf("%q moved StateSince to %v", event, got.StateSince)
		}
		if got.Kernel != session.BlockedOnYou {
			t.Errorf("%q moved the kernel to %q", event, got.Kernel)
		}
		if got.Detail != "permission-prompt" {
			t.Errorf("%q overwrote the detail with %q", event, got.Detail)
		}
	}
}

// TestProgressUpdatesTheDetailWithoutResettingTheClock separates the two: the
// kernel did not move, so state_since holds, but the detail describes what it is
// doing right now and must follow.
func TestProgressUpdatesTheDetailWithoutResettingTheClock(t *testing.T) {
	working := session.Apply(session.Record{}, session.Report{Key: key, Event: session.UserSentPrompt, Detail: "running-tool"}, when)
	got := session.Apply(working, session.Report{Key: key, Event: session.AgentProgressed, Detail: "compacting"}, later)

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
	working := session.Apply(session.Record{}, session.Report{Key: key, Event: session.AgentProgressed, Detail: "compacting"}, when)
	got := session.Apply(working, session.Report{Key: key, Event: session.TurnFinished}, later)
	if got.Detail != "" {
		t.Errorf("Detail = %q, want it cleared by the new state", got.Detail)
	}
}

// TestResurrectionClearsTheEnd: an event on an ended session brings it back.
func TestResurrectionClearsTheEnd(t *testing.T) {
	record := session.Record{}
	for _, event := range []session.Event{session.SessionStarted, session.UserSentPrompt, session.TurnFinished, session.SessionEnded} {
		record = session.Apply(record, session.Report{Key: key, Event: event}, when)
	}
	if record.Kernel != session.Ended {
		t.Fatalf("setup: kernel %q", record.Kernel)
	}
	if !record.EndedAt.Equal(when) {
		t.Errorf("EndedAt = %v, want %v", record.EndedAt, when)
	}

	resumed := session.Apply(record, session.Report{Key: key, Event: session.SessionStarted}, later)
	if resumed.Kernel != session.Idle {
		t.Errorf("Kernel = %q, want idle", resumed.Kernel)
	}
	if !resumed.EndedAt.IsZero() {
		t.Errorf("EndedAt = %v, want it cleared by resurrection", resumed.EndedAt)
	}
	if !resumed.StateSince.Equal(later) {
		t.Errorf("StateSince = %v, want %v — it did change state", resumed.StateSince, later)
	}
}

// TestEndedAtIsNotMovedByEndingTwice: the first end is the end.
func TestEndedAtIsNotMovedByEndingTwice(t *testing.T) {
	ended := session.Apply(session.Record{}, session.Report{Key: key, Event: session.SessionEnded, Detail: "exited"}, when)
	again := session.Apply(ended, session.Report{Key: key, Event: session.SessionEnded, Detail: "process-gone"}, later)
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
	first := session.Apply(session.Record{}, session.Report{
		Key: key, Event: session.TurnFinished, Message: &full, Cwd: "/repo",
		Model: "claude-opus-5", Process: session.Process{PID: 42, BootID: "b"},
	}, when)

	second := session.Apply(first, session.Report{Key: key, Event: session.UserSentPrompt}, later)
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
	third := session.Apply(second, session.Report{Key: key, Event: session.SessionStarted, Message: &cleared}, later)
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
	blocked := session.Apply(session.Record{}, session.Report{
		Key: key, Event: session.BlockedOnHuman, Detail: "permission-prompt", Name: "the-store",
	}, when)
	if blocked.Name != "the-store" {
		t.Fatalf("Name = %q, want the one the event carried", blocked.Name)
	}

	// An event that knows nothing about names is most events.
	quiet := session.Apply(blocked, session.Report{Key: key, Event: session.ContextChanged}, later)
	if quiet.Name != "the-store" {
		t.Errorf("Name = %q, want an event with no name to leave it alone", quiet.Name)
	}
	if !quiet.StateSince.Equal(when) {
		t.Errorf("StateSince = %v, want it unmoved at %v", quiet.StateSince, when)
	}

	// A rename mid-session, on an event that asserts nothing.
	renamed := session.Apply(quiet, session.Report{Key: key, Event: session.ContextChanged, Name: "  renamed  "}, later)
	if renamed.Name != "renamed" {
		t.Errorf("Name = %q, want the new one, cleaned", renamed.Name)
	}
	if !renamed.StateSince.Equal(when) {
		t.Errorf("StateSince = %v: naming is not a state change", renamed.StateSince)
	}
	if renamed.Kernel != session.BlockedOnYou {
		t.Errorf("Kernel = %q, want it untouched", renamed.Kernel)
	}
}

// TestApplyDoesNotMutateItsInput is what lets the store read once, apply, and
// still have the original to compare against.
func TestApplyDoesNotMutateItsInput(t *testing.T) {
	before := session.Apply(session.Record{}, session.Report{
		Key: key, Event: session.UserSentPrompt,
		CapturedContext: &session.CapturedContext{
			By:       map[string]json.RawMessage{"tmux": json.RawMessage(`{"pane":"%3"}`)},
			Ancestry: []session.Ancestor{{PID: 1, Command: "claude"}},
		},
	}, when)
	snapshot, err := json.Marshal(before)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	after := session.Apply(before, session.Report{Key: key, Event: session.TurnFinished}, later)
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
	placed := session.Apply(session.Record{}, session.Report{
		Key: key, Event: session.SessionStarted,
		CapturedContext: &session.CapturedContext{
			By: map[string]json.RawMessage{"zellij": json.RawMessage(`{"pane_env":"7"}`)},
		},
	}, when)
	placed.DerivedContext = map[string]json.RawMessage{"zellij": json.RawMessage(`{"pane":7,"tab":2}`)}
	placed.Annotations = map[string]json.RawMessage{"batch-runner": json.RawMessage(`{"task":"3 of 7"}`)}

	// An ordinary event touches neither.
	same := session.Apply(placed, session.Report{Key: key, Event: session.AgentProgressed}, later)
	if len(same.DerivedContext) != 1 {
		t.Errorf("an event that captured nothing cleared the derived context")
	}

	resumed := session.Apply(placed, session.Report{
		Key: key, Event: session.SessionStarted,
		Process: session.Process{PID: 9999},
		CapturedContext: &session.CapturedContext{
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

// TestACaptureForTheSameProcessIsMergedUnderItsOwnName is the other half of
// D-90: an integration that answers late — installed since, or failing until
// now — lands beside what is already there, and only what was derived from
// ITS old entry is void. One container failing must not cost another its
// placement on every hook.
func TestACaptureForTheSameProcessIsMergedUnderItsOwnName(t *testing.T) {
	running := session.Process{PID: 4242}
	placed := session.Apply(session.Record{}, session.Report{
		Key: key, Event: session.SessionStarted, Process: running,
		CapturedContext: &session.CapturedContext{
			Ancestry: []session.Ancestor{{PID: 4242, Command: "claude"}},
			By:       map[string]json.RawMessage{"zellij": json.RawMessage(`{"pane_env":"7"}`)},
		},
	}, when)
	placed.DerivedContext = map[string]json.RawMessage{"zellij": json.RawMessage(`{"pane":7,"tab":2}`)}

	// The window manager answered this time; zellij was not asked and is not
	// on the report.
	toppedUp := session.Apply(placed, session.Report{
		Key: key, Event: session.AgentProgressed, Process: running,
		CapturedContext: &session.CapturedContext{
			By: map[string]json.RawMessage{"aerospace": json.RawMessage(`{"title":"home | "}`)},
		},
	}, later)

	if got := string(toppedUp.CapturedContext.By["zellij"]); got != `{"pane_env":"7"}` {
		t.Errorf("zellij's capture = %s, want it kept: the process has not changed", got)
	}
	if got := string(toppedUp.CapturedContext.By["aerospace"]); got != `{"title":"home | "}` {
		t.Errorf("aerospace's capture = %s, want what it answered", got)
	}
	if got := string(toppedUp.DerivedContext["zellij"]); got != `{"pane":7,"tab":2}` {
		t.Errorf("zellij's coordinates = %s, want them kept: nothing they came from moved", got)
	}
	if len(toppedUp.CapturedContext.Ancestry) != 1 || !toppedUp.CapturedContext.CapturedAt.Equal(when) {
		t.Errorf("the snapshot's ancestry and time moved on a top-up: %+v", toppedUp.CapturedContext)
	}

	// The same integration answering again for the same process replaces its
	// own entry and voids its own coordinates, and nobody else's.
	toppedUp.DerivedContext["aerospace"] = json.RawMessage(`{"chain":[]}`)
	again := session.Apply(toppedUp, session.Report{
		Key: key, Event: session.AgentProgressed, Process: running,
		CapturedContext: &session.CapturedContext{
			By: map[string]json.RawMessage{"zellij": json.RawMessage(`{"pane_env":"8"}`)},
		},
	}, later)
	if _, still := again.DerivedContext["zellij"]; still {
		t.Error("zellij's coordinates survived zellij's own capture being replaced")
	}
	if _, kept := again.DerivedContext["aerospace"]; !kept {
		t.Error("aerospace's coordinates went with zellij's capture")
	}

	// Mutating the result must not reach the input (Clone).
	if got := string(placed.CapturedContext.By["zellij"]); got != `{"pane_env":"7"}` {
		t.Errorf("Apply's merge wrote into its input: %s", got)
	}
}
