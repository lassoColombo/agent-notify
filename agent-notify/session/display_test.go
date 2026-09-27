package session_test

import (
	"testing"
	"time"

	"github.com/lassoColombo/agent-notify/session"
)

func waiting(name string, kernel session.Kernel, since time.Time) session.Record {
	return session.Record{
		Key:        session.Key{Host: "mac", Agent: "claude", SessionID: name},
		Name:       name,
		Kernel:     kernel,
		Rank:       kernel.Rank(),
		StateSince: since,
	}
}

// TestMostUrgentIsWhatOneLightShows: a zellij tab holding four panes, an LED
// and a dock badge are the same question, and it is asked here once (R24).
func TestMostUrgentIsWhatOneLightShows(t *testing.T) {
	nine := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)

	if _, found := session.MostUrgent(nil); found {
		t.Error("an empty set has no most urgent record")
	}
	if got := session.Urgency(nil); got != session.RankUnknown {
		t.Errorf("Urgency(nil) = %d, want RankUnknown", got)
	}

	tab := []session.Record{
		waiting("one", session.Working, nine),
		waiting("two", session.BlockedOnYou, nine.Add(time.Minute)),
		waiting("three", session.Idle, nine),
	}
	highest, found := session.MostUrgent(tab)
	if !found || highest.Name != "two" {
		t.Errorf("MostUrgent picked %q, want the blocked one", highest.Name)
	}
	if got := session.Urgency(tab); got != session.RankBlockedOnYou {
		t.Errorf("Urgency = %d, want %d", got, session.RankBlockedOnYou)
	}

	// Between two sessions at the same urgency the one waiting longer wins,
	// and it wins identically here and in ByUrgency because both ask the same
	// comparison.
	both := []session.Record{
		waiting("newer", session.BlockedOnYou, nine.Add(time.Minute)),
		waiting("older", session.BlockedOnYou, nine),
	}
	highest, _ = session.MostUrgent(both)
	session.ByUrgency(both)
	if highest.Name != "older" || both[0].Name != "older" {
		t.Errorf("MostUrgent said %q and ByUrgency put %q first; they must agree, and on the "+
			"one that has been waiting longer", highest.Name, both[0].Name)
	}
}

// TestAgoRoundsDown: the number on a bar is a floor a person can trust, not a
// value that reads as longer than the truth.
func TestAgoRoundsDown(t *testing.T) {
	for _, want := range []struct {
		elapsed time.Duration
		text    string
	}{
		{0, "now"},
		{900 * time.Millisecond, "now"},
		{time.Second, "1s"},
		{59 * time.Second, "59s"},
		{119 * time.Second, "1m"},
		{59 * time.Minute, "59m"},
		{90 * time.Minute, "1h"},
		{47 * time.Hour, "1d"},
		{72 * time.Hour, "3d"},
	} {
		if got := session.Ago(want.elapsed); got != want.text {
			t.Errorf("Ago(%s) = %q, want %q", want.elapsed, got, want.text)
		}
	}
}

// TestElapsedIsNeverNegative: a record written by a machine whose clock moved,
// or by a newer binary, must not produce "-3m" on somebody's bar.
func TestElapsedIsNeverNegative(t *testing.T) {
	nine := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	record := waiting("thing", session.Working, nine)

	if got := record.Elapsed(nine.Add(-time.Hour)); got != 0 {
		t.Errorf("Elapsed from before it started = %s, want 0", got)
	}
	if got := record.Elapsed(nine.Add(90 * time.Second)); got != 90*time.Second {
		t.Errorf("Elapsed = %s, want 90s", got)
	}
	if got := (session.Record{}).Elapsed(nine); got != 0 {
		t.Errorf("a record with no state_since reported %s", got)
	}
}

// TestASessionNobodyNamedIsStillTellableApart is the guess, and it is here
// rather than in each display so that all seven make it identically (R24).
//
// The agent-integrations stopped reporting their agents' placeholders (D-75):
// Claude's `agent-notify-16` is what a session is called when nobody has named
// it, and reporting it meant no display could tell it from a name somebody
// chose. What arrives now is an empty name — and three sessions open in one
// repository would all be called `agent-notify` if the guess stopped at the
// directory.
func TestASessionNobodyNamedIsStillTellableApart(t *testing.T) {
	unnamed := func(id, cwd string) session.Record {
		return session.Record{
			Key: session.Key{Host: "mac", Agent: "claude", SessionID: id},
			Cwd: cwd,
		}
	}

	for _, one := range []struct {
		what   string
		record session.Record
		want   string
	}{
		{
			what:   "a name somebody chose is the name",
			record: session.Record{Name: "session-titles", Cwd: "/Users/x/projects/agent-notify"},
			want:   "session-titles",
		},
		{
			what:   "and nobody's is the repository, and which one it is",
			record: unnamed("8ef3a1c2-…", "/Users/x/projects/agent-notify"),
			want:   "agent-notify-8e",
		},
		{
			what:   "a session with nowhere to be is its id",
			record: unnamed("449249b6-119e-4ef0", ""),
			want:   "449249b6",
		},
		{
			what:   "and one with neither is its agent",
			record: session.Record{Key: session.Key{Agent: "claude"}},
			want:   "claude",
		},
	} {
		t.Run(one.what, func(t *testing.T) {
			if got := one.record.DisplayName(); got != one.want {
				t.Errorf("DisplayName() = %q, want %q", got, one.want)
			}
		})
	}

	// The case the suffix exists for: one repository, nobody has named either.
	first := unnamed("8ef3a1c2-1111-4111-8111-111111111111", "/Users/x/projects/agent-notify")
	second := unnamed("16a0bb7f-2222-4222-8222-222222222222", "/Users/x/projects/agent-notify")
	if first.DisplayName() == second.DisplayName() {
		t.Errorf("both sessions are called %q", first.DisplayName())
	}

	// And it never moves: DisplayName is an address as well as a label, so the
	// same record answers to the same thing whatever else is on screen.
	if again := first.DisplayName(); again != "agent-notify-8e" {
		t.Errorf("asked twice and got %q", again)
	}
}

// TestNamedIsHowADisplayTellsTheDifference: an empty Name means nobody named
// it, and that is worth asking about now that it is reliable.
func TestNamedIsHowADisplayTellsTheDifference(t *testing.T) {
	for _, one := range []struct {
		name string
		want bool
	}{
		{"session-titles", true},
		{"", false},
		{"   ", false},
	} {
		record := session.Record{Name: one.name, Cwd: "/Users/x/projects/agent-notify"}
		if got := record.Named(); got != one.want {
			t.Errorf("Named() = %v for %q, want %v", got, one.name, one.want)
		}
	}
}
