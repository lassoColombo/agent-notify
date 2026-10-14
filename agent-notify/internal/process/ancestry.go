package process

import (
	"github.com/lassoColombo/agent-notify/session"
)

// MostAncestorsWorthClimbing bounds the walk.
//
// The hook is not a fixed distance from its agent — a shell may or may not sit
// between them, and a wrapper may add two more — so the walk climbs to a name
// rather than counting steps. The bound is not about depth so much as about a
// process tree that has been corrupted into a cycle: a hook must stop, not
// hang (R1).
const MostAncestorsWorthClimbing = 10

// Ancestry walks up from a pid, recording what it passes.
//
// This must run in the hook process. By the time the session-watcher reads the
// event the hook has exited and there is no chain left to walk from (§A8.4).
//
// It stops at whatever it cannot read rather than failing: a partial chain is
// worth more than none, and the caller is a hook that may never fail (R2).
func Ancestry(start int, prober ReadsProcessFacts) []session.Ancestor {
	return recorded(climb(start, prober))
}

// climb is the walk itself, keeping everything it read about each rung, so
// that whoever matches on the chain matches on what was read rather than
// reading it again (D-114).
func climb(start int, prober ReadsProcessFacts) []Facts {
	var chain []Facts
	seen := map[int]bool{}

	for pid := start; pid > 1 && len(chain) < MostAncestorsWorthClimbing; {
		if seen[pid] {
			// A process tree should never contain a cycle. This is here
			// because "should never" is not "cannot", and the cost of being
			// wrong is a hung agent.
			break
		}
		seen[pid] = true

		facts, err := prober.FactsAbout(pid)
		if err != nil {
			break
		}
		chain = append(chain, facts)
		pid = facts.Parent
	}
	return chain
}

// recorded is the chain as a record carries it: the pid, when it started and
// what it is called.
func recorded(chain []Facts) []session.Ancestor {
	ancestors := make([]session.Ancestor, 0, len(chain))
	for _, facts := range chain {
		ancestors = append(ancestors, session.Ancestor{
			PID: facts.PID, StartedAt: facts.StartedAt, Command: facts.Command,
		})
	}
	return ancestors
}

// FindAgent climbs from a pid looking for the process running the agent, and
// returns it along with everything it walked past.
//
// The pid a hook knows is its own, never the agent's. A hook is started by the
// agent, which makes it a descendant, and a descendant can ask who started it.
//
// The nearest match wins, which is what makes a claude session running inside a
// claude session resolve to the inner one — the session you are actually in.
//
// The match is on the name the kernel keeps for the process and on nothing
// else (D-118): the adapter hands over the constant its agent runs under, and
// the kernel takes the name from the last component of the path handed to
// exec, so a symlink called `claude` to a versioned binary still reads
// `claude`.
//
// Not finding it is not a failure. A shell wrapper that re-execs, or a renamed
// binary, leaves the chain recorded and the agent unknown; liveness then has
// nothing to judge and says so (§A8.4, §A8.5).
func FindAgent(
	binary string, start int, prober ReadsProcessFacts,
) (session.Process, []session.Ancestor, bool) {
	chain := climb(start, prober)
	for _, facts := range chain {
		if binary != "" && facts.Command == binary {
			return session.Process{
				PID:       facts.PID,
				StartedAt: facts.StartedAt,
			}, recorded(chain), true
		}
	}
	return session.Process{}, recorded(chain), false
}
