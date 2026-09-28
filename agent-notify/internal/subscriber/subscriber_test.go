package subscriber_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lassoColombo/agent-notify/internal/subscriber"
	"github.com/lassoColombo/agent-notify/session"
)

func shortRoot(t *testing.T) string {
	t.Helper()
	// Not t.TempDir(): on macOS its path is long enough on its own that
	// appending a socket name exceeds what a unix socket may be (§A7.2).
	root, err := os.MkdirTemp("/tmp", "an-sub")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	return root
}

func aSession(id string, kernel session.Kernel, message string) session.Record {
	return session.Record{
		Key:        session.Key{Host: "mac", Agent: "claude", SessionID: id},
		Kernel:     kernel,
		Rank:       kernel.Rank(),
		Message:    message,
		Name:       id,
		Sequence:   1,
		StateSince: time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC),
	}
}

// watched collects what a subscriber was shown, which is the only thing these
// tests assert about.
type watched struct {
	mu    sync.Mutex
	views []session.View
}

func (w *watched) record(view session.View) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.views = append(w.views, view)
	return nil
}

func (w *watched) latest() (session.View, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.views) == 0 {
		return session.View{}, false
	}
	return w.views[len(w.views)-1], true
}

// first is the opening view, which is the only one some contracts are about.
func (w *watched) first() (session.View, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.views) == 0 {
		return session.View{}, false
	}
	return w.views[0], true
}

func (w *watched) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.views)
}

func waitFor(t *testing.T, what string, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("waited for %s and it never happened", what)
}

// TestTenLineSubscriberSeesChangesLive is the first third of M9's "done when",
// and the whole argument for the fake: no agent, no store, no daemon, no
// terminal — a display under test and a script.
func TestTenLineSubscriberSeesChangesLive(t *testing.T) {
	root := shortRoot(t)
	fake, err := subscriber.StartFake(root, nil)
	if err != nil {
		t.Fatalf("StartFake: %v", err)
	}
	defer fake.Stop()

	seen := &watched{}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	// This is the ten lines.
	go subscriber.Run(ctx, subscriber.Subscription{
		Name:     "a-display",
		Root:     root,
		OnChange: seen.record,
	})

	waitFor(t, "the display to connect", func() bool { return len(fake.Connected()) == 1 })

	// The opening snapshot arrives before any delta, even when there is
	// nothing in it: a display must be correct from the moment it attaches.
	waitFor(t, "the opening snapshot", func() bool { return seen.count() >= 1 })
	first, _ := seen.latest()
	if len(first.Changed) != 0 {
		t.Errorf("the first thing a display saw reported %d change(s)", len(first.Changed))
	}

	// One at a time, and waited for. Two published in the same breath are one
	// world with two changes in it — a subscriber is sent what is true, not a
	// message per write — and this test is about the second one being named as
	// the thing that moved.
	fake.Publish(aSession("one", session.Working, "building"))
	waitFor(t, "the first session", func() bool {
		view, ok := seen.latest()
		return ok && len(view.Sessions) == 1
	})

	fake.Publish(aSession("two", session.BlockedOnYou, "may I?"))
	waitFor(t, "both sessions", func() bool {
		view, ok := seen.latest()
		return ok && len(view.Sessions) == 2
	})

	view, _ := seen.latest()
	if view.Sessions[0].Kernel != session.BlockedOnYou {
		t.Errorf("the most urgent session is not first: %q", view.Sessions[0].Kernel)
	}
	if len(view.Changed) != 1 || view.Changed[0].Record.Key.SessionID != "two" {
		t.Errorf("Changed = %v, want just the one that moved", view.Changed)
	}
}

// TestASubscriberSurvivesASessionWatcherRestart is the second third.
//
// The subscriber is never told to reconnect and never asked to. It simply is
// still correct afterwards, which is what R22 is for.
func TestASubscriberSurvivesASessionWatcherRestart(t *testing.T) {
	root := shortRoot(t)

	first, err := subscriber.StartFake(root, nil)
	if err != nil {
		t.Fatalf("StartFake: %v", err)
	}

	seen := &watched{}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go subscriber.Run(ctx, subscriber.Subscription{
		Name: "a-display", Root: root, OnChange: seen.record,
	})

	waitFor(t, "the display to connect", func() bool { return len(first.Connected()) == 1 })
	first.Publish(aSession("one", session.Working, "before the restart"))
	waitFor(t, "the first session", func() bool {
		view, ok := seen.latest()
		return ok && len(view.Sessions) == 1
	})

	// The session-watcher goes away entirely, taking the socket with it.
	if err := first.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	before := seen.count()

	// A new one, which knows nothing about the old one's connections.
	second, err := subscriber.StartFake(root, nil)
	if err != nil {
		t.Fatalf("the second StartFake: %v", err)
	}
	defer second.Stop()
	second.Publish(aSession("two", session.BlockedOnYou, "after the restart"))

	waitFor(t, "the display to come back", func() bool { return len(second.Connected()) == 1 })
	waitFor(t, "the display to be correct again", func() bool {
		view, ok := seen.latest()
		return ok && len(view.Sessions) == 1 && view.Sessions[0].Key.SessionID == "two"
	})

	view, _ := seen.latest()
	if seen.count() <= before {
		t.Error("the display was never told anything after the restart")
	}
	if view.Sessions[0].Message != "after the restart" {
		t.Errorf("it is showing %q", view.Sessions[0].Message)
	}
	// And the session the old session-watcher knew about is gone, because a
	// snapshot replaces the world rather than adding to it.
	for _, record := range view.Sessions {
		if record.Key.SessionID == "one" {
			t.Error("a session from before the restart survived the new snapshot")
		}
	}
}

