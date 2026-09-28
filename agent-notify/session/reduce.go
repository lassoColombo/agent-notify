package session

import (
	"time"
)

// Report is what an agent-integration says happened: one event, plus whatever
// the agent's hook happened to know at the time.
//
// Every field except Event and Key is optional, and an absent field means
// "leave what is there alone" rather than "clear it". A hook that knows the cwd
// and a hook that does not must both be usable, and the one that does not must
// not erase what the other established.
type Report struct {
	Key   Key
	Event Event

	// Detail refines the kernel this event produces. It is replaced whenever
	// the event asserts a state, including with nothing: a detail belongs to
	// the state it qualifies, and carrying "compacting" into a finished turn
	// would be worse than carrying nothing.
	Detail string

	// Message is a pointer because nil and "" mean different things: nil is an
	// event that carried no text, "" is an event that deliberately cleared it.
	Message *string

	// Name is what the agent calls this session, for an agent that keeps a name
	// of its own. It is a label and not a state: it rides along with whatever
	// event happened to carry it, and it never moves state_since by itself
	// (§A7.4.2, D-60). Empty means "leave the stored name alone" — an event is
	// how a name arrives, never how one is cleared.
	Name string

	Cwd string
	// Branch is filled in by core on the hook path, not by the adapter — it is
	// read from whatever is checked out at Cwd, so that one answer serves every
	// agent and no agent's idea of it has to be trusted (§A7.4.4).
	Branch string
	// Model is what the agent says it is using, in the agent's own spelling.
	// Empty means "leave the stored one alone", like every other label here.
	Model string

	// Spent is every response the adapter could see in one read of the tail of
	// its agent's own accounting, oldest first. Core adds the ones it has not
	// counted yet and moves the cursor, because the adapter cannot: it is a
	// one-shot process that exits before the record carrying the running total
	// has been written (§A7.4.3, D-61).
	Spent []Spend

	Process         Process
	CapturedContext *CapturedContext
}

// Reduce is the state machine, and the whole of it (plan.md §A5.7).
//
// It is pure and it is called from two places — record-agent-event for what the
// agent reports, and the session-watcher for what observation discovers — which
// is what fixes the failure both previous implementations had: an adapter
// forced to discard a real event because it could not see the state it would
// have overwritten (§A5.4).
//
// The default branch is where three rules meet, and they turn out to be one
// rule. `session-started` asserts that a session exists rather than that
// something changed, and an existence assertion is idempotent. `context-changed`
// carries metadata and never moves a state. And an event this build has never
// heard of must not be guessed at. All three mean: leave a live session exactly
// where it is, and bring a dead or absent one back as idle.
//
// None of this absorbs an agent's habits. An agent that fires a hook at a
// moment its name does not describe is the adapter's problem to translate
// (D-31); core deciding otherwise would be core learning about one agent.
//
// That last clause is the resurrection rule, and it is general rather than a
// special case of `session-started`: any event arriving on an ended session
// revives it, so resume works even for an agent with no session-start hook at
// all.
func Reduce(previous Kernel, event Event) Kernel {
	switch event {
	case UserSentPrompt, AgentProgressed:
		return Working
	case TurnFinished:
		return FinishedATurn
	case TurnFailed:
		return Broke
	case TurnInterrupted:
		return Idle
	case BlockedOnHuman:
		return BlockedOnYou
	case SessionEnded:
		return Ended
	default:
		if previous.Live() {
			return previous
		}
		return Idle
	}
}

// Apply is Reduce plus everything else the event carries: the next record,
// given the one on disk. It is pure: the clock is a parameter and the result
// shares nothing mutable with previous. Key, Sequence, CreatedAt and UpdatedAt
// are the store's to stamp, on every write.
//
// A zero previous means a session nobody has recorded yet.
//
// Apply deliberately never touches Annotations. Annotations have their own
// write path and their own lifecycle, and the rule that a derived annotation
// dies with the captured context it came from needs provenance the record does
// not yet carry (§A18 Q15).
func Apply(previous Record, report Report, now time.Time) Record {
	next := previous.Clone()

	kernel := Reduce(previous.Kernel, report.Event)
	moved := kernel != previous.Kernel
	if moved || report.Event.AssertsState() {
		next.Kernel = kernel
		next.Detail = CleanLine(report.Detail)
		if moved {
			next.StateSince = now
			// Rank is only restamped when the kernel actually changed, so a
			// record carrying an unfamiliar kernel keeps the rank that came
			// with it rather than being handed RankUnknown by this build.
			next.Rank = kernel.Rank()
		}
	}

	switch {
	case moved && kernel == Ended:
		next.EndedAt = now
	case moved && previous.Kernel == Ended:
		// Resurrection. The session is back; it has no end.
		next.EndedAt = time.Time{}
	}

	if report.Message != nil {
		next.Message = CleanMessage(*report.Message)
	}
	if report.Name != "" {
		next.Name = CleanLine(report.Name)
	}
	if report.Cwd != "" {
		next.Cwd = CleanLine(report.Cwd)
	}
	if report.Branch != "" {
		next.Branch = CleanLine(report.Branch)
	}
	if report.Model != "" {
		next.Model = CleanLine(report.Model)
	}

	// Tokens accumulate, and doing so does not move state_since: a session that
	// has spent more is not a session that has been waiting for a shorter time
	// — the rule Name and the annotations already keep.
	next.Usage = next.Usage.Adding(report.Spent)
	if report.Process.PID != 0 {
		next.Process = report.Process
	}
	if report.CapturedContext != nil {
		// Replacing the captured context voids everything derived from it in
		// the same write. A pane id worked out from an environment that no
		// longer applies does not become merely out of date — it points at
		// somebody else's pane, and focusing it takes you confidently to the
		// wrong place, which §A11.3 calls worse than not going at all.
		next.CapturedContext = *report.CapturedContext
		next.DerivedContext = nil
		if next.CapturedContext.CapturedAt.IsZero() {
			next.CapturedContext.CapturedAt = now
		}
	}
	return next
}
