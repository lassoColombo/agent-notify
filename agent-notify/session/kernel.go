package session

import (
	"cmp"
	"maps"
	"slices"
)

// Kernel is the closed, core-owned half of a state (plan.md §A5).
//
// It is not a description of the agent. It is a description of what the state
// demands of the human, which is why it can legitimately be closed: compacting,
// running a tool, summarising and waiting on a subagent are four different
// things inside an agent and one single thing to your attention. What differs
// between them is exactly what the detail carries.
type Kernel string

// The six kernel states, each named for what it demands of you (D-15).
const (
	// BlockedOnYou — it stopped mid-work and cannot continue until you answer.
	BlockedOnYou Kernel = "blocked-on-you"
	// Broke — the turn died; work stopped without finishing.
	Broke Kernel = "broke"
	// FinishedATurn — it produced an answer and stopped; your move, at your
	// leisure. Nothing marks this as read; it is true until you reply (D-16).
	FinishedATurn Kernel = "finished-a-turn"
	// Working — it is doing something; nothing is required of you.
	Working Kernel = "working"
	// Idle — alive, nothing pending.
	Idle Kernel = "idle"
	// Ended — gone; the detail says why.
	Ended Kernel = "ended"
)

// Ranks are sparse on purpose. A state inserted later must not renumber its
// neighbours, because a display keyed on the rank lives in a repository we do
// not control and is not rebuilt when core ships a seventh state (§A5.5).
const (
	RankBlockedOnYou  = 50
	RankBroke         = 40
	RankFinishedATurn = 30
	RankWorking       = 20
	RankIdle          = 10
	RankEnded         = 0

	// RankUnknown is what Rank reports for a kernel this build has never heard
	// of. It is not a rank: a record carries its own rank precisely so that a
	// reader meeting an unfamiliar kernel uses the number that came with it
	// rather than one computed here.
	RankUnknown = -1
)

// kernels is the one table. The urgency ordering below is derived from it
// rather than written out a second time, so a seventh state cannot be added
// here and forgotten there.
var kernels = map[Kernel]int{
	BlockedOnYou:  RankBlockedOnYou,
	Broke:         RankBroke,
	FinishedATurn: RankFinishedATurn,
	Working:       RankWorking,
	Idle:          RankIdle,
	Ended:         RankEnded,
}

// Rank is the kernel's position in the urgency order — how much your delay
// costs, not how interesting the agent finds itself.
//
// BlockedOnYou outranks Broke because the cost of your delay is still running:
// a blocked agent sits there burning your wall-clock, a broken one has already
// stopped.
func (k Kernel) Rank() int {
	if rank, ok := kernels[k]; ok {
		return rank
	}
	return RankUnknown
}

// Known reports whether this build understands the kernel. An unknown kernel is
// never an error and never a reason to discard anything (R5): failing to
// understand a state is not a licence to decide what it was.
func (k Kernel) Known() bool {
	_, ok := kernels[k]
	return ok
}

// Live reports whether the session is still one you could be waiting on.
//
// The empty kernel means no record at all, which is not live in a different way
// from Ended — but the same way as far as every caller here is concerned. An
// unknown kernel is live, because assuming otherwise is a way of deleting
// something we merely failed to understand (R5).
func (k Kernel) Live() bool { return k != "" && k != Ended }

// kernelsByUrgency is the table sorted once, at startup. Kernels() is called
// per record painted, and sorting six entries on each of those would be work
// nobody asked for.
var kernelsByUrgency = slices.SortedFunc(maps.Keys(kernels), func(a, b Kernel) int {
	return cmp.Compare(kernels[b], kernels[a])
})

// Kernels returns the six, most urgent first. Displays that enumerate states —
// a legend, a configuration check, a test — should ask rather than hardcode.
//
// It hands back a copy, so that a caller sorting or truncating what it is given
// cannot reorder the table underneath everybody else.
func Kernels() []Kernel { return slices.Clone(kernelsByUrgency) }
