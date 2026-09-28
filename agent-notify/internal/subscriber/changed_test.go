package subscriber_test

import (
	"context"
	"testing"

	"github.com/lassoColombo/agent-notify/internal/subscriber"
	"github.com/lassoColombo/agent-notify/session"
)

// What a subscriber is told MOVED is a different question from what it is told
// is TRUE, and this file is about the first one.
//
// The state is level-triggered and always whole, which is what makes a display
// impossible to write wrongly (R22). A notifier cannot work that way — a banner
// is posted once, at a moment — so View.Changed is the one edge in the SDK, and
// §A12.1 predicted it would be needed before anything was built. These tests
// are the contract that field makes.

// TestTheFirstViewCarriesNoChanges: "what moved since the last call" is nothing
// when there was no last call.
//
// It is the rule that lets a notifier be written without a flag of its own. The
// one in this repository has `seeded bool` precisely because this was not true,
// and its comment says why: a display that started thirty seconds ago would
// otherwise open with a banner for every agent that happens to be blocked,
// which is a restart telling you about the past.
func TestTheFirstViewCarriesNoChanges(t *testing.T) {
	root := shortRoot(t)
	fake, err := subscriber.StartFake(root, nil)
	if err != nil {
		t.Fatalf("StartFake: %v", err)
	}
	defer fake.Stop()

	// Already running before anything connects, which is the ordinary case: a
	// bar is started long after the agents it is going to show.
	fake.Publish(aSession("already", session.BlockedOnYou, "waiting for you"))

	seen := &watched{}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go subscriber.Run(ctx, subscriber.Subscription{
		Name: "a-notifier", Root: root, OnChange: seen.record,
	})

	waitFor(t, "the opening snapshot", func() bool {
		view, ok := seen.latest()
		return ok && len(view.Sessions) == 1
	})

	first, _ := seen.first()
	if len(first.Changed) != 0 {
		t.Errorf("the first view carried %d change(s): %v", len(first.Changed), first.Changed)
	}
	// The STATE is whole from the first instant, which is the other half of the
	// same contract and the reason this is not just "start empty".
	if len(first.Sessions) != 1 {
		t.Errorf("the first view carried %d session(s), want the world as it stands",
			len(first.Sessions))
	}
}

// TestAReconnectionIsNotAChange is the defect this file exists to pin.
//
// The session-watcher restarting is ordinary — `watcher reload`, a crash, a new
// version installed, a laptop opening — and every subscriber is handed a fresh
// snapshot when it does. What a subscriber holds has to survive that, or every
// session in that snapshot reads as having moved and a notifier posts a banner
// per live agent every time the daemon comes back.
func TestAReconnectionIsNotAChange(t *testing.T) {
	root := shortRoot(t)
	first, err := subscriber.StartFake(root, nil)
	if err != nil {
		t.Fatalf("StartFake: %v", err)
	}

	seen := &watched{}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go subscriber.Run(ctx, subscriber.Subscription{
		Name: "a-notifier", Root: root, OnChange: seen.record,
	})

	waitFor(t, "the display to connect", func() bool { return len(first.Connected()) == 1 })
	unmoved := aSession("steady", session.Working, "building")
	first.Reported(unmoved, session.AgentProgressed)
	waitFor(t, "the session to arrive", func() bool {
		view, ok := seen.latest()
		return ok && len(view.Sessions) == 1
	})
	before := seen.count()

	// The session-watcher goes away and comes back with the same world in it.
	if err := first.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	second, err := subscriber.StartFake(root, nil)
	if err != nil {
		t.Fatalf("the second StartFake: %v", err)
	}
	defer second.Stop()
	second.Publish(unmoved)

	waitFor(t, "the display to come back", func() bool { return len(second.Connected()) == 1 })
	waitFor(t, "a view after reconnecting", func() bool { return seen.count() > before })

	after, _ := seen.latest()
	if after.Why != "snapshot" {
		t.Fatalf("the view after reconnecting was a %q", after.Why)
	}
	if len(after.Sessions) != 1 {
		t.Errorf("the state is wrong after reconnecting: %d session(s)", len(after.Sessions))
	}
	if len(after.Changed) != 0 {
		t.Errorf("reconnecting reported %d session(s) as changed when none moved: %v",
			len(after.Changed), after.Changed)
	}
}

