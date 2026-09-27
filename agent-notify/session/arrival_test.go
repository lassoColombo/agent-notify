package session_test

import (
	"testing"
	"time"

	"github.com/lassoColombo/agent-notify/session"
)

// What is allowed to interrupt a person is the rule with the largest
// consequence in this system, and this file is the only place it is written
// down. It was three places until D-64 — a menu bar, a notification banner and
// a set of bar chips, in three repositories that never compare notes.

// TestWhatIsWorthInterruptingFor is the predicate itself, stated against every
// state rather than against an example, because the point of it is that it
// holds for the ones nobody has invented yet.
func TestWhatIsWorthInterruptingFor(t *testing.T) {
	for _, one := range []struct {
		kernel session.Kernel
		want   bool
	}{
		{session.BlockedOnYou, true},
		{session.Broke, true},
		{session.FinishedATurn, true},
		// An agent getting on with it is the state a bar is in most of the
		// day. A display that lit up for this would be lit up all day.
		{session.Working, false},
		{session.Idle, false},
		{session.Ended, false},
	} {
		record := waiting("one", one.kernel, time.Time{})
		if got := record.WantsYou(); got != one.want {
			t.Errorf("%s.WantsYou() = %v, want %v", one.kernel, got, one.want)
		}
	}
}

// TestAStateThisBuildHasNeverHeardOfCanStillWantYou is why a rank rides in
// every record (§A5.5).
//
// A seventh state, added by an agent-integration built against a later core,
// arrives here with the number that build gave it. The predicate reads that
// number rather than looking the kernel up, so the new state interrupts people
// correctly without a line changing in core or in any display.
func TestAStateThisBuildHasNeverHeardOfCanStillWantYou(t *testing.T) {
	stranger := session.Record{
		Key:    session.Key{Host: "mac", Agent: "claude", SessionID: "one"},
		Kernel: session.Kernel("needs-a-password"),
		Rank:   45, // between broke and blocked-on-you
	}
	if !stranger.Kernel.Known() {
		// The premise of the test: this build has never heard of it.
		if got := stranger.Kernel.Rank(); got != session.RankUnknown {
			t.Fatalf("the stranger's kernel is somehow known: rank %d", got)
		}
	}
	if !stranger.WantsYou() {
		t.Error("a state this build cannot name, carrying a rank above working, was ignored")
	}

	quiet := stranger
	quiet.Rank = 15 // between idle and working
	if quiet.WantsYou() {
		t.Error("a state this build cannot name, carrying a rank below working, interrupted")
	}
}

// TestNothingArrivesWhenInterruptingIsTurnedOff: a window of zero is a display
// that has been asked to stay passive, which is a thing to ask for.
func TestNothingArrivesWhenInterruptingIsTurnedOff(t *testing.T) {
	nine := time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)
	records := []session.Record{waiting("one", session.BlockedOnYou, nine)}

	for _, within := range []time.Duration{0, -time.Second} {
		if session.JustArrived(records, nine, within).Announced() {
			t.Errorf("something arrived with a window of %s", within)
		}
	}
}

// TestWhatJustHappenedIsNotWhatIsWorst is the question this answers.
//
// Somebody looking up at a display that has just changed is asking "what just
// happened", and the answer to that is not "what is worst" — that question is
// already answered by the order of everything on the screen.
func TestWhatJustHappenedIsNotWhatIsWorst(t *testing.T) {
	nine := time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)
	within := 8 * time.Second

	// The worse one has been sitting there for five seconds; the milder one
	// has just this second landed.
	records := []session.Record{
		waiting("older-and-worse", session.BlockedOnYou, nine.Add(-5*time.Second)),
		waiting("newer-and-milder", session.FinishedATurn, nine),
	}

	arrival := session.JustArrived(records, nine, within)
	if !arrival.Announced() {
		t.Fatal("nothing arrived")
	}
	if got := arrival.Record.Name; got != "newer-and-milder" {
		t.Errorf("announced %q, want the one that just happened", got)
	}
	if want := nine.Add(within); !arrival.Until.Equal(want) {
		t.Errorf("Until = %s, want %s", arrival.Until, want)
	}
}

// TestTwoInTheSameInstantAreBrokenByUrgencyThenByName: a tie has to be broken
// by something, and it has to be broken the same way twice.
//
// Without the name, two sessions blocking on the same tick make a display that
// announces a different one on every repaint — which is the same complaint
// ByUrgency exists to answer, asked about a different list.
func TestTwoInTheSameInstantAreBrokenByUrgencyThenByName(t *testing.T) {
	nine := time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)
	within := 8 * time.Second

	records := []session.Record{
		waiting("a-finished", session.FinishedATurn, nine),
		waiting("b-blocked", session.BlockedOnYou, nine),
	}
	arrival := session.JustArrived(records, nine, within)
	if !arrival.Announced() {
		t.Fatal("nothing arrived")
	}
	if got := arrival.Record.Name; got != "b-blocked" {
		t.Errorf("announced %q, want the more urgent of two in the same instant", got)
	}

	// Same instant, same rank: the name decides, and it decides the same way
	// however the list was handed over.
	tied := []session.Record{
		waiting("zebra", session.BlockedOnYou, nine),
		waiting("aardvark", session.BlockedOnYou, nine),
	}
	first := session.JustArrived(tied, nine, within)
	reversed := session.JustArrived([]session.Record{tied[1], tied[0]}, nine, within)
	if first.Record.Name != reversed.Record.Name {
		t.Errorf("the answer depends on the order of the list: %q then %q",
			first.Record.Name, reversed.Record.Name)
	}
	if got := first.Record.Name; got != "aardvark" {
		t.Errorf("announced %q, want the name that sorts first", got)
	}
}

