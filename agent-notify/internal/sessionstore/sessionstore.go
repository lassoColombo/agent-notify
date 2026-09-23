// Package store is the only thing that writes a record.
//
// It is the truth of the system (R4). Everything that crosses a socket is a
// hint that may be lost without consequence, and a subscriber that missed one
// re-reads from here and is correct. That is what lets the sockets have no
// delivery guarantee, overflow be handled by dropping, and a session-watcher
// restart cost nothing.
package sessionstore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	agentnotify "github.com/lassoColombo/agent-notify"
	"github.com/lassoColombo/agent-notify/internal/config"
	"github.com/lassoColombo/agent-notify/internal/paths"
)

// SessionStore is every session's record, in files under one root.
type SessionStore struct {
	layout   paths.Layout
	patience time.Duration
	bounds   agentnotify.Bounds
	keep     time.Duration
}

// LockPatience bounds how long a writer waits for another writer to finish with
// the same session (plan.md §A7.3). On expiry the write is abandoned:
// record-agent-event logs and exits 0, because failing the hook is never an
// option (R2) and the next event re-establishes the state anyway.
//
// It is a var and it is exported only so that the test which deliberately holds
// a lock can shorten the wait it is measuring. Nothing at runtime writes it,
// and no user can — this package is internal.
var LockPatience = 2 * time.Second

// Open prepares the store, creating the directories if they are not there.
func Open(layout paths.Layout, settings config.Config) (*SessionStore, error) {
	if err := layout.Create(); err != nil {
		return nil, err
	}
	return &SessionStore{
		layout:   layout,
		patience: LockPatience,
		bounds:   agentnotify.Bounds{Messages: settings.HistoryMessages, Changes: settings.HistoryChanges},
		keep:     settings.KeepEndedSessions,
	}, nil
}

// Read returns the record for a key, whether it is live or ended.
//
// It takes no lock. A reader that catches a writer mid-flight sees either the
// old record or the new one, never a mixture, because a record is replaced by
// rename and never edited in place.
func (s *SessionStore) Read(key agentnotify.Key) (agentnotify.Record, bool, error) {
	live, ended := s.theLiveAndEndedFilesFor(key)

	var liveRecord, endedRecord agentnotify.Record
	liveFound, liveErr := readJSON(live, &liveRecord)
	endedFound, endedErr := readJSON(ended, &endedRecord)
	if err := errors.Join(liveErr, endedErr); err != nil {
		return agentnotify.Record{}, false, err
	}

	switch {
	case liveFound && endedFound:
		// Both exist only if a process died between writing the destination
		// and removing the source. The sequence says which one is real: it is
		// monotonic per session and never restarts, so the higher number is
		// always the later write (R16). The next write removes the loser.
		if endedRecord.Sequence > liveRecord.Sequence {
			return endedRecord, true, nil
		}
		return liveRecord, true, nil
	case liveFound:
		return liveRecord, true, nil
	case endedFound:
		return endedRecord, true, nil
	}
	return agentnotify.Record{}, false, nil
}

// theLiveAndEndedFilesFor is the two files a session's record can be in, and
// there are never any others.
func (s *SessionStore) theLiveAndEndedFilesFor(key agentnotify.Key) (live, ended string) {
	return s.layout.SessionFile(key.String()), s.layout.EndedFile(key.String())
}

// Apply is the write path: lock, read, reduce, stamp, write, unlock.
//
// The whole sequence happens under the session's lock, so two hooks firing
// together for one session serialise rather than one overwriting the other's
// read. Different sessions never contend.
func (s *SessionStore) Apply(
	report agentnotify.Report, now time.Time,
) (agentnotify.Record, error) {
	return s.Update(report.Key, now, func(previous agentnotify.Record) agentnotify.Record {
		return agentnotify.Apply(previous, report, now)
	})
}