// TestASnapshotComparesOnlyTheFieldsAskedFor: the two ends of one field have to
// agree about what "changed" means.
//
// The session-watcher filters a delta on the subscriber's own `wake_on` before
// it sends one at all. A snapshot is diffed on this side instead, and diffing
// it on everything meant a display that declared `wake_on = ["kernel"]` — never
// woken by a new message — found one in Changed anyway if it arrived while that
// display was disconnected.
func TestASnapshotComparesOnlyTheFieldsAskedFor(t *testing.T) {
	root := shortRoot(t)
	first, err := subscriber.StartFake(root, nil)
	if err != nil {
		t.Fatalf("StartFake: %v", err)
	}

	seen := &watched{}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go subscriber.Run(ctx, subscriber.Subscription{
		Name: "a-tab-renamer", Root: root,
		WakeOn:   []string{"kernel"},
		OnChange: seen.record,
	})

	waitFor(t, "the display to connect", func() bool { return len(first.Connected()) == 1 })
	first.Publish(aSession("one", session.Working, "building"))
	waitFor(t, "the session to arrive", func() bool {
		view, ok := seen.latest()
		return ok && len(view.Sessions) == 1
	})
	before := seen.count()

	// While it is away the agent says something new. Its kernel does not move.
	if err := first.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	second, err := subscriber.StartFake(root, nil)
	if err != nil {
		t.Fatalf("the second StartFake: %v", err)
	}
	defer second.Stop()
	second.Publish(aSession("one", session.Working, "still building, now with feeling"))

	waitFor(t, "the display to come back", func() bool { return len(second.Connected()) == 1 })
	waitFor(t, "a view after reconnecting", func() bool { return seen.count() > before })

	after, _ := seen.latest()
	if len(after.Changed) != 0 {
		t.Errorf("a message-only change reached a subscriber that only asked about the "+
			"kernel: %v", after.Changed)
	}
	// The state is still whole. It is only what counts as a CHANGE that the
	// subscriber narrowed, which is the distinction R23 rests on.
	if len(after.Sessions) != 1 || after.Sessions[0].Message != "still building, now with feeling" {
		t.Errorf("the state is wrong after reconnecting: %v", after.Sessions)
	}
}

// TestAChangeCarriesWhatItMovedFrom: the transition was on the wire all along.
//
// Every delta carries the previous kernel and the event that caused it. Both
// were decoded and dropped one line before they would have been handed over,
// which is why the only notifier in the world rebuilds a weaker version of them
// from remembered timestamps.
func TestAChangeCarriesWhatItMovedFrom(t *testing.T) {
	root := shortRoot(t)
	fake, err := subscriber.StartFake(root, nil)
	if err != nil {
		t.Fatalf("StartFake: %v", err)
	}
	defer fake.Stop()

	seen := &watched{}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go subscriber.Run(ctx, subscriber.Subscription{
		Name: "a-notifier", Root: root, OnChange: seen.record,
	})

	waitFor(t, "the display to connect", func() bool { return len(fake.Connected()) == 1 })
	fake.Reported(aSession("one", session.Working, "building"), session.UserSentPrompt)
	waitFor(t, "the session to arrive", func() bool {
		view, ok := seen.latest()
		return ok && len(view.Changed) == 1
	})

	// An arrival is not a transition: there is nothing it moved from.
	arrival, _ := seen.latest()
	if got := arrival.Changed[0].PreviousKernel; got != "" {
		t.Errorf("a session nobody had seen before moved from %q", got)
	}
	if got := arrival.Changed[0].Event; got != session.UserSentPrompt {
		t.Errorf("event = %q, want the one that was reported", got)
	}

	fake.Reported(aSession("one", session.BlockedOnYou, "may I?"), session.BlockedOnHuman)
	waitFor(t, "the transition", func() bool {
		view, ok := seen.latest()
		return ok && len(view.Changed) == 1 &&
			view.Changed[0].Record.Kernel == session.BlockedOnYou
	})

	change := mostRecentChange(t, seen)
	if change.PreviousKernel != session.Working {
		t.Errorf("previous kernel = %q, want working", change.PreviousKernel)
	}
	if change.Event != session.BlockedOnHuman {
		t.Errorf("event = %q, want blocked-on-human", change.Event)
	}
	// Which is the whole point: a state that moved can be told from a change
	// that left it alone, without the subscriber remembering anything.
	if change.PreviousKernel == change.Record.Kernel {
		t.Error("the state moved and the change does not say so")
	}
}

