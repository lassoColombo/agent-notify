package main

import (
	"strings"
	"testing"
	"time"

	agentnotify "github.com/lassoColombo/agent-notify"
	"github.com/lassoColombo/agent-notify/subscribe"
)

var nine = time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)

func session(id string, kernel agentnotify.Kernel, since time.Time) agentnotify.Record {
	return agentnotify.Record{
		Key:        agentnotify.Key{Host: "mac", Agent: "claude", SessionID: id},
		Name:       id,
		Kernel:     kernel,
		Rank:       kernel.Rank(),
		StateSince: since,
	}
}

// moved is one entry of View.Changed: a record, and the kernel it moved from.
// The SDK fills the second in from the delta, or from the session it was last
// shown when the change was spotted in a snapshot (D-63).
func moved(from agentnotify.Kernel, record agentnotify.Record) subscribe.Change {
	return subscribe.Change{Record: record, PreviousKernel: from}
}

func notifier() *Notifier {
	return NewNotifier(Preview{Lines: 4, Width: 40}, agentnotify.NewPalette(DefaultColours, nil))
}

// TestNothingMovedIsNothingToSay.
//
// This is also where the first view is dealt with, and it is worth being
// explicit that it is no longer dealt with HERE: a display that started thirty
// seconds ago must not open with a banner for every agent that happens to be
// blocked, and the guarantee that stops it is the SDK's — the first view
// carries no changes at all (D-63). This program used to keep a `seeded` flag
// for the same purpose and now keeps nothing.
func TestNothingMovedIsNothingToSay(t *testing.T) {
	if notices := notifier().Fresh(nil); len(notices) != 0 {
		t.Errorf("announced %v with nothing changed", notices)
	}
}

// TestAStateChangeIsAnnounced, with everything a banner needs on it.
func TestAStateChangeIsAnnounced(t *testing.T) {
	blocked := session("alpha", agentnotify.BlockedOnYou, nine.Add(time.Minute))
	blocked.Message = "Shall I delete the branch?"

	notices := notifier().Fresh([]subscribe.Change{moved(agentnotify.Working, blocked)})
	if len(notices) != 1 {
		t.Fatalf("got %d notices, want 1", len(notices))
	}
	if notices[0].Title != "alpha" || notices[0].Subtitle != "blocked on you" {
		t.Errorf("the notice says %q / %q", notices[0].Title, notices[0].Subtitle)
	}
	if !strings.Contains(notices[0].Body, "delete the branch") {
		t.Errorf("the body is %q", notices[0].Body)
	}
	if notices[0].Key != blocked.Key.String() {
		t.Errorf("the notice carries key %q — tapping it would focus nothing", notices[0].Key)
	}
}

// TestAMessageChangingWhileBlockedIsNotASecondBanner is the case that decides
// the shape of this file.
//
// This display asks to be woken for `message`, because the message is the body
// of the banner. So an agent that is blocked and goes on revising what it said
// arrives here over and over, every time genuinely changed. Only the kernel it
// moved from tells "it just blocked" from "it is still blocked and said
// something else" — and until D-63 the SDK decoded that and threw it away,
// which is why this program used to keep a map of timestamps instead.
func TestAMessageChangingWhileBlockedIsNotASecondBanner(t *testing.T) {
	n := notifier()
	blocked := session("alpha", agentnotify.BlockedOnYou, nine)

	if notices := n.Fresh([]subscribe.Change{moved(agentnotify.Working, blocked)}); len(notices) != 1 {
		t.Fatalf("the change that matters produced %d notices", len(notices))
	}
	for i := range 10 {
		revised := blocked
		revised.Message = strings.Repeat("thinking out loud ", i+1)
		if again := n.Fresh([]subscribe.Change{
			moved(agentnotify.BlockedOnYou, revised),
		}); len(again) != 0 {
			t.Fatalf("a message change produced a second banner: %v", again)
		}
	}
}

// TestASessionNobodyHasSeenBeforeIsAnnounced. A session core has never held
// arrives with an empty PreviousKernel, which is not the kernel it has, so it
// counts as having moved. One that starts life blocked is worth saying.
func TestASessionNobodyHasSeenBeforeIsAnnounced(t *testing.T) {
	notices := notifier().Fresh([]subscribe.Change{
		moved("", session("alpha", agentnotify.BlockedOnYou, nine)),
	})
	if len(notices) != 1 {
		t.Errorf("got %d notices, want the new session announced", len(notices))
	}
}

