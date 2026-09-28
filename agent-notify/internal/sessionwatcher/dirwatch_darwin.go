//go:build darwin

package sessionwatcher

import (
	"fmt"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// DirWatch reports when an entry in any of its directories is added, replaced
// or removed. Every record write is a rename into sessions/ or ended/, so this
// is the whole of how a change in the store is noticed.
type DirWatch struct {
	queue int
	fds   []int
}

func WatchDirectories(dirs ...string) (*DirWatch, error) {
	queue, err := unix.Kqueue()
	if err != nil {
		return nil, fmt.Errorf("cannot open a kqueue: %w", err)
	}
	watch := &DirWatch{queue: queue}
	for _, dir := range dirs {
		fd, err := unix.Open(dir, unix.O_EVTONLY|unix.O_DIRECTORY, 0)
		if err != nil {
			watch.Close()
			return nil, fmt.Errorf("cannot watch %s: %w", dir, err)
		}
		watch.fds = append(watch.fds, fd)
		event := unix.Kevent_t{
			Ident:  uint64(fd),
			Filter: unix.EVFILT_VNODE,
			Flags:  unix.EV_ADD | unix.EV_CLEAR,
			Fflags: unix.NOTE_WRITE | unix.NOTE_DELETE | unix.NOTE_RENAME,
		}
		if _, err := unix.Kevent(queue, []unix.Kevent_t{event}, nil, nil); err != nil {
			watch.Close()
			return nil, fmt.Errorf("cannot watch %s: %w", dir, err)
		}
	}
	return watch, nil
}

// Changed blocks until something moves or the deadline passes.
func (d *DirWatch) Changed(within time.Duration) bool {
	events := make([]unix.Kevent_t, 16)
	timeout := unix.NsecToTimespec(int64(within))
	count, err := unix.Kevent(d.queue, nil, events, &timeout)
	if err != nil && err != syscall.EINTR {
		time.Sleep(100 * time.Millisecond)
	}
	return count > 0
}

func (d *DirWatch) Close() error {
	for _, fd := range d.fds {
		_ = unix.Close(fd)
	}
	return unix.Close(d.queue)
}
