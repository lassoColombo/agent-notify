// Package process answers the hardest question in the system: is that agent
// still there?
//
// An agent is a process. If the process is gone the agent is gone, and every
// other signal is a proxy that is wrong somewhere — a terminal pane outlives
// the process it held, a transcript outlives the session that wrote it, and a
// quiet record means "waiting for you" as often as it means "dead".
//
// Everything here that decides anything is a pure function of a record and a
// ReadsProcessFacts, so every rule is tested against an invented machine with
// nothing running (plan.md §A8).
package process

import (
	"errors"
	"os"
	"time"

	"github.com/lassoColombo/agent-notify/session"
)

// ErrNoSuchProcess is the one failure that means something. Every other error
// from a ReadsProcessFacts means "cannot tell", and cannot tell must never
// become deleting (R5).
var ErrNoSuchProcess = errors.New("no such process")

// Facts are what this machine knows about one process.
type Facts struct {
	PID int
	// StartedAt is what makes a pid mean something. Pids are recycled, so a
	// number that matched an agent an hour ago may match a stranger now, and a
	// dead session would be kept alive forever in the belief that the stranger
	// is it.
	//
	// It is only ever compared with another reading from the same boot, and on
	// Linux it is not a date: it is the time since boot, counted from the Unix
	// epoch, because that is the one form of it the kernel never moves (D-89).
	StartedAt time.Time
	// Command is the short name the kernel keeps, truncated to its own limit.
	Command string
	// Path is the executable's full path where it could be read. It is used to
	// match a configured binary and never stored: a record is about a kilobyte
	// and rides in every delta (§A13.1), and six absolute paths would be a
	// quarter of that for something nothing reads yet.
	Path   string
	Parent int
}

// ReadsProcessFacts is one question about one pid. The implementation that asks
// this machine is in facts_darwin.go and facts_linux.go; tests hand in a map.
type ReadsProcessFacts interface {
	FactsAbout(pid int) (Facts, error)
}

// SameProcessWindow is how close two start times must be to be the same
// process.
//
// Exact equality would be wrong: the resolution differs by platform and by
// probe — microseconds from sysctl here, clock ticks from /proc on Linux — and
// a record written by one and checked by another would disagree with itself.
// A second is safe because pid reuse requires the kernel's pid counter to wrap,
// which is tens of thousands of processes, not one second's worth.
const SameProcessWindow = time.Second

// SameProcess reports whether a recorded process and a live one are the same
// thing, rather than the same number.
func SameProcess(recorded session.Process, found Facts) bool {
	if recorded.PID != found.PID {
		return false
	}
	if recorded.StartedAt.IsZero() || found.StartedAt.IsZero() {
		// A record written before we knew how to read a start time cannot be
		// checked for reuse. Saying "same" keeps it alive, and keeping a dead
		// session is a smaller wrong than ending a live one (R5).
		return true
	}
	difference := recorded.StartedAt.Sub(found.StartedAt)
	return difference < SameProcessWindow && difference > -SameProcessWindow
}

// Self is the pid of this process, which is the one thing a working reader must
// always be able to see (§A8.2).
func Self() int { return os.Getpid() }
