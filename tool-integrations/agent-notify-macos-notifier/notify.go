package main

import (
	"github.com/lassoColombo/agent-notify/session"
)

// A notification is the one EDGE in this system, and this file is where the
// level is turned back into one.
//
// Everything else here is level-triggered on purpose (R22): a display is handed
// the current state, never a transition, so it cannot get out of step after a
// dropped message or a restart. A banner cannot work that way — it is posted
// once, at a moment, and a display that re-posted it on every repaint would put
// forty banners on the screen for one thing happening.
//
// §A12.1 predicted exactly this: of every display worth imagining, the notifier
// is the one that needs the transition rather than the state. It used to
// MANUFACTURE one — a map of the `state_since` it had last spoken about per
// session, a flag for whether it had ever looked, and a sweep to keep the map
// bounded by what was running. It does not any more. The SDK hands it
// View.Changed, and every entry carries the kernel its session moved FROM
// (D-63, D-71).
//
// So this file remembers nothing at all, and that is the point rather than a
// tidy-up. The memory did not go away, it moved to the only place that can
// keep it right: a subscriber's picture of what it was last SHOWN, which
// survives a reconnection, an overflow and a resync — three things this
// program cannot see happen. What is left here is the part that was always
// this program's alone, which is deciding which transitions are worth
// interrupting somebody for.

// Notice is one notification, ready to post.
type Notice struct {
	// Key is the session, and also the notification's identifier: a second
	// notice about one session replaces the first rather than stacking under it.
	Key      string
	Title    string
	Subtitle string
	Body     string
	// Colour is the state's, in the spelling the rest of this system writes
	// colours in. What is DONE with it is the caller's business — it becomes a
	// picture beside the text — but which colour it is is decided here, with
	// the record in hand, and nowhere else.
	Colour string
}

// Notifier decides what to say out loud.
//
// It holds nothing per session. What has already been said is not a thing this
// has to remember any more — see the note at the top of this file.
type Notifier struct {
	Preview Preview
	// Colours is the invader's colour per state, the same table the bar wears.
	Colours session.Palette
}

func NewNotifier(preview Preview, colours session.Palette) *Notifier {
	return &Notifier{Preview: preview, Colours: colours}
}

// Fresh is what to say about what just moved.
//
// Three rules, and every one of them is about the record in hand rather than
// about anything remembered:
//
//   - THE STATE HAS TO HAVE MOVED. A change is anything this display asked to
//     be woken for, and it asked for `message` as well, because the message is
//     the body of the banner. So a blocked agent revising what it said arrives
//     here again and again, and must not produce a banner each time. The
//     kernel it moved from is what tells the two apart, and before D-63 it was
//     decoded by the SDK and thrown away.
//   - IT HAS TO BE WORTH INTERRUPTING FOR, which is Record.WantsYou: a rank
//     above working. Rank rather than a list of kernels, so that a state a
//     newer core ships is judged by the number it carries rather than by what
//     this build happened to be compiled with (§A5.5).
//   - IT HAS TO BE LIVE. The transition to ended reaches even a display that
//     asked for no ended sessions, because that is how a bar learns to take a
//     row away (D-26), and "it finished" is not something to interrupt anybody
//     with.
func (n *Notifier) Fresh(changed []session.Change) []Notice {
	var notices []Notice
	for _, change := range changed {
		record := change.Record
		if change.PreviousKernel == record.Kernel {
			// Something moved, and it was not the state: a new message, a
			// renamed session, a working directory. Nothing to say out loud.
			continue
		}
		if !record.Kernel.Live() || !record.WantsYou() {
			continue
		}
		notices = append(notices, Notice{
			Key:      record.Key.String(),
			Title:    record.DisplayName(),
			Subtitle: State(record),
			Body:     n.Preview.Said(record),
			Colour:   n.Colours.For(record),
		})
	}
	return notices
}
