package process

import (
	agentnotify "github.com/lassoColombo/agent-notify"
)

// Verdict is what can be said about a session's process. Three values, not two,
// because the third is the safety rail: not knowing must never become deleting
// (R5, R27).
type Verdict int

const (
	// CannotTell — for any reason at all. Touch nothing.
	CannotTell Verdict = iota
	// Running — the process we recorded is the process running now.
	Running
	// Gone — provably. This is the only verdict that ends a session.
	Gone
)

func (v Verdict) String() string {
	switch v {
	case Running:
		return "running"
	case Gone:
		return "gone"
	default:
		return "cannot tell"
	}
}

// LivenessOf decides whether one session's process is still there.
//
// The reboot rule comes first and probes nothing: pids from a previous boot
// either do not exist or belong to something else entirely, so a machine that
// has restarted would otherwise show ghosts forever (§A8.3).
func LivenessOf(record agentnotify.Record, boot string, prober ReadsProcessFacts) Verdict {
	if record.Process.PID <= 0 {
		// A record with no process can never be proved dead. That is the right
		// answer and not a gap: it is what the lease is for, one day (§A8.5).
		return CannotTell
	}
	if boot != "" && record.Process.BootID != "" && record.Process.BootID != boot {
		return Gone
	}

	facts, err := prober.FactsAbout(record.Process.PID)
	switch {
	case err == ErrNoSuchProcess:
		return Gone
	case err != nil:
		return CannotTell
	case !SameProcess(record.Process, facts):
		// The pid is in use, by something else. Ours is gone.
		return Gone
	}
	return Running
}

// Ending is one session that has ended without anyone saying so, and why. The
// reason becomes the detail on the `ended` kernel (§A5.6).
type Ending struct {
	Key    agentnotify.Key
	Detail string
}

// The two ways a session ends without a hook to announce it.
const (
	// ProcessGone — the process it ran in is not there any more.
	ProcessGone = "process-gone"
	// Superseded — its process is alive but is running a newer session.
	// `/clear` does not end an agent; it starts a fresh session inside the same
	// process, so two records can name one living pid and only the newest is
	// real (§A6).
	Superseded = "superseded"
)

// Ended is the sweep's decision, over every live record at once.
//
// It is a pure function of the records and one question about processes, so
// every rule is tested against invented records with nothing running.
// `self` is the caller's own pid, passed in rather than read here so that this
// stays a function of its arguments and nothing else.
func Ended(
	records []agentnotify.Record, boot string, self int, prober ReadsProcessFacts,
) []Ending {
	// The rail that makes every verdict below meaningful: if the prober cannot
	// see the process asking, it is not looking at this machine, and nothing it
	// says is worth acting on. Without this, one broken moment ends every
	// session on the bar at once.
	if _, err := prober.FactsAbout(self); err != nil {
		return nil
	}

	judgeable := make([]agentnotify.Record, 0, len(records))
	for _, record := range records {
		if record.Kernel == agentnotify.Ended || record.Process.PID <= 0 {
			continue
		}
		judgeable = append(judgeable, record)
	}

	var ending []Ending
	for _, record := range judgeable {
		switch LivenessOf(record, boot, prober) {
		case Gone:
			ending = append(ending, Ending{Key: record.Key, Detail: ProcessGone})
			continue
		case CannotTell:
			continue
		}

		// The process is alive. It runs one session at a time, so a newer
		// record naming the same process — the same pid started at the same
		// moment, not merely the same number — means this one is finished.
		if supersededBy(record, judgeable) {
			ending = append(ending, Ending{Key: record.Key, Detail: Superseded})
		}
	}
	return ending
}

func supersededBy(record agentnotify.Record, others []agentnotify.Record) bool {
	for _, other := range others {
		if other.Key == record.Key {
			continue
		}
		if !SameProcess(record.Process, Facts{
			PID:       other.Process.PID,
			StartedAt: other.Process.StartedAt,
		}) {
			continue
		}
		if other.UpdatedAt.After(record.UpdatedAt) {
			return true
		}
	}
	return false
}
