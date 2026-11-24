package process

import (
	"github.com/lassoColombo/agent-notify/session"
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

// LivenessOf decides whether one session's process is still there.
//
// The reboot rule comes first and probes nothing: pids from a previous boot
// either do not exist or belong to something else entirely, so a machine that
// has restarted would otherwise show ghosts forever (§A8.3).
func LivenessOf(record session.Record, boot string, prober ReadsProcessFacts) Verdict {
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
	Key    session.Key
	Detail string
}

// The two ways a session ends without a hook to announce it.
const (
	// ProcessGone — the process it ran in is not there any more.
	ProcessGone = "process-gone"
	// Superseded — its process is alive but is running a newer session.
	// `/clear` does not end an agent; it starts a fresh session inside the same
	// process, so two records can name one living pid and only the newest is
	// real (§A6). Or the conversation went on in another session, which says
	// so, and the process this one ran in stayed behind (D-127).
	Superseded = "superseded"
)

// Ended is the sweep's decision, over every live record at once.
//
// It is a pure function of the records and one question about processes, so
// every rule is tested against invented records with nothing running.
// `self` is the caller's own pid, passed in rather than read here so that this
// stays a function of its arguments and nothing else.
func Ended(
	records []session.Record, boot string, self int, prober ReadsProcessFacts,
) []Ending {
	// The rail that makes every verdict below meaningful: if the prober cannot
	// see the process asking, it is not looking at this machine, and nothing it
	// says is worth acting on. Without this, one broken moment ends every
	// session on the bar at once.
	if _, err := prober.FactsAbout(self); err != nil {
		return nil
	}

	judgeable := make([]session.Record, 0, len(records))
	for _, record := range records {
		if record.Kernel == session.Ended || record.Process.PID <= 0 {
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

		// The process is alive. It runs one session at a time, so a record
		// the agent reported on later from the same process — the same pid
		// started at the same moment, not merely the same number — means
		// this one is finished. So does a record that says it carries on
		// from this one, wherever it runs (D-127).
		if supersededBy(record, judgeable) || continuedIn(record, records) {
			ending = append(ending, Ending{Key: record.Key, Detail: Superseded})
		}
	}
	return ending
}

// continuedIn is whether another session of the same agent, on the same host,
// says it carries on from this one and was reported on after it (D-127). The
// order is D-110's: a session talked to again after it was continued is the
// one being talked to, and a claim older than its last report must not end
// it. Every record handed over is asked, ended ones included, because a
// continuation that has itself finished still says where this one went.
func continuedIn(record session.Record, others []session.Record) bool {
	for _, other := range others {
		if other.ContinuedFrom != record.Key.SessionID || other.Key == record.Key ||
			other.Key.Host != record.Key.Host || other.Key.Agent != record.Key.Agent {
			continue
		}
		if other.ReportedAt.After(record.ReportedAt) {
			return true
		}
	}
	return false
}

// supersededBy compares when the two sessions were last reported on, which
// is the one thing that says which of them the process is running: a hook
// speaks from inside the process about the session it is in. Not
// `updated_at`, which a derive or an annotate landing on the older session
// moves; not `created_at`, which the older session keeps when it is resumed
// inside the same process after the newer one (D-110). A record from before
// the stamp existed is never newer than one with it, and two of them end
// nobody (R5).
func supersededBy(record session.Record, others []session.Record) bool {
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
		if other.ReportedAt.After(record.ReportedAt) {
			return true
		}
	}
	return false
}
