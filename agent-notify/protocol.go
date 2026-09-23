package agentnotify

import (
	"encoding/json"
	"fmt"
)

// The protocol a tool-integration speaks: newline-delimited JSON over a stream
// socket, in both directions (plan.md §A13).
//
// It is written down here, in the public package, because it is a published
// interface between separately released repositories — an author who cannot use
// the Go SDK speaks this directly, in whatever language they have.
//
// Every message carries `kind`, and a reader dispatches on it.

// Message kinds, client to server.
const (
	KindHello  = "hello"
	KindResync = "resync"
)

// Message kinds, server to client.
const (
	KindWelcome  = "welcome"
	KindRefused  = "refused"
	KindSnapshot = "snapshot"
	KindDelta    = "delta"
	KindGone     = "gone"
)

// Hello is the first thing a subscriber says, and the only thing it must say.
//
// Everything in it is something the session-watcher must not guess (§A10.3).
type Hello struct {
	Kind string `json:"kind"`
	// Name is what this integration calls itself. The session-watcher stamps
	// it on anything the integration writes, so that a writer never names
	// itself and cannot name anybody else (R25).
	Name string `json:"name"`
	// Version is the core version this was built against. It is announced and
	// logged, never negotiated on: everything here is built and released
	// together, so a version that did not match would be a deployment somebody
	// half finished rather than a protocol to arbitrate (D-77).
	Version string `json:"version"`
	// Roles are what it does: display, container, enricher, or something core
	// has never heard of, which is carried and ignored rather than refused.
	Roles []string `json:"roles,omitempty"`
	// WakeOn names the record fields it cares about, so that nothing is woken
	// for a change it does not care about (R23). Empty means everything.
	WakeOn []string `json:"wake_on,omitempty"`
	// WantEnded asks for ended sessions in the opening snapshot. A bar says no
	// and a picker says yes; the *transition* to ended is delivered either way,
	// because that is how a bar learns to remove the row (D-26).
	WantEnded bool `json:"want_ended,omitempty"`
}

// There was an `environment` field here, naming the variables an integration
// wanted captured out of an agent's process, and it is gone (D-57).
//
// It could never have worked. Capture happens in the hook, the hook has no
// socket, and this message arrives over one — so the declaration reached the
// session-watcher a long time after the only process that could have acted on
// it had exited (D-39). Nothing ever read it. An integration that needs
// variables reads them itself, in `capture-environment`.

// Welcome accepts the connection.
type Welcome struct {
	Kind    string `json:"kind"`
	Version string `json:"version"`
	// Since is when this session-watcher started. A subscriber that sees it
	// change knows the session-watcher was restarted under it.
	Since string `json:"since,omitempty"`
}

// Refused declines it, permanently, and says why.
//
// There is no negotiation and no retry: a subscriber that said something the
// session-watcher cannot act on is told so once, in words, rather than being
// left to reconnect into the same answer.
type Refused struct {
	Kind    string `json:"kind"`
	Reason  string `json:"reason"`
	Version string `json:"version"`
}

// Snapshot is every session the subscriber asked to see, and it always arrives
// before any delta.
//
// It is also what a queue overflow degrades to, and what answering a resync
// sends: a display must be able to arrive at any moment and be correct
// (§A12.2).
type Snapshot struct {
	Kind     string   `json:"kind"`
	Sessions []Record `json:"sessions"`
	// Why is "connect", "asked" or "overflow". Nothing needs to act on it; it
	// is there so that a log can explain a redraw.
	Why string `json:"why,omitempty"`
}

// Delta is one session that changed.
//
// It carries the whole record rather than "session X changed, re-read", because
// the stream has no size limit and a record is about a kilobyte — sending it
// takes the store off the hot read path entirely (§A13.1).
type Delta struct {
	Kind    string `json:"kind"`
	Session Record `json:"session"`
	// PreviousKernel saves a subscriber from re-examining a record when nothing
	// it cares about moved. It is an optimisation, never the mechanism:
	// correctness rests on comparing what was last done with what is true now
	// (R22).
	PreviousKernel Kernel `json:"previous_kernel,omitempty"`
	Event          Event  `json:"event,omitempty"`
}

// Gone says a session has been forgotten entirely — pruned past
// `keep-ended-sessions` — so that a display's map does not keep it forever.
type Gone struct {
	Kind string `json:"kind"`
	Key  Key    `json:"key"`
}

// Resync asks for a fresh snapshot. `sketchybar --reload` restarts a whole bar,
// and a display that cannot ask ends up blank until an agent happens to do
// something (§A12.2).
type Resync struct {
	Kind string `json:"kind"`
}

// KindOf reads the kind out of a line without decoding the rest of it.
func KindOf(line []byte) (string, error) {
	var envelope struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(line, &envelope); err != nil {
		return "", fmt.Errorf("not a protocol message: %w", err)
	}
	if envelope.Kind == "" {
		return "", fmt.Errorf("a protocol message with no kind")
	}
	return envelope.Kind, nil
}
