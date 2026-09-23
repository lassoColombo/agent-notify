package process

import (
	"path/filepath"

	agentnotify "github.com/lassoColombo/agent-notify"
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
func Ancestry(start int, prober ReadsProcessFacts) []agentnotify.Ancestor {
	var chain []agentnotify.Ancestor
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
		chain = append(chain, agentnotify.Ancestor{
			PID:       facts.PID,
			StartedAt: facts.StartedAt,
			Command:   facts.Command,
		})
		pid = facts.Parent
	}
	return chain
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
// Not finding it is not a failure. A shell wrapper that re-execs, or a renamed
// binary, leaves the chain recorded and the agent unknown; liveness then has
// nothing to judge and says so (§A8.4, §A8.5).
func FindAgent(
	binary string, start int, prober ReadsProcessFacts,
) (agentnotify.Process, []agentnotify.Ancestor, bool) {
	chain := Ancestry(start, prober)

	for _, ancestor := range chain {
		facts, err := prober.FactsAbout(ancestor.PID)
		if err != nil {
			continue
		}
		if !isBinary(facts, binary) {
			continue
		}
		return agentnotify.Process{
			PID:       facts.PID,
			StartedAt: facts.StartedAt,
		}, chain, true
	}
	return agentnotify.Process{}, chain, false
}

// isBinary decides whether a process is the one the user configured.
//
// Three spellings are accepted because all three are things a person writes in
// a config file: the short name the kernel reports, the full path, and the
// basename of that path. Accepting only the first would make
// `binary = "/opt/homebrew/bin/claude"` — the way you disambiguate two claudes
// — silently match nothing.
func isBinary(facts Facts, binary string) bool {
	if binary == "" {
		return false
	}
	switch binary {
	case facts.Command, facts.Path, filepath.Base(facts.Path):
		return true
	}
	return false
}