// TestASubscriberThatFellBehindIsCorrectWhenItCatchesUp is the last third.
//
// A subscriber that stops reading misses worlds, and must still end up showing
// exactly what is true rather than what it happened to catch. There is nothing
// to degrade to now: every write is the whole world, asked for at the moment it
// is written, so falling behind costs pictures nobody saw and never a wrong one
// (§A9.3, R15).
func TestASubscriberThatFellBehindIsCorrectWhenItCatchesUp(t *testing.T) {
	root := shortRoot(t)
	fake, err := subscriber.StartFake(root, nil)
	if err != nil {
		t.Fatalf("StartFake: %v", err)
	}
	defer fake.Stop()

	layout, err := subscriber.FakeLayout(root)
	if err != nil {
		t.Fatalf("FakeLayout: %v", err)
	}

	// A subscriber that connects and then stops reading, which is the only way
	// to make a queue overflow on purpose.
	connection, err := net.Dial("unix", layout.SubscribersSocket())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer connection.Close()

	hello, _ := json.Marshal(session.Hello{
		Kind: session.KindHello, Name: "a-slow-display", Version: session.Version,
	})
	if _, err := connection.Write(append(hello, '\n')); err != nil {
		t.Fatalf("Write: %v", err)
	}
	waitFor(t, "the slow display to connect", func() bool { return len(fake.Connected()) == 1 })

	// Far more writes than a stopped reader can take, while it reads nothing.
	const many = 400
	for i := range many {
		fake.Publish(aSession("s"+strconv.Itoa(i), session.Working, "busy"))
	}
	// ...and then the truth changes, after the overflow.
	final := aSession("s0", session.BlockedOnYou, "the only thing that matters now")
	final.Sequence = 99
	fake.Publish(final)

	// Now it starts reading. Whatever it is sent, the last word must be right.
	reader := bufio.NewReader(connection)
	_ = connection.SetReadDeadline(time.Now().Add(10 * time.Second))

	sessions := map[string]session.Record{}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		_ = connection.SetReadDeadline(time.Now().Add(700 * time.Millisecond))
		line, err := reader.ReadBytes('\n')
		if err != nil {
			break
		}
		kind, err := session.KindOf(line)
		if err != nil {
			continue
		}
		if kind != session.KindSnapshot {
			continue
		}
		var snapshot session.Snapshot
		if err := json.Unmarshal(line, &snapshot); err != nil {
			t.Fatalf("unreadable snapshot: %v", err)
		}
		// Replaced, never merged: each one is the whole world, so the last one
		// read is the only one that has to be right.
		sessions = map[string]session.Record{}
		for _, record := range snapshot.Sessions {
			sessions[record.Key.SessionID] = record
		}
	}

	if len(sessions) != many {
		t.Errorf("it ended up holding %d sessions, want %d", len(sessions), many)
	}
	if got := sessions["s0"]; got.Kernel != session.BlockedOnYou {
		t.Errorf("s0 is %q/%q, want the state that was true when it caught up",
			got.Kernel, got.Message)
	}
}

// TestASubscriberFromAnotherVersionIsStillWelcome. Versions are announced and
// not arbitrated (D-77): everything here is built and released together, so a
// mismatch is a deployment somebody half finished, and refusing the connection
// would take the display down instead of telling them.
func TestASubscriberFromAnotherVersionIsStillWelcome(t *testing.T) {
	root := shortRoot(t)
	fake, err := subscriber.StartFake(root, nil)
	if err != nil {
		t.Fatalf("StartFake: %v", err)
	}
	defer fake.Stop()

	layout, _ := subscriber.FakeLayout(root)
	connection, err := net.Dial("unix", layout.SubscribersSocket())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer connection.Close()

	hello, _ := json.Marshal(session.Hello{
		Kind: session.KindHello, Name: "from-the-future", Version: "9.0.0",
	})
	connection.Write(append(hello, '\n'))

	_ = connection.SetReadDeadline(time.Now().Add(5 * time.Second))
	line, err := bufio.NewReader(connection).ReadBytes('\n')
	if err != nil {
		t.Fatalf("no answer at all: %v", err)
	}
	var welcome session.Welcome
	if err := json.Unmarshal(line, &welcome); err != nil {
		t.Fatalf("unreadable: %v", err)
	}
	if welcome.Kind != session.KindWelcome {
		t.Fatalf("got a %q, want a welcome", welcome.Kind)
	}
	if welcome.Version != session.Version {
		t.Errorf("the welcome says version %q, want this core's own %q",
			welcome.Version, session.Version)
	}
}