// Update is Apply for a change that is not an event: naming a session,
// annotating it, recording what an integration derived.
//
// The change function receives the record as it is on disk and returns what it
// should become. It runs under the lock, so it may read anything in the record
// and rely on it still being true when it returns. The store stamps `sequence`,
// `updated_at`, `schema` and `key` afterwards, unconditionally — a caller never
// gets to decide those (§A7.3).
func (s *SessionStore) Update(
	key agentnotify.Key,
	now time.Time,
	change func(previous agentnotify.Record) agentnotify.Record,
) (agentnotify.Record, error) {
	if err := key.ReasonThisKeyCannotBeUsed(); err != nil {
		return agentnotify.Record{}, err
	}

	held, err := lock(s.layout.LockFile(key.String()), s.patience)
	if err != nil {
		return agentnotify.Record{}, err
	}
	defer unlock(held)

	previous, _, err := s.Read(key)
	if err != nil {
		return agentnotify.Record{}, err
	}

	next := change(previous)
	next.Key = key
	next.Sequence = previous.Sequence + 1
	next.UpdatedAt = now
	if next.CreatedAt.IsZero() {
		next.CreatedAt = now
	}

	destination := s.fileFor(next)
	if err := writeJSON(destination, next); err != nil {
		return agentnotify.Record{}, err
	}
	// Written before anything is removed, so a crash in between leaves two
	// files rather than none — and Read resolves two by sequence, while nothing
	// resolves zero. Removing whichever place is not the destination covers
	// both the ordinary move and the leftover of somebody else's crash.
	live, ended := s.theLiveAndEndedFilesFor(key)
	for _, other := range []string{live, ended} {
		if other == destination {
			continue
		}
		if err := os.Remove(other); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return next, fmt.Errorf("record written to %s but %s remains: %w", destination, other, err)
		}
		if err := syncDir(filepath.Dir(other)); err != nil {
			return next, err
		}
	}

	if err := s.recordHistory(key, previous, next); err != nil {
		// History is a convenience; the record is the truth. Failing the whole
		// write because the hover preview lost an entry would be the wrong
		// trade on the hook path.
		return next, fmt.Errorf("record written, history not: %w", err)
	}
	return next, nil
}

// fileFor is the one place that decides which directory a record belongs in.
// Ending is a transition, not a deletion (R6): the record moves, it is not
// removed, and a session that comes back is recognised.
func (s *SessionStore) fileFor(record agentnotify.Record) string {
	if record.Kernel == agentnotify.Ended {
		return s.layout.EndedFile(record.Key.String())
	}
	return s.layout.SessionFile(record.Key.String())
}

func (s *SessionStore) recordHistory(
	key agentnotify.Key, previous, next agentnotify.Record,
) error {
	if s.bounds.Messages <= 0 && s.bounds.Changes <= 0 {
		return nil
	}
	path := s.layout.HistoryFile(key.String())

	var history agentnotify.History
	if _, err := readJSON(path, &history); err != nil {
		// A corrupt history file must not stop a session from being recorded.
		// Starting a fresh one loses previews, not state.
		history = agentnotify.History{}
	}

	history, changed := history.Record(previous, next, s.bounds)
	if !changed {
		return nil
	}
	return writeJSON(path, history)
}

// History returns what a session has said and how its state has moved. An
// absent file is an empty history, not an error: most sessions have said
// nothing yet.
func (s *SessionStore) History(key agentnotify.Key) (agentnotify.History, error) {
	var history agentnotify.History
	if _, err := readJSON(s.layout.HistoryFile(key.String()), &history); err != nil {
		return agentnotify.History{}, err
	}
	return history, nil
}

// List returns every live session. This is the cold read path: it must work
// with no session-watcher running, because `agent-notify list` and an agent's
// own statusline both depend on it (§A7.6).
func (s *SessionStore) List() ([]agentnotify.Record, error) {
	return s.listDir(s.layout.Sessions())
}