// TestSomethingOtherThanTheStateMovingIsNotATransition: a new message on a
// session that is still working is a change a preview pane wants and a notifier
// must ignore, and the two are told apart by one comparison.
func TestSomethingOtherThanTheStateMovingIsNotATransition(t *testing.T) {
	root := shortRoot(t)
	fake, err := subscriber.StartFake(root, nil)
	if err != nil {
		t.Fatalf("StartFake: %v", err)
	}
	defer fake.Stop()

	seen := &watched{}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go subscriber.Run(ctx, subscriber.Subscription{
		Name: "a-notifier", Root: root, OnChange: seen.record,
	})

	waitFor(t, "the display to connect", func() bool { return len(fake.Connected()) == 1 })
	fake.Reported(aSession("one", session.Working, "building"), session.UserSentPrompt)
	waitFor(t, "the session to arrive", func() bool {
		view, ok := seen.latest()
		return ok && len(view.Sessions) == 1
	})

	fake.Reported(aSession("one", session.Working, "still building"), session.AgentProgressed)
	waitFor(t, "the second message", func() bool {
		view, ok := seen.latest()
		return ok && len(view.Changed) == 1 && view.Changed[0].Record.Message == "still building"
	})

	change := mostRecentChange(t, seen)
	if change.PreviousKernel != change.Record.Kernel {
		t.Errorf("a message-only change reads as a transition from %q to %q",
			change.PreviousKernel, change.Record.Kernel)
	}
}

// TestASnapshotStillSaysWhatASessionMovedFrom: a subscriber that missed the
// delta — disconnected, or its queue overflowed — still knows what it last saw,
// and that is the same fact the delta would have carried.
func TestASnapshotStillSaysWhatASessionMovedFrom(t *testing.T) {
	root := shortRoot(t)
	first, err := subscriber.StartFake(root, nil)
	if err != nil {
		t.Fatalf("StartFake: %v", err)
	}

	seen := &watched{}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go subscriber.Run(ctx, subscriber.Subscription{
		Name: "a-notifier", Root: root, OnChange: seen.record,
	})

	waitFor(t, "the display to connect", func() bool { return len(first.Connected()) == 1 })
	first.Reported(aSession("one", session.Working, "building"), session.UserSentPrompt)
	waitFor(t, "the session to arrive", func() bool {
		view, ok := seen.latest()
		return ok && len(view.Sessions) == 1
	})
	before := seen.count()

	// It blocks while nobody is listening.
	if err := first.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	second, err := subscriber.StartFake(root, nil)
	if err != nil {
		t.Fatalf("the second StartFake: %v", err)
	}
	defer second.Stop()
	second.Publish(aSession("one", session.BlockedOnYou, "may I?"))

	waitFor(t, "the display to come back", func() bool { return len(second.Connected()) == 1 })
	waitFor(t, "the change to arrive in a snapshot", func() bool {
		view, ok := seen.latest()
		return ok && seen.count() > before && len(view.Changed) == 1
	})

	after, _ := seen.latest()
	if after.Why != "snapshot" {
		t.Fatalf("this was meant to arrive in a snapshot, not a %q", after.Why)
	}
	change := after.Changed[0]
	if change.PreviousKernel != session.Working {
		t.Errorf("previous kernel = %q, want working — what this subscriber last saw",
			change.PreviousKernel)
	}
	// No event, and that is honest rather than missing: nothing told this
	// subscriber what happened, it worked out that something had.
	if change.Event != "" {
		t.Errorf("event = %q, and a snapshot is never accompanied by one", change.Event)
	}
}

// mostRecentChange is the one change in the latest view, which is what every
// test above is actually asking about.
func mostRecentChange(t *testing.T, w *watched) session.Change {
	t.Helper()
	view, ok := w.latest()
	if !ok {
		t.Fatal("no view was ever delivered")
	}
	if len(view.Changed) != 1 {
		t.Fatalf("the latest view carried %d changes, want one: %v", len(view.Changed), view.Changed)
	}
	return view.Changed[0]
}
