//go:build darwin

package watcher

import (
	"fmt"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Exits tells us the moment a process we did not start goes away.
//
// [verified 2026-09-16, macOS 26] kqueue's EVFILT_PROC with NOTE_EXIT registers
// against any process owned by the same user, with no privileges and no
// entitlement, and the event arrives the instant that process exits. This is
// what makes `kill -9` on an agent show up on your bar in milliseconds instead
// of at the next sweep (plan.md §A8.1).
//
// The sweep stays, as the safety net behind it: a pid we failed to register, a
// record inherited from a previous session-watcher, a process that died in the
// gap before we were listening.
type Exits struct {
	queue   int
	watched map[int]bool
}

// WatchExits opens the queue.
func WatchExits() (*Exits, error) {
	queue, err := unix.Kqueue()
	if err != nil {
		return nil, fmt.Errorf("cannot open a kqueue: %w", err)
	}
	return &Exits{queue: queue, watched: map[int]bool{}}, nil
}

// Watch registers one pid, if it is not registered already.
//
// A pid that has already exited fails to register, and that failure is the
// answer rather than a problem: the sweep will judge it on the next tick, from
// the store, against the start time. Registration is an optimisation on top of
// a correct-but-slower mechanism, never the mechanism itself (R5).
func (e *Exits) Watch(pid int) error {
	if pid <= 0 || e.watched[pid] {
		return nil
	}
	event := unix.Kevent_t{
		Ident:  uint64(pid),
		Filter: unix.EVFILT_PROC,
		Flags:  unix.EV_ADD | unix.EV_ONESHOT,
		Fflags: unix.NOTE_EXIT,
	}
	if _, err := unix.Kevent(e.queue, []unix.Kevent_t{event}, nil, nil); err != nil {
		return fmt.Errorf("cannot watch pid %d: %w", pid, err)
	}
	e.watched[pid] = true
	return nil
}

// Forget stops tracking a pid we have already dealt with, so that a pid reused
// later can be watched again.
func (e *Exits) Forget(pid int) { delete(e.watched, pid) }

// Watching reports how many pids are registered, for doctor.
func (e *Exits) Watching() int { return len(e.watched) }

// WhichPidsExited blocks until a watched process exits or the deadline passes,
// and returns the pids that went.
//
// The timeout is not how exits are noticed — an event arrives the instant it
// happens — it is only how this returns to the caller's loop so that a sweep
// tick or a shutdown can be seen.
func (e *Exits) WhichPidsExited(within time.Duration) ([]int, error) {
	events := make([]unix.Kevent_t, 16)
	timeout := unix.NsecToTimespec(int64(within))

	count, err := unix.Kevent(e.queue, nil, events, &timeout)
	if err != nil {
		if err == syscall.EINTR {
			return nil, nil
		}
		return nil, fmt.Errorf("cannot read the kqueue: %w", err)
	}

	gone := make([]int, 0, count)
	for _, event := range events[:count] {
		pid := int(event.Ident)
		// EV_ONESHOT removed it already; forget it so a later record naming
		// the same pid can register again.
		delete(e.watched, pid)
		gone = append(gone, pid)
	}
	return gone, nil
}

// Close releases the queue and every registration with it.
func (e *Exits) Close() error {
	e.watched = nil
	return unix.Close(e.queue)
}
