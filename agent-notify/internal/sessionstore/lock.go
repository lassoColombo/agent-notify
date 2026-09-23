//go:build unix

package sessionstore

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/lassoColombo/agent-notify/internal/paths"
)

// ErrBusy is returned when another writer held the session's lock for longer
// than the configured patience.
//
// It is not a failure to escalate. record-agent-event logs it and exits 0,
// because failing the hook is never an option (R2) and the next event
// re-establishes the state anyway. Losing a write is cheaper than hanging the
// agent (R1).
var ErrBusy = errors.New("another writer holds this session")

// lock takes an exclusive flock on the session's lock file.
//
// The lock file is opened and never renamed or replaced, so the lock always
// refers to the same inode — locking a file you are about to replace by rename
// is the classic way to hold a lock nobody else can see (plan.md §A7.3).
//
// flock is released by the kernel when the file descriptor goes away, including
// on SIGKILL, so there is no stale lock to clean up after a crash and no
// liveness check to write.
func lock(path string, patience time.Duration) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, paths.FileMode)
	if err != nil {
		return nil, fmt.Errorf("cannot open the lock %s: %w", path, err)
	}

	// Poll rather than block: a blocking flock cannot be given a deadline, and
	// an unbounded wait on the hook path is the one thing R1 forbids. Contention
	// is two hooks of the same session firing together, so it is brief.
	deadline := time.Now().Add(patience)
	wait := time.Millisecond
	for {
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return file, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			file.Close()
			return nil, fmt.Errorf("cannot lock %s: %w", path, err)
		}
		if !time.Now().Before(deadline) {
			file.Close()
			return nil, fmt.Errorf("%w: %s", ErrBusy, path)
		}
		time.Sleep(wait)
		if wait < 20*time.Millisecond {
			wait *= 2
		}
	}
}

// unlock drops the lock. Closing the descriptor would be enough; the explicit
// unlock says so at the call site.
func unlock(file *os.File) error {
	defer file.Close()
	return syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}
