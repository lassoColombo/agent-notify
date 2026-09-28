package session_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lassoColombo/agent-notify/session"
)

// spend is the fixture these tests count, kept short because what is being
// tested is which responses are added rather than how much each one cost.
func spend(id string, output uint64) session.Spend {
	return session.Spend{Response: id, Input: 1, Output: output, CacheRead: 10, CacheWrite: 2, Reasoning: output / 2}
}

// TestOneResponseIsCountedOnce is the measurement that decided the shape of
// this (§A7.4.3): Claude writes one transcript line per content block and
// repeats the response's whole usage on every one of them, so an adapter
// reading lines hands over the same response several times over. Counting them
// is a total twice the size of the truth.
func TestOneResponseIsCountedOnce(t *testing.T) {
	var usage session.Usage
	usage = usage.Adding([]session.Spend{spend("req-1", 100), spend("req-1", 100), spend("req-1", 100)})

	if usage.Output != 100 {
		t.Errorf("output = %d after one response written three times, want 100", usage.Output)
	}
	if usage.CountedThrough != "req-1" {
		t.Errorf("counted_through = %q, want the response just counted", usage.CountedThrough)
	}
}

// TestASecondReadOfTheSameTailCostsNothing: every hook re-reads a window that
// overlaps the last one almost entirely, and that overlap must be free.
func TestASecondReadOfTheSameTailCostsNothing(t *testing.T) {
	window := []session.Spend{spend("req-1", 10), spend("req-2", 20), spend("req-3", 30)}

	usage := session.Usage{}.Adding(window)
	again := usage.Adding(window)
	if again != usage {
		t.Errorf("re-reading the same window changed the totals: %+v then %+v", usage, again)
	}

	moved := again.Adding(append(window, spend("req-4", 40)))
	if want := usage.Output + 40; moved.Output != want {
		t.Errorf("output = %d after one new response, want %d", moved.Output, want)
	}
	if moved.CountedThrough != "req-4" {
		t.Errorf("counted_through = %q, want the newest response in the window", moved.CountedThrough)
	}
}

// TestTheCursorSurvivesBeingWrittenTwice is where the two rules meet: the
// response the last read stopped on is itself written across several lines, so
// finding the first of them and resuming after it would count the rest again.
func TestTheCursorSurvivesBeingWrittenTwice(t *testing.T) {
	first := []session.Spend{spend("req-1", 10), spend("req-1", 10), spend("req-2", 20), spend("req-2", 20)}
	usage := session.Usage{}.Adding(first)
	if usage.Output != 30 {
		t.Fatalf("output = %d on the first read, want 30", usage.Output)
	}

	usage = usage.Adding(append(first, spend("req-3", 30), spend("req-3", 30)))
	if want := uint64(60); usage.Output != want {
		t.Errorf("output = %d on the second read, want %d", usage.Output, want)
	}
}

// TestEveryCounterAccumulates: the four are disjoint and each one adds, and
// reasoning rides along as the part of output that was thinking.
func TestEveryCounterAccumulates(t *testing.T) {
	usage := session.Usage{}.Adding([]session.Spend{spend("req-1", 100), spend("req-2", 200)})

	for _, one := range []struct {
		name string
		got  uint64
		want uint64
	}{
		{"input", usage.Input, 2},
		{"output", usage.Output, 300},
		{"cache_read", usage.CacheRead, 20},
		{"cache_write", usage.CacheWrite, 4},
		{"reasoning", usage.Reasoning, 150},
	} {
		if one.got != one.want {
			t.Errorf("%s = %d, want %d", one.name, one.got, one.want)
		}
	}
}

// TestAWindowThatNoLongerReachesTheCursorAddsWhatItHolds pins the failure worth
// having. Undercounting by what fell off the end is recoverable in the sense
// that it stops; a total that counts the same response twice grows on its own
// and no later read can bring it down.
func TestAWindowThatNoLongerReachesTheCursorAddsWhatItHolds(t *testing.T) {
	usage := session.Usage{CountedThrough: "req-fell-off-the-end", Output: 1000}
	usage = usage.Adding([]session.Spend{spend("req-8", 10), spend("req-9", 20)})

	if want := uint64(1030); usage.Output != want {
		t.Errorf("output = %d, want %d — everything the window held", usage.Output, want)
	}
}

// TestAResponseNobodyCanNameIsNotCounted: an id is what makes a response
// recognisable on the next read, so one without an id is a promise to count it
// again.
func TestAResponseNobodyCanNameIsNotCounted(t *testing.T) {
	usage := session.Usage{}.Adding([]session.Spend{{Output: 500}, spend("req-1", 10)})

	if usage.Output != 10 {
		t.Errorf("output = %d, want only the response that could be named", usage.Output)
	}
}

// TestTokensAccumulateAcrossReports: spending is a running total, and an
// overlapping read of the same tail adds only what is new.
func TestTokensAccumulateAcrossReports(t *testing.T) {
	record := session.Apply(session.Record{}, session.Report{
		Key: key, Event: session.AgentProgressed,
		Spent: []session.Spend{spend("req-1", 10)},
	}, when)
	record = session.Apply(record, session.Report{
		Key: key, Event: session.AgentProgressed,
		Spent: []session.Spend{spend("req-1", 10), spend("req-2", 20)},
	}, later)

	if want := uint64(30); record.Usage.Output != want {
		t.Errorf("output = %d, want %d", record.Usage.Output, want)
	}
}

