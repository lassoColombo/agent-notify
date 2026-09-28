package subscriber

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/lassoColombo/agent-notify/internal/paths"
	"github.com/lassoColombo/agent-notify/internal/sessionwatcher"
	"github.com/lassoColombo/agent-notify/session"
)

// Fake is a session-watcher with no agents behind it.
//
// It is the highest-leverage thing in the SDK: there will be many integrations,
// and without this every one of them is tested by hand, with a real agent, in a
// real terminal, by a person watching a bar (§A10.4).
//
// It is not a second implementation of anything. It is the real fan-out — the
// same handshake, the same world on connecting, the same one slot per
// subscriber — with a script instead of a store behind it. A display that works
// against this works against the real one, because the only difference is where
// the records came from.
type Fake struct {
	root string
	held *sessionwatcher.TheOnlyRunningWatcher
	subs *sessionwatcher.Subscribers

	mu       sync.Mutex
	sessions map[string]session.Record
}

// StartFake brings one up under root, which the integration under test points
// at with subscribe.Integration.Root or AGENT_NOTIFY_ROOT.
func StartFake(root string, logger *slog.Logger) (*Fake, error) {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	layout, err := paths.FromEnvironmentOrUnder(root)
	if err != nil {
		return nil, err
	}
	if err := layout.Create(); err != nil {
		return nil, err
	}

	held, err := sessionwatcher.TakeIfNobodyElseHasIt(layout)
	if err != nil {
		return nil, fmt.Errorf("something is already running under %s: %w", root, err)
	}

	fake := &Fake{root: root, held: held, sessions: map[string]session.Record{}}
	subs, err := sessionwatcher.Serve(layout, held, logger, fake.snapshot)
	if err != nil {
		held.Release()
		return nil, err
	}
	fake.subs = subs
	go subs.Accept()
	return fake, nil
}

// Publish plays one record, which is the whole of what a display can be told.
//
// It used to have a sibling that played the event an agent-integration reported
// alongside it, for a notifier to read. Nothing read it, here or anywhere, and
// what a notifier actually tells a transition by is the kernel it last saw.
func (f *Fake) Publish(record session.Record) {
	f.mu.Lock()
	f.sessions[record.Key.String()] = record
	f.mu.Unlock()
	f.subs.PokeEveryone()
}

// Forget plays a session being pruned.
func (f *Fake) Forget(key session.Key) {
	f.mu.Lock()
	delete(f.sessions, key.String())
	f.mu.Unlock()
	f.subs.PokeEveryone()
}

// Connected is who has joined, which is how a test waits for its display to
// arrive before playing anything at it.
func (f *Fake) Connected() []string { return f.subs.Connected() }

// Root is where it is listening, for an integration to point at.
func (f *Fake) Root() string { return f.root }

func (f *Fake) snapshot(wantEnded bool) []session.Record {
	f.mu.Lock()
	defer f.mu.Unlock()

	sessions := make([]session.Record, 0, len(f.sessions))
	for _, record := range f.sessions {
		if !wantEnded && record.Kernel == session.Ended {
			continue
		}
		sessions = append(sessions, record)
	}
	session.ByUrgency(sessions)
	return sessions
}

// Stop closes the socket and releases the lock.
func (f *Fake) Stop() error {
	err := f.subs.Close()
	if releasing := f.held.Release(); releasing != nil && err == nil {
		err = releasing
	}
	return err
}

// FakeLayout is where a Fake keeps its socket, for a test that needs the path.
func FakeLayout(root string) (paths.Layout, error) {
	return paths.FromEnvironmentOrUnder(root)
}
