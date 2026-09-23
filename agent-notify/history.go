package agentnotify

import "time"

// History is what a session carries beyond its current record: the last few
// things it said, and the last few times its state moved (plan.md §A7.7).
//
// It is read on demand, for one session, when somebody hovers over a chip or
// opens a preview — never delivered with a delta. That asymmetry is the whole
// reason it is a separate file: keeping it out of the record is what stops a
// delta growing from a kilobyte to twenty.
//
// Both halves are bounded by count rather than by time, and both are deleted
// with the session they describe, so history needs no retention rule of its own
// and cannot outlive what it describes.
type History struct {
	Messages []SaidSomething `json:"messages,omitempty"`
	Changes  []StateChange   `json:"changes,omitempty"`
}

// SaidSomething is one thing an agent said, with the state it was in when it
// said it — because the (kernel, detail) pair is what says how to read the text
// (D-17), and in history there is no current state to fall back on.
type SaidSomething struct {
	At       time.Time `json:"at"`
	Sequence uint64    `json:"sequence"`
	Kernel   Kernel    `json:"kernel"`
	Detail   string    `json:"detail,omitempty"`
	Message  string    `json:"message"`
}

// StateChange is one transition. `From` is empty for the first one, because
// there was nothing before it.
type StateChange struct {
	At       time.Time `json:"at"`
	Sequence uint64    `json:"sequence"`
	From     Kernel    `json:"from,omitempty"`
	To       Kernel    `json:"to"`
	Detail   string    `json:"detail,omitempty"`
}

// Bounds is how many of each to keep. Zero keeps none, which §A15 requires to
// be expressible: somebody who does not want an agent's words on disk at all
// must be able to say so.
type Bounds struct {
	Messages int
	Changes  int
}

// Record appends whatever the transition from previous to next is worth
// recording, and trims to the bounds.
//
// Nothing is appended for a write that changed neither the state nor the text,
// which is the common case — an agent reporting continued progress — so most
// writes touch no history file at all.
func (h History) Record(previous, next Record, bounds Bounds) (History, bool) {
	appended := false

	if next.Message != "" && next.Message != previous.Message {
		h.Messages = append(h.Messages, SaidSomething{
			At: next.UpdatedAt, Sequence: next.Sequence,
			Kernel: next.Kernel, Detail: next.Detail, Message: next.Message,
		})
		appended = true
	}
	if next.Kernel != previous.Kernel {
		h.Changes = append(h.Changes, StateChange{
			At: next.StateSince, Sequence: next.Sequence,
			From: previous.Kernel, To: next.Kernel, Detail: next.Detail,
		})
		appended = true
	}
	if !appended {
		return h, false
	}

	h.Messages = lastN(h.Messages, bounds.Messages)
	h.Changes = lastN(h.Changes, bounds.Changes)
	return h, true
}

// lastN keeps the tail, because the newest entries are the ones anybody asked
// for. A bound of zero keeps nothing at all rather than keeping everything,
// which is the reading that makes "set it to zero" mean what it says.
func lastN[T any](items []T, keep int) []T {
	if keep <= 0 {
		return nil
	}
	if len(items) <= keep {
		return items
	}
	return items[len(items)-keep:]
}
