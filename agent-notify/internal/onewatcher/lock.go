// Package onewatcher is why there is one session-watcher: the lock that makes
// it single, and the way anybody starts one when nobody holds it.
package onewatcher

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/lassoColombo/agent-notify/internal/paths"
	"github.com/lassoColombo/agent-notify/session"
)

// Holder is what the lock file says about whoever holds it.
type Holder struct {
	PID     int       `json:"pid"`
	Version string    `json:"version"`
	Since   time.Time `json:"since"`
}

// Lock is the exclusive lock that makes the session-watcher single.
type Lock struct {
	file *os.File
}

// ErrAlreadyRunning means somebody else holds the lock, which is the ordinary
// outcome of a race and not a failure.
var ErrAlreadyRunning = fmt.Errorf("a session-watcher is already running")

// Take acquires the lock, or reports who has it.
//
// The kernel holds this mutex, so it survives every kind of death including
// SIGKILL. The lock file is never unlinked: deleting a locked file breaks the
// mutex, because the next process creates a fresh inode and locks that instead
// (plan.md §A9.2).
func Take(layout paths.Layout) (*Lock, error) {
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

	held := Holder{PID: os.Getpid(), Version: session.Version, Since: time.Now().UTC()}
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
	return &Lock{file: file}, nil
}

func (l *Lock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	defer l.file.Close()
	return syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
}

// WhoHolds reads the lock file without taking it. It reports what the holder
// wrote, not whether it is still alive; Running answers that.
func WhoHolds(layout paths.Layout) (Holder, error) {
	content, err := os.ReadFile(layout.WatcherLock())
	if err != nil {
		return Holder{}, err
	}
	var held Holder
	if err := json.Unmarshal(content, &held); err != nil {
		return Holder{}, fmt.Errorf("%s is not readable: %w", layout.WatcherLock(), err)
	}
	return held, nil
}

// Running reports whether anybody holds the lock, by trying to take it and
// giving it straight back.
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

// StartIfNobodyIs starts a detached session-watcher unless one holds the lock.
// Several callers may race; the lock means exactly one survives.
func StartIfNobodyIs(layout paths.Layout, configured string) error {
	if Running(layout) {
		return nil
	}
	return Spawn(layout, configured)
}

// Stop asks a running session-watcher to shut down: SIGTERM to the pid the
// lock file names, sent because a person asked. Nothing here ever kills.
func Stop(layout paths.Layout, within time.Duration) error {
	if !Running(layout) {
		return fmt.Errorf("no session-watcher is running")
	}
	held, err := WhoHolds(layout)
	if err != nil {
		return fmt.Errorf("something holds the lock but did not say who: %w", err)
	}
	if err := syscall.Kill(held.PID, syscall.SIGTERM); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return fmt.Errorf("cannot signal pid %d: %w", held.PID, err)
	}

	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if !Running(layout) {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("pid %d still holds the lock %s after being asked to stop", held.PID, within)
}
