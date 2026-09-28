package session

import (
	"encoding/json"
	"fmt"
)

// The protocol core's own client speaks to the session-watcher:
// newline-delimited JSON over a stream socket (plan.md §A13).
//
// Three messages, and the conversation is almost one-way. A subscriber says
// hello; it is welcomed or refused; and from then on it is sent the whole world
// whenever something it asked about moves. There is no message for "this one
// session changed", none for "this one is gone", and none for "send it all
// again", because a whole world is the answer to all three.
//
// What a DISPLAY sees is not this. Core hands a display a [View] — on stdin for
// one it runs, on stdout for one reading `agent-notify tail --json` — and a view
// is the world plus what changed in it since that display last looked. Only
// something with a memory of its own can say that, which is why the world is
// what crosses the socket and the view is made at whichever end survives.
//
// Every message carries `kind`, and a reader dispatches on it.

// Message kinds, client to server.
const KindHello = "hello"

// Message kinds, server to client.
const (
	KindWelcome  = "welcome"
	KindRefused  = "refused"
	KindSnapshot = "snapshot"
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
	// WakeOn names the record fields it cares about, so that nothing is woken
	// for a change it does not care about (R23). Empty means everything.
	WakeOn []string `json:"wake_on,omitempty"`
	// WantEnded asks for ended sessions. A bar says no and a picker says yes;
	// a bar learns to remove the row by the row not being in the next world it
	// is sent, which is D-26 as a mechanism rather than a convention.
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

// There was a `roles` field here too — "display", "container", "enricher" — and
// it is gone for the opposite reason: it arrived, it was logged, it was copied
// into three structs, and not one line of code ever branched on it.
//
// A role is a category a person infers and then writes down, and writing it
// down twice is how the two copies come to disagree. What core actually needs
// is what it is about to run, which is [Capabilities.Methods]: "is a container"
// became "answers `focus`", and that one cannot be wrong.

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

// Snapshot is every session the subscriber asked to see, and it is the only
// thing the session-watcher ever sends it.
//
// It carries whole records rather than "these keys changed, re-read them",
// because the stream has no size limit and a record is about a kilobyte —
// sending them takes the store off the hot read path entirely (§A13.1). A
// subscriber that has just connected, one that fell behind, and one that has
// been up for a week all read the same message, which is why arriving at any
// moment and being correct is structural here rather than a case that has to be
// handled (§A12.2).
type Snapshot struct {
	Kind     string   `json:"kind"`
	Sessions []Record `json:"sessions"`
}

// There were three more messages here, and all three answered a question a
// whole world answers by itself.
//
// `delta` said one session had changed, and carried the kernel it moved from
// and the event that caused it. The kernel it moved from is something the far
// end already knows, because it holds what it was last shown; the event was
// read by nobody, anywhere, in the whole system.
//
// `gone` said a session had been pruned, so that nobody's map kept it for ever.
// A map rebuilt from the world it was just sent cannot keep it.
//
// `resync` asked for a fresh snapshot, for a display that had been restarted
// under core — `sketchybar --reload` was the case, and sketchybar went in D-79.
// Every write is a fresh snapshot now, so there is nothing to ask for.

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