// ListEnded returns the sessions that have ended and are still remembered —
// what a picker offers you to resume.
func (s *SessionStore) ListEnded() ([]agentnotify.Record, error) {
	return s.listDir(s.layout.Ended())
}

func (s *SessionStore) listDir(dir string) ([]agentnotify.Record, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot list %s: %w", dir, err)
	}

	records := make([]agentnotify.Record, 0, len(entries))
	var problems []error
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			// .write-* temporaries live here for microseconds at a time.
			continue
		}
		var record agentnotify.Record
		found, err := readJSON(filepath.Join(dir, entry.Name()), &record)
		if err != nil {
			// One unreadable record must not cost the caller the other
			// nineteen; a display showing nineteen sessions is right about
			// nineteen sessions.
			problems = append(problems, err)
			continue
		}
		if found {
			records = append(records, record)
		}
	}
	return records, errors.Join(problems...)
}

// ForgetWhatIsTooOld deletes ended sessions that have outlived
// `keep-ended-sessions`, and the history that belongs to them.
//
// It is the only thing in the system that deletes a session, and it deletes
// only what has been dead longer than the configured memory. Not knowing must
// never become deleting (R5): a record with no `ended_at` is never pruned, even
// though its presence in ended/ says it should have one.
func (s *SessionStore) ForgetWhatIsTooOld(
	now time.Time,
) (removed []agentnotify.Key, err error) {
	ended, listErr := s.ListEnded()
	var problems []error
	if listErr != nil {
		problems = append(problems, listErr)
	}

	for _, record := range ended {
		if record.EndedAt.IsZero() || now.Sub(record.EndedAt) < s.keep {
			continue
		}
		if err := s.forget(record.Key, record.Sequence); err != nil {
			problems = append(problems, err)
			continue
		}
		removed = append(removed, record.Key)
	}

	problems = append(problems, s.pruneLocks(now))
	return removed, errors.Join(problems...)
}

// forget removes one session's files under its own lock, and re-checks under
// that lock that the session is still the one it decided to delete. Between
// listing and locking, a resumed session can have written a new record — and
// deleting a live session is the one mistake this package must not make.
func (s *SessionStore) forget(key agentnotify.Key, expected uint64) error {
	held, err := lock(s.layout.LockFile(key.String()), s.patience)
	if err != nil {
		return err
	}
	defer unlock(held)

	current, found, err := s.Read(key)
	if err != nil {
		return err
	}
	if !found || current.Sequence != expected || current.Kernel != agentnotify.Ended {
		return nil
	}

	for _, path := range []string{
		s.layout.EndedFile(key.String()),
		s.layout.SessionFile(key.String()),
		s.layout.HistoryFile(key.String()),
	} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("cannot forget %s: %w", path, err)
		}
	}
	return nil
}

// pruneLocks removes lock files that belong to no session and are older than
// the memory window.
//
// The lock file is never deleted while it could be referenced, which is why
// this is a separate pass with a second condition: a lock created moments ago
// may belong to a writer that has not written its first record yet.
func (s *SessionStore) pruneLocks(now time.Time) error {
	entries, err := os.ReadDir(s.layout.Locks())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot list %s: %w", s.layout.Locks(), err)
	}

	var problems []error
	for _, entry := range entries {
		name := strings.TrimSuffix(entry.Name(), ".lock")
		if name == entry.Name() {
			continue
		}
		info, err := entry.Info()
		if err != nil || now.Sub(info.ModTime()) < s.keep {
			continue
		}
		key, err := agentnotify.ParseKey(name)
		if err != nil {
			continue
		}
		if _, found, err := s.Read(key); err != nil || found {
			continue
		}
		if err := os.Remove(filepath.Join(s.layout.Locks(), entry.Name())); err != nil && !os.IsNotExist(err) {
			problems = append(problems, err)
		}
	}
	return errors.Join(problems...)
}
