package agentnotify

// Event is what an agent-integration says happened. Closed and versioned, like
// the kernel, and for the same reason: an adapter that could invent events
// would be an adapter that decides what your bar counts (plan.md §A5.7).
//
// An adapter may map one of its agent's hooks to nothing at all. Claude's idle
// nudges, auth-success notices and quota chatter must never escalate, and
// knowing which is which is agent-specific knowledge that belongs in the
// adapter rather than here.
type Event string

// The nine events.
const (
	// SessionStarted — the agent announced a session.
	//
	// It asserts that a session exists rather than that something changed,
	// which is why it leaves a live session alone: asserting the existence of
	// something that already exists is idempotent. An adapter should not emit
	// it for anything that is not a session beginning or resuming — a
	// compaction is the agent working (D-31).
	SessionStarted Event = "session-started"
	// UserSentPrompt — you asked it for something.
	UserSentPrompt Event = "user-sent-prompt"
	// AgentProgressed — it is still going. A finished subagent maps here: the
	// event both previous implementations had to throw away, because an adapter
	// could not see the parent state it would have overwritten.
	AgentProgressed Event = "agent-progressed"
	// TurnFinished — it produced an answer and stopped.
	TurnFinished Event = "turn-finished"
	// TurnFailed — the turn died without finishing.
	TurnFailed Event = "turn-failed"
	// TurnInterrupted — the turn was stopped on purpose, by the person sitting
	// there. Nothing was produced and nothing is owed.
	//
	// It is not TurnFinished, which promises an answer to go and read; it is
	// not TurnFailed, which is loud about something nobody chose. The state it
	// leaves is idle, and the whole point is that a notifier stays quiet:
	// announcing "your agent finished" a tenth of a second after you pressed
	// Esc is a false statement caused by your own keypress (D-36).
	TurnInterrupted Event = "turn-interrupted"
	// BlockedOnHuman — it cannot continue until you answer.
	BlockedOnHuman Event = "blocked-on-human"
	// SessionEnded — it is gone. The detail says why.
	SessionEnded Event = "session-ended"
	// ContextChanged — metadata only: cwd, model, whatever the agent knows
	// about itself. Never a state change.
	ContextChanged Event = "context-changed"
)

// events maps each event to whether it asserts a state.
//
// The three that do not — and any event a newer adapter invents, which lands in
// the same bucket — carry metadata and leave the kernel, the detail and
// state_since exactly where they were.
var events = map[Event]bool{
	SessionStarted:  false,
	UserSentPrompt:  true,
	AgentProgressed: true,
	TurnFinished:    true,
	TurnFailed:      true,
	TurnInterrupted: true,
	BlockedOnHuman:  true,
	SessionEnded:    true,
	ContextChanged:  false,
}

// Known reports whether this build understands the event.
//
// An unknown event is a mistake in an adapter — every adapter is built against
// this source (D-77) — and the reducer still refuses to guess at it: it is
// treated as metadata, because the session it names may not exist yet and
// learning that it exists is worth something, but it may not move a state whose
// meaning nothing here knows.
func (e Event) Known() bool {
	_, ok := events[e]
	return ok
}

// AssertsState reports whether this event makes a claim about what the agent is
// doing, as opposed to telling us something about the session.
func (e Event) AssertsState() bool { return events[e] }

// Events returns the nine, in the order they are documented.
func Events() []Event {
	return []Event{
		SessionStarted, UserSentPrompt, AgentProgressed, TurnFinished,
		TurnFailed, TurnInterrupted, BlockedOnHuman, SessionEnded, ContextChanged,
	}
}
