//go:build linux

package storewatch

import (
	"fmt"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Watch reports when an entry in any of its directories is added, replaced
// or removed. Every record write is a rename into sessions/ or ended/, so this
// is the whole of how a change in the store is noticed.
type Watch struct {
	inotify int
}

func Directories(dirs ...string) (*Watch, error) {
	inotify, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	if err != nil {
		return nil, fmt.Errorf("cannot open an inotify instance: %w", err)
	}
	watch := &Watch{inotify: inotify}
	for _, dir := range dirs {
		// What NOTE_WRITE, NOTE_DELETE and NOTE_RENAME say on macOS: an entry
		// came or went, or the directory itself did.
		events := uint32(unix.IN_CREATE | unix.IN_DELETE | unix.IN_MOVED_FROM | unix.IN_MOVED_TO |
			unix.IN_DELETE_SELF | unix.IN_MOVE_SELF | unix.IN_ONLYDIR)
		if _, err := unix.InotifyAddWatch(inotify, dir, events); err != nil {
			watch.Close()
			return nil, fmt.Errorf("cannot watch %s: %w", dir, err)
		}
	}
	return watch, nil
}

// Changed blocks until something moves or the deadline passes.
func (d *Watch) Changed(within time.Duration) bool {
	ready := []unix.PollFd{{Fd: int32(d.inotify), Events: unix.POLLIN}}
	count, err := unix.Poll(ready, int(within.Milliseconds()))
	if err != nil && err != syscall.EINTR {
		time.Sleep(100 * time.Millisecond)
	}
	if count <= 0 {
		return false
	}
	// Which entries moved does not matter, only that one did. But the events
	// must be read, or the descriptor stays readable and every call after this
	// returns at once; kqueue's EV_CLEAR is what does this on macOS.
	events := make([]byte, 4096)
	for {
		if n, _ := unix.Read(d.inotify, events); n <= 0 {
			return true
		}
	}
}

func (d *Watch) Close() error {
	return unix.Close(d.inotify)
}