// TestWakeOnFiltersWhatArrives is R23: nothing is woken for a change it does
// not care about.
func TestWakeOnFiltersWhatArrives(t *testing.T) {
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
		Name: "a-pane-renamer", Root: root, WakeOn: []string{"kernel", "name"},
		OnChange: seen.record,
	})
	waitFor(t, "the display to connect", func() bool { return len(fake.Connected()) == 1 })
	waitFor(t, "the opening snapshot", func() bool { return seen.count() >= 1 })

	fake.Publish(aSession("one", session.Working, "first"))
	waitFor(t, "the first delta", func() bool { return seen.count() >= 2 })
	after := seen.count()

	// The message changes and nothing else. A pane renamer does not care.
	changedMessage := aSession("one", session.Working, "second")
	changedMessage.Sequence = 2
	fake.Publish(changedMessage)
	time.Sleep(300 * time.Millisecond)
	if seen.count() != after {
		t.Errorf("a pane renamer was woken for a message change (%d -> %d)", after, seen.count())
	}

	// The kernel changes. It does care.
	movedOn := aSession("one", session.FinishedATurn, "second")
	movedOn.Sequence = 3
	fake.Publish(movedOn)
	waitFor(t, "the kernel change", func() bool { return seen.count() > after })
}

func contains(text, want string) bool {
	return len(want) == 0 || len(text) >= len(want) && indexOf(text, want) >= 0
}

func indexOf(text, want string) int {
	for i := 0; i+len(want) <= len(text); i++ {
		if text[i:i+len(want)] == want {
			return i
		}
	}
	return -1
}

// TestAWakeOnNobodyCanMeetIsRefusedBeforeAnythingHappens is D-70 at the door it
// actually has to be caught at.
//
// The bounded context is deliberate: without the check, Run does not fail — it
// connects perfectly well and simply never wakes — so a regression here has to
// show up as a nil error at the deadline rather than as a test that hangs.
func TestAWakeOnNobodyCanMeetIsRefusedBeforeAnythingHappens(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	root := shortRoot(t)
	err := subscriber.Run(ctx, subscriber.Subscription{
		Name:     "misspelt",
		Root:     root,
		WakeOn:   []string{"kernel", "Kernel"},
		OnChange: func(session.View) error { return nil },
	})
	if err == nil {
		t.Fatal("Run accepted a wake-on that can never wake")
	}
	for _, want := range []string{"misspelt", "Kernel", `did you mean "kernel"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %q, want it to mention %q", err, want)
		}
	}

	// And nothing happened: no socket, no session-watcher started, nothing
	// written. The check is worth little if it fires after the side effects.
	left, readErr := os.ReadDir(root)
	if readErr != nil {
		t.Fatalf("ReadDir: %v", readErr)
	}
	for _, entry := range left {
		t.Errorf("Run created %s before refusing", entry.Name())
	}
}

// TestTheWakeOnEveryDisplayActuallyUsesIsAccepted, so that the check cannot be
// tightened into one that refuses the real thing.
func TestTheWakeOnEveryDisplayActuallyUsesIsAccepted(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := subscriber.Run(ctx, subscriber.Subscription{
		Name:     "honest",
		Root:     shortRoot(t),
		WakeOn:   []string{"kernel", "detail", "rank", "name", "cwd", "captured_context"},
		OnChange: func(session.View) error { return nil },
	})
	if err != nil {
		t.Fatalf("a real wake-on was refused: %v", err)
	}
}

// TestAnEndedSessionLeavesAViewThatDidNotAskForOne: WantEnded has to mean the
// same thing in a delta as it does in a snapshot, or every display writes this
// filter itself and D-26 is a convention rather than a mechanism.
func TestAnEndedSessionLeavesAViewThatDidNotAskForOne(t *testing.T) {
	root := shortRoot(t)
	fake, err := subscriber.StartFake(root, nil)
	if err != nil {
		t.Fatalf("StartFake: %v", err)
	}
	defer fake.Stop()

	bar, picker := &watched{}, &watched{}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go subscriber.Run(ctx, subscriber.Subscription{Name: "bar", Root: root, OnChange: bar.record})
	go subscriber.Run(ctx, subscriber.Subscription{
		Name: "picker", Root: root, WantEnded: true, OnChange: picker.record,
	})
	waitFor(t, "both to connect", func() bool { return len(fake.Connected()) == 2 })

	fake.Publish(aSession("one", session.Working, "building"))
	waitFor(t, "the bar to see it", func() bool {
		view, ok := bar.latest()
		return ok && len(view.Sessions) == 1
	})

	ended := aSession("one", session.Ended, "building")
	ended.Sequence = 2
	fake.Publish(ended)

	waitFor(t, "the bar to drop it", func() bool {
		view, ok := bar.latest()
		return ok && len(view.Sessions) == 0
	})
	waitFor(t, "the picker to keep it", func() bool {
		view, ok := picker.latest()
		return ok && len(view.Sessions) == 1 && view.Sessions[0].Kernel == session.Ended
	})
}
