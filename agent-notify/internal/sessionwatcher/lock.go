//go:build unix

// Package watcher is the one long-lived process: it watches for agents dying,
// sweeps up behind itself, and — from M9 — talks to the integrations.
package sessionwatcher

import (
	"encoding/json"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/lassoColombo/agent-notify/internal/paths"
	"github.com/lassoColombo/agent-notify/session"
)

// TheProcessHoldingTheLock is what the lock file says about whoever holds it,
// for doctor to read.
type TheProcessHoldingTheLock struct {
	PID     int       `json:"pid"`
	Version string    `json:"version"`
	Since   time.Time `json:"since"`
}

// TheOnlyRunningWatcher is the exclusive lock that makes the session-watcher single.
type TheOnlyRunningWatcher struct {
	file *os.File
}

// TakeIfNobodyElseHasIt acquires the lock, or reports who has it.
//
// The kernel holds this mutex, so it survives every kind of death including
// SIGKILL — which a check-then-act on a pid file does not. It is held for the
// process's life and released only by exiting.
//
// The lock file is **never unlinked**, and that is not tidiness: deleting a
// locked file breaks the mutex, because the next process creates a fresh inode
// and locks that instead. Then there are two session-watchers and neither knows
// (plan.md §A9.2).
func TakeIfNobodyElseHasIt(layout paths.Layout) (*TheOnlyRunningWatcher, error) {
	file, err := os.OpenFile(layout.WatcherLock(), os.O_CREATE|os.O_RDWR, paths.FileMode)
	if err != nil {
		return nil, fmt.Errorf("cannot open %s: %w", layout.WatcherLock(), err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		if held, readErr := WhoHolds(layout); readErr == nil && held.PID != 0 {
			return nil, fmt.Errorf("%w: pid %d, version %s, since %s",
				ErrAlreadyRunning, held.PID, held.Version, held.Since.Format(time.RFC3339))
		}
		return nil, ErrAlreadyRunning
	}

	held := TheProcessHoldingTheLock{
		PID: os.Getpid(), Version: session.Version, Since: time.Now().UTC(),
	}
	encoded, err := json.Marshal(held)
	if err != nil {
		file.Close()
		return nil, err
	}
	// Truncate rather than replace: the inode has to stay the one everybody
	// else is trying to lock.
	if err := file.Truncate(0); err != nil {
		file.Close()
		return nil, err
	}
	if _, err := file.WriteAt(append(encoded, '\n'), 0); err != nil {
		file.Close()
		return nil, err
	}
	return &TheOnlyRunningWatcher{file: file}, nil
}

// ErrAlreadyRunning means somebody else holds the lock, which is the ordinary
// outcome of a race between two processes both trying to start one. It is not
// a failure: the loser simply has nothing to do.
var ErrAlreadyRunning = fmt.Errorf("a session-watcher is already running")

// Release drops the lock. Exiting would do it too; this says so at the call site.
func (s *TheOnlyRunningWatcher) Release() error {
	if s == nil || s.file == nil {
		return nil
	}
	defer s.file.Close()
	return syscall.Flock(int(s.file.Fd()), syscall.LOCK_UN)
}

// WhoHolds reads the lock file without taking it, which is what doctor does.
// It reports what the holder wrote, not whether it is still alive — a stale
// record is possible and Running is the question to ask about that.
func WhoHolds(layout paths.Layout) (TheProcessHoldingTheLock, error) {
	content, err := os.ReadFile(layout.WatcherLock())
	if err != nil {
		return TheProcessHoldingTheLock{}, err
	}
	var held TheProcessHoldingTheLock
	if err := json.Unmarshal(content, &held); err != nil {
		return TheProcessHoldingTheLock{},
			fmt.Errorf("%s is not readable: %w", layout.WatcherLock(), err)
	}
	return held, nil
}

// Running reports whether anybody holds the lock, by trying to take it and
// giving it straight back.
//
// A failure to acquire is the answer, not an error: flock is the only thing
// that knows, and asking it is cheap.
func Running(layout paths.Layout) bool {
	file, err := os.OpenFile(layout.WatcherLock(), os.O_CREATE|os.O_RDWR, paths.FileMode)
	if err != nil {
		return false
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return true
	}
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	return false
}