// TestAHookThatKnowsNothingAboutTokensLeavesThemAlone. Most reports carry no
// spending at all — a session ending, an event from an adapter that never
// learned to read its agent's accounting — and none of them may erase a total.
func TestAHookThatKnowsNothingAboutTokensLeavesThemAlone(t *testing.T) {
	spent := session.Apply(session.Record{}, session.Report{
		Key: key, Event: session.AgentProgressed,
		Spent: []session.Spend{spend("req-1", 10)},
	}, when)

	after := session.Apply(spent, session.Report{Key: key, Event: session.TurnFinished}, later)
	if after.Usage != spent.Usage {
		t.Errorf("usage = %+v after a report that said nothing about it, want %+v", after.Usage, spent.Usage)
	}
}

// TestSpendingDoesNotMoveStateSince is the rule Name and the annotations
// already keep: a session that has spent more has not been waiting for less
// time, and "blocked 12m" must survive every tool call in between.
func TestSpendingDoesNotMoveStateSince(t *testing.T) {
	blocked := session.Apply(session.Record{}, session.Report{Key: key, Event: session.BlockedOnHuman}, when)
	after := session.Apply(blocked, session.Report{
		Key: key, Event: session.ContextChanged,
		Spent: []session.Spend{spend("req-1", 10)},
	}, later)

	if !after.StateSince.Equal(blocked.StateSince) {
		t.Errorf("state_since = %s, want it left at %s", after.StateSince, blocked.StateSince)
	}
}

// TestSpendingAloneWakesNobody. A counter that moves on every response would
// otherwise turn a fifty-tool-call turn into fifty renders on every display,
// which is the thing Differs exists to stop (R23, §A9.3).
func TestSpendingAloneWakesNobody(t *testing.T) {
	before := session.Apply(session.Record{}, session.Report{Key: key, Event: session.AgentProgressed}, when)
	after := session.Apply(before, session.Report{
		Key: key, Event: session.AgentProgressed,
		Spent: []session.Spend{spend("req-1", 10)},
	}, later)

	if session.Differs(before, after, nil) {
		t.Error("a display that named no fields was woken by tokens alone")
	}
	if !session.Differs(before, after, []string{"usage"}) {
		t.Error("a display that asked for `usage` was not woken when it moved")
	}
}

// TestUsageIsAbsentUntilThereIsSome keeps the stored record readable: a session
// whose adapter reports no tokens must not grow an empty object.
func TestUsageIsAbsentUntilThereIsSome(t *testing.T) {
	record := session.Apply(session.Record{}, session.Report{Key: key, Event: session.SessionStarted}, when)
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if fields := string(encoded); strings.Contains(fields, `"usage"`) {
		t.Errorf("a session that has spent nothing wrote %s", fields)
	}

	spent := session.Apply(record, session.Report{
		Key: key, Event: session.AgentProgressed,
		Spent: []session.Spend{spend("req-1", 10)},
	}, later)
	encoded, err = json.Marshal(spent)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(encoded), `"usage":{"input":1,"output":10`) {
		t.Errorf("usage is not where a reader would look for it: %s", encoded)
	}
}

// TestTotalIsTheFourDisjointCounters, and not the five: reasoning is the share
// of output that was thinking, so adding it in charges for those tokens twice.
func TestTotalIsTheFourDisjointCounters(t *testing.T) {
	usage := session.Usage{Input: 1, Output: 10, CacheRead: 100, CacheWrite: 1000, Reasoning: 5}

	if want := uint64(1111); usage.Total() != want {
		t.Errorf("total = %d, want %d", usage.Total(), want)
	}
}

// TestAUsageOnlyChangeReachesWhoeverNamedItAndNobodyElse is the distinction the
// live system caught and the unit tests above could not.
//
// Tokens move on every response, so a display is woken by them only if it said
// `usage` out loud — and the one that did must actually hear about the thing it
// named. Both halves are this one comparison, which is why there is no second
// gate in front of it to answer the first half on somebody else's behalf.
func TestAUsageOnlyChangeReachesWhoeverNamedItAndNobodyElse(t *testing.T) {
	before := session.Apply(session.Record{}, session.Report{Key: key, Event: session.AgentProgressed}, when)
	spent := session.Apply(before, session.Report{
		Key: key, Event: session.AgentProgressed,
		Spent: []session.Spend{spend("req-1", 10)},
	}, later)

	if !session.Differs(before, spent, []string{"usage"}) {
		t.Error("a write that moved only tokens did not reach the display that named usage")
	}
	if session.Differs(before, spent, nil) {
		t.Error("and it must still wake nobody who did not ask")
	}
}

// TestAStampOnlyChangeWakesNobody keeps the gate a gate. A record that moved
// only its sequence is a write that means nothing to anyone, including the
// display that asked about the most volatile field there is.
func TestAStampOnlyChangeWakesNobody(t *testing.T) {
	before := session.Apply(session.Record{}, session.Report{Key: key, Event: session.AgentProgressed}, when)
	again := session.Apply(before, session.Report{Key: key, Event: session.AgentProgressed}, later)

	if again.Sequence == before.Sequence {
		t.Fatal("the fixture did not write twice")
	}
	if session.Differs(before, again, nil) {
		t.Error("a write that moved only the stamps woke somebody")
	}
	if session.Differs(before, again, []string{"usage"}) {
		t.Error("and it woke the display that named the field it did not move")
	}
}
