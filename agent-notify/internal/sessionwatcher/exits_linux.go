//go:build linux

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
// [verified 2026-10-09, Linux 6.10] pidfd_open(2), Linux 5.3 and later, gives a
// descriptor for any process owned by the same user, with no privileges, and
// the descriptor becomes readable the instant that process exits (plan.md
// §A8.1). They all sit in one epoll set, so
// a pid watched while WhichPidsExited is waiting is seen by that wait, as with
// kqueue on macOS. The sweep stays as the safety net behind it.
type Exits struct {
	epoll   int
	mu      sync.Mutex
	watched map[int]int // pid to its pidfd
}

func WatchExits() (*Exits, error) {
	epoll, err := unix.EpollCreate1(unix.EPOLL_CLOEXEC)
	if err != nil {
		return nil, fmt.Errorf("cannot open an epoll set: %w", err)
	}
	return &Exits{epoll: epoll, watched: map[int]int{}}, nil
}

// Watch registers one pid. A pid that has already exited fails to register,
// and the sweep judges it from the store instead (R5).
func (e *Exits) Watch(pid int) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.watched[pid]; pid <= 0 || ok {
		return nil
	}
	pidfd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		return fmt.Errorf("cannot watch pid %d: %w", pid, err)
	}
	// The kernel hands Fd back untouched, so it carries the pid, which is
	// what the caller wants back, rather than the descriptor.
	event := unix.EpollEvent{Events: unix.EPOLLIN, Fd: int32(pid)}
	if err := unix.EpollCtl(e.epoll, unix.EPOLL_CTL_ADD, pidfd, &event); err != nil {
		_ = unix.Close(pidfd)
		return fmt.Errorf("cannot watch pid %d: %w", pid, err)
	}
	e.watched[pid] = pidfd
	return nil
}

// WhichPidsExited blocks until a watched process exits or the deadline passes.
func (e *Exits) WhichPidsExited(within time.Duration) ([]int, error) {
	events := make([]unix.EpollEvent, 16)

	count, err := unix.EpollWait(e.epoll, events, int(within.Milliseconds()))
	if err != nil {
		if err == syscall.EINTR {
			return nil, nil
		}
		return nil, fmt.Errorf("cannot read the epoll set: %w", err)
	}

	gone := make([]int, 0, count)
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, event := range events[:count] {
		pid := int(event.Fd)
		// Closing the pidfd takes it out of the set, which is what EV_ONESHOT
		// does on macOS. Left open, it would report the same exit forever.
		if pidfd, ok := e.watched[pid]; ok {
			_ = unix.Close(pidfd)
			delete(e.watched, pid)
		}
		gone = append(gone, pid)
	}
	return gone, nil
}

func (e *Exits) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, pidfd := range e.watched {
		_ = unix.Close(pidfd)
	}
	return unix.Close(e.epoll)
}