// TestWorkingIsNeverWorthABanner, by the same rule the bar uses.
func TestWorkingIsNeverWorthABanner(t *testing.T) {
	n := notifier()
	for _, quiet := range []agentnotify.Kernel{agentnotify.Working, agentnotify.Idle} {
		if notices := n.Fresh([]subscribe.Change{
			moved(agentnotify.BlockedOnYou, session("alpha", quiet, nine.Add(time.Hour))),
		}); len(notices) != 0 {
			t.Errorf("%s produced %v", quiet, notices)
		}
	}
}

// TestAnEndedSessionIsNotABanner (D-26). The transition to ended reaches this
// display even though it asked for no ended sessions, because that is how a bar
// learns to take a row away.
func TestAnEndedSessionIsNotABanner(t *testing.T) {
	if notices := notifier().Fresh([]subscribe.Change{
		moved(agentnotify.Working, session("alpha", agentnotify.Ended, nine.Add(time.Hour))),
	}); len(notices) != 0 {
		t.Errorf("announced %v for a session that ended", notices)
	}
}

// TestAStateThisBuildHasNeverHeardOfCanStillInterruptYou is §A5.5 reaching the
// notifier: the record carries its own rank, so a seventh state shipped by a
// newer core is judged by the number it came with rather than by the list this
// binary was compiled against.
func TestAStateThisBuildHasNeverHeardOfCanStillInterruptYou(t *testing.T) {
	future := session("alpha", agentnotify.Kernel("awaiting-approval"), nine)
	future.Rank = 45 // between broke and blocked-on-you

	notices := notifier().Fresh([]subscribe.Change{moved(agentnotify.Working, future)})
	if len(notices) != 1 {
		t.Fatalf("got %d notices, want the unknown state announced on its rank", len(notices))
	}
}

// TestTwoSessionsBothGetTheirOwn, because the identifier is the session and one
// notification must never replace another's.
func TestTwoSessionsBothGetTheirOwn(t *testing.T) {
	notices := notifier().Fresh([]subscribe.Change{
		moved(agentnotify.Working, session("alpha", agentnotify.BlockedOnYou, nine.Add(time.Minute))),
		moved(agentnotify.Working, session("beta", agentnotify.Broke, nine.Add(time.Minute))),
	})
	if len(notices) != 2 {
		t.Fatalf("got %d notices, want one each", len(notices))
	}
	if notices[0].Key == notices[1].Key {
		t.Errorf("both notices carry %q, so one would replace the other", notices[0].Key)
	}
}

// TestANoticeCarriesTheStatesColour, because what a banner is drawn in is
// decided here, with the record in hand, and nowhere else.
func TestANoticeCarriesTheStatesColour(t *testing.T) {
	notices := notifier().Fresh([]subscribe.Change{
		moved(agentnotify.Working, session("alpha", agentnotify.Broke, nine.Add(time.Minute))),
	})
	if len(notices) != 1 {
		t.Fatalf("notices = %v", notices)
	}
	if notices[0].Colour != DefaultColours[agentnotify.Broke] {
		t.Errorf("the notice is %q, want the colour broke wears on the bar", notices[0].Colour)
	}
}

// TestTheNotifierRemembersNothing is D-71 stated as a property.
//
// Handing the same change to two notifiers, and to one of them twice, must give
// the same answer every time. It is the SDK that guarantees a transition is
// reported once — "since the last call" survives a reconnection, an overflow
// and a resync, none of which this program can see — and the moment this file
// keeps a map of its own it has a second, worse answer to the same question.
func TestTheNotifierRemembersNothing(t *testing.T) {
	change := []subscribe.Change{
		moved(agentnotify.Working, session("alpha", agentnotify.BlockedOnYou, nine)),
	}
	first := notifier()
	if a, b := first.Fresh(change), first.Fresh(change); len(a) != 1 || len(b) != 1 {
		t.Errorf("the same change gave %d then %d notices", len(a), len(b))
	}
	if c := notifier().Fresh(change); len(c) != 1 {
		t.Errorf("a second notifier gave %d notices for the same change", len(c))
	}
}
