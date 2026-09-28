package session

// What a display is handed: everything, and what moved.
//
// It lives here rather than in the SDK because it stopped being a Go type. It
// is what `render` reads on its stdin and what `agent-notify tail --json`
// prints, which makes it a published shape somebody can render from in any
// language — the same move [Hello] made, and for the same reason.

// View is the world as it stands.
type View struct {
	// Sessions is everything, most urgent first, already ordered by the rule
	// every display would otherwise write for itself (R24).
	Sessions []Record `json:"sessions"`

	// Changed is what moved since the last call, most urgent first. A renderer
	// ignores it; a notifier reads it and nothing else.
	//
	// "Since the last call" is meant literally, and it is the whole contract:
	// it survives a reconnection, an overflow and a resync, because what it is
	// compared against is what this display was last SHOWN rather than what
	// arrived on this particular connection. On the very first call it is
	// empty — nothing has moved when there was no previous call — which is
	// what stops a display that started thirty seconds ago opening with a
	// banner for every agent that happens to be blocked.
	Changed []Change `json:"changed,omitempty"`

	// Why is "snapshot" or "delta", for a log line.
	Why string `json:"why,omitempty"`
}

// Change is one session that moved, and what it moved from.
//
// It exists because a notifier is the one display that genuinely needs the
// transition rather than the state (§A12.1), and everything needed to give it
// one is already here: the session-watcher puts the previous kernel and the
// event in every delta, and whoever holds the last view knows the previous
// kernel even for a change it learns about from a snapshot. Before this, both
// were decoded and dropped, and the one notifier in the world rebuilt a weaker
// version of them from remembered timestamps.
//
// The record is a named field rather than an embedded one on purpose: Record
// has a MarshalJSON, and embedding it would mean a Change logged as JSON
// silently came out as the record alone, with the two fields that make it a
// change missing.
type Change struct {
	// Record is the session as it now stands.
	Record Record `json:"record"`

	// PreviousKernel is the kernel this display last saw it in, in the
	// vocabulary [Delta] uses for the same fact. It is empty for a session it
	// has never seen before, which is not a transition at all — it is an
	// arrival.
	//
	// `PreviousKernel == Record.Kernel` is the ordinary case and means
	// something other than the state moved: a new message, a rename, a fresh
	// token count. A notifier skips those; a preview pane wants them.
	PreviousKernel Kernel `json:"previous_kernel,omitempty"`

	// Event is what the agent-integration reported, when this change arrived
	// as a delta and something reported it.
	//
	// It is empty in two honest cases, and neither means "nothing happened":
	// a change the session-watcher OBSERVED rather than was told about carries
	// no event (D-12 — death and supersession), and a change first seen in a
	// snapshot was never accompanied by one. PreviousKernel is the field to
	// reason from; this one is for saying why out loud.
	Event Event `json:"event,omitempty"`
}

// LastShown is the world as one display was last handed it.
//
// It exists so that [View.Changed] can mean "since the last view" rather than
// "since this connection opened", and it is kept by whoever does the handing:
// core, for a display it runs; the SDK, for one that connects. Either way it
// outlives the thing on the other end, which is the point. A session-watcher
// restarting hands out a fresh picture of the world, and a picture that began
// empty at the same moment reports every session in it as changed — so a
// notifier written to the documented contract would post a banner per live
// agent every time the daemon came back.
type LastShown struct {
	sessions map[string]Record
	// shown is false until a view has been handed over. The first one carries
	// no changes: "since the last view" is nothing when there was no last view.
	shown bool
}

// HasSeenAView reports whether anything has been handed over yet.
//
// It is asked by a caller deciding whether there is any point handing one over
// now: the first view is always worth it — it is the world arriving — and after
// that only a view with something in it is.
func (l *LastShown) HasSeenAView() bool { return l.shown }

// Replace swaps the whole world in and reports what actually moved.
//
// It compares on the fields this display asked to be woken for, which is the
// same rule applied before it is woken at all. Comparing on everything instead
// meant the two ends of one field disagreed about what "changed" means: a
// display that declared `wake_on = ["kernel"]` is never woken by a new message,
// and used to find one in Changed anyway if it arrived while it was away.
//
// The previous kernel is carried out with each one. A whole world says nothing
// about how a session got where it is, but whoever has been shown a view before
// knows what it last saw, and that is the same fact.
func (l *LastShown) Replace(fresh []Record, wakeOn []string) []Change {
	if l.sessions == nil {
		l.sessions = make(map[string]Record, len(fresh))
	}
	was := make(map[string]Kernel, len(fresh))
	var moved []Record
	arrived := make(map[string]bool, len(fresh))

	for _, record := range fresh {
		key := record.Key.String()
		arrived[key] = true
		if previous, have := l.sessions[key]; !have || Differs(previous, record, wakeOn) {
			was[key] = previous.Kernel
			moved = append(moved, record)
		}
		l.sessions[key] = record
	}
	for key := range l.sessions {
		if !arrived[key] {
			delete(l.sessions, key)
		}
	}

	// Ordered here, while these are still records, so that the one urgency
	// rule does the work rather than a second spelling of it (R24).
	ByUrgency(moved)
	changed := make([]Change, 0, len(moved))
	for _, record := range moved {
		changed = append(changed, Change{Record: record, PreviousKernel: was[record.Key.String()]})
	}
	return changed
}

// Applied puts one record in and says whether it was worth keeping.
//
// False means it is older than what is already held, which is what stops a late
// delta undoing a fresher picture (R16).
func (l *LastShown) Applied(record Record) bool {
	if l.sessions == nil {
		l.sessions = map[string]Record{}
	}
	key := record.Key.String()
	if was, have := l.sessions[key]; have && was.Sequence > record.Sequence {
		return false
	}
	l.sessions[key] = record
	return true
}

// Forget drops one, and says whether it was there to drop.
func (l *LastShown) Forget(key string) bool {
	if _, have := l.sessions[key]; !have {
		return false
	}
	delete(l.sessions, key)
	return true
}

// ViewOf is the view to hand over: everything held, most urgent first, with
// these changes — or with none at all if this is the first.
func (l *LastShown) ViewOf(changed []Change, why string) View {
	all := make([]Record, 0, len(l.sessions))
	for _, record := range l.sessions {
		all = append(all, record)
	}
	ByUrgency(all)

	if !l.shown {
		// The first view is the world arriving, not the world moving. Every
		// session in it would otherwise read as a change, which is a restart
		// telling you about the past.
		changed = nil
		l.shown = true
	}
	return View{Sessions: all, Changed: changed, Why: why}
}