// TestAnArrivalStopsBeingNewsOnItsOwnClock is D-47, which a day of using the
// bar taught it.
//
// The deadline is state-since plus the window, never "when I noticed". A
// display repaints for every change to every session, and a deadline measured
// from the moment of noticing would be pushed forward by every one of those —
// an announcement would then last as long as anything at all was happening.
func TestAnArrivalStopsBeingNewsOnItsOwnClock(t *testing.T) {
	nine := time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)
	within := 8 * time.Second
	records := []session.Record{waiting("one", session.BlockedOnYou, nine)}

	arrival := session.JustArrived(records, nine.Add(7*time.Second), within)
	if !arrival.Announced() {
		t.Fatal("it stopped being news a second early")
	}
	if want := nine.Add(within); !arrival.Until.Equal(want) {
		t.Errorf("Until = %s, want the record's own clock plus the window (%s)",
			arrival.Until, want)
	}

	// Seven seconds later still, and a dozen repaints in between: it is over,
	// and no amount of looking at it extends it.
	if session.JustArrived(records, nine.Add(14*time.Second), within).Announced() {
		t.Error("an announcement outlived its window")
	}
	// The instant it expires counts as expired: "until" is not inclusive, or a
	// display would repaint once more to say the same thing.
	if session.JustArrived(records, nine.Add(within), within).Announced() {
		t.Error("an announcement was still news at the moment it ran out")
	}
}

// TestAnEndedSessionNeverArrives: a session that has ended is not displayed at
// all (D-26), and it certainly does not interrupt anybody.
func TestAnEndedSessionNeverArrives(t *testing.T) {
	nine := time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)
	records := []session.Record{
		waiting("gone", session.Ended, nine),
		// And an empty record, which is what a zero value looks like: no
		// kernel at all, and a rank nobody set.
		{Key: session.Key{SessionID: "nothing"}, Rank: 99, StateSince: nine},
	}
	if arrival := session.JustArrived(records, nine, 8*time.Second); arrival.Announced() {
		t.Errorf("%q arrived, and it is not a live session", arrival.Record.Key.SessionID)
	}
}

// TestSameTellsOneAnnouncementFromAnother is how a display knows whether to
// re-arm the clock it is holding.
func TestSameTellsOneAnnouncementFromAnother(t *testing.T) {
	nine := time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)
	within := 8 * time.Second

	blocked := waiting("one", session.BlockedOnYou, nine)
	arrival := session.JustArrived([]session.Record{blocked}, nine, within)

	// The same session, repainted a second later because something else
	// entirely changed. Still the same announcement.
	again := session.JustArrived([]session.Record{blocked}, nine.Add(time.Second), within)
	if !arrival.Same(again) {
		t.Error("a repaint turned one announcement into two")
	}

	// The same session, in the same state, having entered it again — a second
	// permission prompt answered and re-asked. That is news a second time.
	afresh := blocked
	afresh.StateSince = nine.Add(3 * time.Second)
	third := session.JustArrived([]session.Record{afresh}, nine.Add(3*time.Second), within)
	if arrival.Same(third) {
		t.Error("re-entering a state was read as the announcement that was already up")
	}

	// A different session, at the same moment, is a different announcement.
	other := waiting("two", session.BlockedOnYou, nine)
	fourth := session.JustArrived([]session.Record{other}, nine, within)
	if arrival.Same(fourth) {
		t.Error("two sessions were read as one announcement")
	}
}

// TestTheZeroArrivalIsNothingToSay: a display holds one value whether or not
// anything is worth interrupting for, and the quiet case has to be the easy one
// — it is the state a bar is in most of the day.
func TestTheZeroArrivalIsNothingToSay(t *testing.T) {
	var nothing session.Arrival
	if nothing.Announced() {
		t.Error("the zero Arrival claims to be announcing something")
	}
	// Two paints with nothing to say are the same announcement, which is what
	// stops a display re-arming a clock for a change that did not happen.
	if !nothing.Same(session.Arrival{}) {
		t.Error("two empty arrivals are not the same")
	}

	nine := time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)
	real := session.JustArrived(
		[]session.Record{waiting("one", session.BlockedOnYou, nine)}, nine, 8*time.Second)
	if !real.Announced() {
		t.Fatal("nothing arrived")
	}
	if real.Same(nothing) || nothing.Same(real) {
		t.Error("an announcement and the absence of one compare as the same")
	}
}
