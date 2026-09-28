package subscribe_test

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/lassoColombo/agent-notify/internal/config"
	"github.com/lassoColombo/agent-notify/internal/paths"
	"github.com/lassoColombo/agent-notify/internal/sessionstore"
	"github.com/lassoColombo/agent-notify/session"
	"github.com/lassoColombo/agent-notify/subscribe"
)

// aStore is a root with no session-watcher: `agent-notify-binary` names a
// program that exits at once, so Run's attempt to start one costs nothing.
func aStore(t *testing.T) (string, *sessionstore.SessionStore) {
	t.Helper()
	root := configured(t, "agent-notify-binary = \"/usr/bin/true\"\n")
	layout, err := paths.Under(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := layout.Create(); err != nil {
		t.Fatal(err)
	}
	settings, _ := config.Load(layout.ConfigFile)
	store, err := sessionstore.Open(layout, settings)
	if err != nil {
		t.Fatal(err)
	}
	return root, store
}

type shown struct {
	mu    sync.Mutex
	views []session.View
}

func (s *shown) take(view session.View) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.views = append(s.views, view)
	return nil
}

func (s *shown) all() []session.View {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]session.View(nil), s.views...)
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

func running(t *testing.T, root string, integration subscribe.Integration) *shown {
	t.Helper()
	integration.Root = root
	views := &shown{}
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- subscribe.Run(ctx, integration, views.take) }()
	t.Cleanup(func() {
		stop()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	waitFor(t, "the first view", func() bool { return len(views.all()) > 0 })
	return views
}

func report(t *testing.T, store *sessionstore.SessionStore, id string, event session.Event, message string) {
	t.Helper()
	report := session.Report{
		Key: session.Key{Host: "mac", Agent: "fake", SessionID: id}, Event: event, Name: id,
	}
	if message != "" {
		report.Message = &message
	}
	if _, err := store.Apply(report, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
}

func TestTheFirstViewCarriesNoChangesAndTheNextSaysWhatMoved(t *testing.T) {
	root, store := aStore(t)
	report(t, store, "one", session.UserSentPrompt, "")
	views := running(t, root, subscribe.Integration{Name: "test"})

	if first := views.all()[0]; len(first.Sessions) != 1 || len(first.Changed) != 0 {
		t.Fatalf("the first view carries %d session(s) and %d change(s)",
			len(first.Sessions), len(first.Changed))
	}

	report(t, store, "one", session.TurnFinished, "")
	waitFor(t, "the change", func() bool { return len(views.all()) > 1 })
	change := views.all()[1].Changed
	if len(change) != 1 || change[0].PreviousKernel != session.Working ||
		change[0].Record.Kernel != session.FinishedATurn {
		t.Errorf("changed = %+v, want working → finished-a-turn", change)
	}
}

func TestWakeOnFiltersWhatArrives(t *testing.T) {
	root, store := aStore(t)
	report(t, store, "one", session.TurnFinished, "")
	views := running(t, root, subscribe.Integration{Name: "test", WakeOn: []string{"kernel"}})

	report(t, store, "one", session.TurnFinished, "still talking")
	time.Sleep(300 * time.Millisecond)
	if got := len(views.all()); got != 1 {
		t.Errorf("shown %d views; a message is not on the wake list", got)
	}

	report(t, store, "one", session.UserSentPrompt, "")
	waitFor(t, "the kernel to move", func() bool { return len(views.all()) == 2 })
}

func TestAnEndedSessionLeavesAViewThatDidNotAskForOne(t *testing.T) {
	root, store := aStore(t)
	report(t, store, "one", session.TurnFinished, "")
	views := running(t, root, subscribe.Integration{Name: "test"})

	report(t, store, "one", session.SessionEnded, "")
	waitFor(t, "the session to leave", func() bool {
		all := views.all()
		return len(all[len(all)-1].Sessions) == 0
	})
}

func TestAnEndedSessionStaysForOneThatAsked(t *testing.T) {
	root, store := aStore(t)
	report(t, store, "one", session.TurnFinished, "")
	views := running(t, root, subscribe.Integration{Name: "test", WantEnded: true})

	report(t, store, "one", session.SessionEnded, "")
	waitFor(t, "the ended session", func() bool {
		all := views.all()
		last := all[len(all)-1]
		return len(last.Sessions) == 1 && last.Sessions[0].Kernel == session.Ended
	})
}

func TestAWakeOnNobodyCanMeetIsRefusedBeforeAnythingHappens(t *testing.T) {
	root, _ := aStore(t)
	err := subscribe.Run(context.Background(),
		subscribe.Integration{Name: "test", Root: root, WakeOn: []string{"kernal"}},
		func(session.View) error { return nil })
	if err == nil {
		t.Fatal("a misspelled wake-on was accepted")
	}
	if _, err := os.Stat(root + "/state/agent-notify.log"); err == nil {
		t.Error("it ran far enough to open the log")
	}
}
