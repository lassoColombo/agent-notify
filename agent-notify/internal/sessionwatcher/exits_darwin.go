//go:build darwin

package sessionwatcher

import (
	"fmt"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Exits tells us the moment a process we did not start goes away.
//
// [verified 2026-09-16, macOS 26] kqueue's EVFILT_PROC with NOTE_EXIT registers
// against any process owned by the same user, with no privileges, and the event
// arrives the instant that process exits (plan.md §A8.1). The sweep stays as
// the safety net behind it.
type Exits struct {
	queue   int
	mu      sync.Mutex
	watched map[int]bool
}

func WatchExits() (*Exits, error) {
	queue, err := unix.Kqueue()
	if err != nil {
		return nil, fmt.Errorf("cannot open a kqueue: %w", err)
	}
	return &Exits{queue: queue, watched: map[int]bool{}}, nil
}

// Watch registers one pid. A pid that has already exited fails to register,
// and the sweep judges it from the store instead (R5).
func (e *Exits) Watch(pid int) error {
	e.mu.Lock()
	defer e.mu.Unlock()
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

// WhichPidsExited blocks until a watched process exits or the deadline passes.
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
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, event := range events[:count] {
		pid := int(event.Ident)
		delete(e.watched, pid)
		gone = append(gone, pid)
	}
	return gone, nil
}

func (e *Exits) Close() error {
	return unix.Close(e.queue)
}
