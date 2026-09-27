package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/lassoColombo/agent-notify/container"
	"github.com/lassoColombo/agent-notify/subscribe"
)

// Name is what this integration calls itself: its config table, its entry in
// `[container] order`, and its section of a record.
//
// The role is in the name because zellij is two programs (D-37). This one can
// take your cursor somewhere; agent-notify-zellij-display, installed
// separately, only writes titles.
const Name = "zellij-container"

// me is this integration: how everything here asks where agent-notify's files
// are, and what its own settings say.
//
// Root is left empty on purpose — empty means "wherever this process's
// environment says", which is right everywhere but a test (D-69).
var me = subscribe.Integration{Name: Name}

// The two variables zellij puts in every pane's environment. They exist only
// inside the agent's own process, which is why reading them is a subcommand run
// as a child of the hook rather than anything this program does later (D-27).
const (
	sessionVariable = "ZELLIJ_SESSION_NAME"
	paneVariable    = "ZELLIJ_PANE_ID"
)

// Captured is what `capture-environment` returns: exactly what the environment
// said, and nothing worked out from it.
//
// Nothing here asks zellij anything. That is the rule for this half and it is
// not a style preference: this runs inside the agent's process tree, on the path
// the agent waits on, and `zellij action` against a wedged server is precisely
// the thing that must never be there (R1, R3).
type Captured struct {
	Session string `json:"ZELLIJ_SESSION_NAME"`
	Pane    string `json:"ZELLIJ_PANE_ID"`
}

// Coordinates is what `interpret-environment` returns: where the session is,
// worked out by asking zellij, in the session-watcher, where blocking is allowed.
//
// The tab is here even though it is re-read at focus time, because it is what
// makes a record legible: "pane 17 of tab 1 in session home" is something a
// person can check, and a coordinate nobody can read is a coordinate nobody can
// debug.
type Coordinates struct {
	Session string `json:"session"`
	Pane    int    `json:"pane"`
	Tab     int    `json:"tab"`
	TabName string `json:"tab_name,omitempty"`
}

// ours reports whether a blob is this container's coordinates at all.
//
// The session name is the discriminator, and the pane id deliberately is not:
// pane 0 is a perfectly real pane in zellij, so "absent" and "the first one"
// would be the same value. Decoding is not strict, because a record written by
// a newer version of this program carries fields this one has never heard of
// and must still be usable (R12).
func (c Coordinates) ours() bool { return c.Session != "" }

// Capture reads the environment. It cannot fail in a way worth reporting: a
// session that is not in zellij returns empty, which the session-watcher will
// decline to interpret, and that is the ordinary case for an agent in a bare
// terminal rather than an error anybody should see.
func Capture() (any, error) {
	return Captured{
		Session: os.Getenv(sessionVariable),
		Pane:    os.Getenv(paneVariable),
	}, nil
}

// Interpret turns what Capture returned into coordinates, by asking zellij
// which tab holds that pane.
func Interpret(zellij Zellij, captured json.RawMessage) (any, error) {
	var given Captured
	if err := json.Unmarshal(captured, &given); err != nil {
		return nil, fmt.Errorf("what was captured is not this container's: %w", err)
	}
	if given.Session == "" || given.Pane == "" {
		return nil, fmt.Errorf("this session was not started inside zellij")
	}
	pane, err := strconv.Atoi(given.Pane)
	if err != nil {
		return nil, fmt.Errorf("%s was %q, which is not a pane id", paneVariable, given.Pane)
	}

	panes, err := zellij.Panes(given.Session)
	if err != nil {
		return nil, err
	}
	for _, one := range panes {
		if !one.Plugin && one.ID == pane {
			return Coordinates{
				Session: given.Session, Pane: pane,
				Tab: one.TabID, TabName: one.TabName,
			}, nil
		}
	}
	return nil, fmt.Errorf("pane %d is not in zellij session %q any more", pane, given.Session)
}

// NotAttached is a word core does not have yet, and the one this needs: the
// session is running, the pane is there, and nobody is looking at any of it.
//
// It is its own failure because its next move is its own: not "prune the
// record" (place-is-gone), not "start zellij" (not-running), but `zellij attach
// <session>` — after which focusing works exactly as it always did. Core
// carries a problem it has never heard of through untouched, so this can live
// here until core adopts the word (§A11.3).
const NotAttached = container.Problem("not-attached")

// Focus brings the pane to the front: its tab first, then the pane.
//
// It re-reads where the pane is rather than trusting the stored tab, because a
// pane can be moved between tabs and a coordinate is validated at the moment of
// use, never before it (R17). Taking somebody confidently to the wrong place is
// worse than not going (§A11.3).
//
// It also asks whether you are already there, and that is not an optimisation.
// zellij REFUSES to focus a pane that is already focused — `focus-pane-id` exits
// 2 with "Pane Terminal(60) is already focused" — so a focus that had nothing
// left to do would otherwise be reported as a failure, having in fact succeeded.
// Asking first makes focus idempotent, which is what anything anybody can click
// twice has to be.
//
// ── WHY THERE IS A CLIENT CHECK, AND A SECOND READ AFTERWARDS ───────────────
// Everything above was true of the version that reported success for a focus
// that moved nobody. Two facts about zellij make that possible, both measured
// on 0.45.1 (see the README):
//
//   - FOCUS EXISTS WITHOUT A VIEWER. `go-to-tab-by-id` and `focus-pane-id`
//     against a session no terminal is attached to both exit 0 and are both
//     obeyed: the stored focus moves, no tab becomes `active`, and the next
//     client to attach lands there. Nothing whatsoever appears on a screen. A
//     detached session is therefore not a focus that failed — it is a focus
//     with nobody to perform it for, and `list-clients` is the only place that
//     distinction is visible.
//   - THE EXIT CODE IS NOT EVIDENCE. zellij exits 0 whether or not an action
//     did anything at all. So the only honest proof that a focus landed is to
//     read the state back and see the pane in front, which is what the second
//     read is. It costs two subprocesses on a path a person just clicked.
//
// ── AND WHY A SESSION WITH NO VIEWER IS NOT ALWAYS THE END ──────────────────
// `not-attached` used to be the answer to every session nobody was looking at,
// and for most callers it still is. But when the asking comes from a terminal —
// the picker, opened by its keybinding in the session you are reading this in —
// that terminal is itself the viewer the session is missing, and zellij will
// move it (see [Zellij.SwitchSession]). That is the three-way switch below, and
// the client check is what it turns on.
func Focus(zellij Zellij, coordinates json.RawMessage) (container.Outcome, error) {
	var where Coordinates
	if err := json.Unmarshal(coordinates, &where); err != nil || !where.ours() {
		return container.Failed(container.NeverPlaced,
			"these are not this container's coordinates"), nil
	}

	panes, err := zellij.Panes(where.Session)
	if err != nil {
		return container.Failed(container.NotRunning, err.Error()), nil
	}
	pane, found := paneOf(panes, where.Pane)
	if !found {
		return container.Failed(container.PlaceIsGone,
			fmt.Sprintf("pane %d is not in zellij session %q any more", where.Pane, where.Session)), nil
	}
	tabs, err := zellij.Tabs(where.Session)
	if err != nil {
		return container.Failed(container.NotRunning, err.Error()), nil
	}
	// Already in front, so there is nothing to do and somebody is evidently
	// looking: a tab is `active` only while a client is on it, which is why
	// this can be answered before asking who is attached.
	if Verdict(panes, tabs, where.Pane).Answer == container.Yes {
		return container.Done(), nil
	}

	clients, err := zellij.Clients(where.Session)
	if err != nil {
		return container.Failed(container.NotRunning, err.Error()), nil
	}

	// Which session THIS program is running in, empty for every caller but a
	// terminal: the menu-bar item, a notification and anything else under
	// launchd have no zellij around them. It decides the middle case below,
	// because zellij moves a terminal between sessions only for a program that
	// terminal started ([Zellij.SwitchSession]).
	here := os.Getenv(sessionVariable)

	switch {
	case len(clients) > 0:
		// Somebody is looking at that session, so this is the ordinary work of
		// moving their tab and pane — never a switch, even when this is running
		// somewhere else. That viewer may be a second window the container
		// outside this one has just brought to the front, and taking the
		// terminal somebody is typing in to the same session would undo that
		// and move the wrong person.
		//
		// The session is alive, the pane is there and somebody is demonstrably
		// looking, so anything that goes wrong now is zellij declining rather
		// than zellij being absent — two words, because they deserve different
		// next moves.
		if err := zellij.GoToTab(where.Session, pane.TabID); err != nil {
			return container.Failed(container.Refused, err.Error()), nil
		}
		// The guard is "already in front of its own tab", not `pane.Focused`, and
		// the difference is a bug somebody had to close a floating pane by hand to
		// get past: a tab has TWO focused panes, so the focused TILED pane is
		// `Focused` and behind the floating layer at the same time. Skipping the
		// focus there skipped it in precisely the case that needed it most, because
		// `focus-pane-id` on a tiled pane is what LOWERS that layer — measured on
		// 0.45.1, it exits 0 and hides the floating panes rather than refusing.
		if !inFrontOfItsOwnTab(pane, tabs) {
			if err := zellij.FocusPane(where.Session, where.Pane); err != nil {
				return container.Failed(container.Refused, err.Error()), nil
			}
		}

	case here != "" && here != where.Session:
		// Nobody is looking at it, and this was asked from a terminal that can
		// come. One command is the whole of it: the terminal leaves the session
		// it is in, attaches to this one and lands on this pane with its tab
		// active. go-to-tab and focus-pane would instead be asking a session
		// nobody is in to rearrange itself, which is the silent no-op this
		// branch exists to stop reporting as a focus.
		if err := zellij.SwitchSession(where.Session, where.Pane); err != nil {
			return container.Failed(container.Refused, err.Error()), nil
		}
		// zellij answers before the terminal has arrived — measured at 25ms out
		// and 130ms in on 0.45.1 — so the read-back below would find a session
		// with nobody in it and call a switch that worked a refusal. Two seconds
		// is a hundred times the wait because it is here for the terminal that
		// never comes, and it stays inside core's five for one focus step.
		waited := time.Now().Add(2 * time.Second)
		for len(clients) == 0 {
			if time.Now().After(waited) {
				return container.Failed(container.Refused, fmt.Sprintf(
					"zellij took the switch and no terminal came to session %q", where.Session)), nil
			}
			time.Sleep(25 * time.Millisecond)
			if clients, err = zellij.Clients(where.Session); err != nil {
				return container.Failed(container.NotRunning, err.Error()), nil
			}
		}

	default:
		// Nobody is looking at it and nobody here can be brought: either nothing
		// says where this is running, or it is running in a pane of that same
		// session — a shell that outlived the terminal it was started in, and
		// switching a session to itself moves nobody.
		return container.Failed(NotAttached, fmt.Sprintf(
			"zellij session %q is running and no terminal is showing it, and this focus was "+
				"not asked for from inside a zellij pane, so there is nothing here to bring "+
				"— `zellij attach %s` first",
			where.Session, where.Session)), nil
	}

	panes, err = zellij.Panes(where.Session)
	if err != nil {
		return container.Failed(container.NotRunning, err.Error()), nil
	}
	tabs, err = zellij.Tabs(where.Session)
	if err != nil {
		return container.Failed(container.NotRunning, err.Error()), nil
	}
	if verdict := Verdict(panes, tabs, where.Pane); verdict.Answer != container.Yes {
		return container.Failed(container.Refused, fmt.Sprintf(
			"zellij took the focus commands and pane %d is still not in front: %s",
			where.Pane, verdict.Detail)), nil
	}
	return container.Done(), nil
}

// Focused answers whether that pane is the one a person is looking at.
//
// Three facts have to agree, and working that out was the only genuinely
// surprising thing about zellij's model. `is_focused` is per TAB, not per
// session — every tab has one — and a tab has TWO focused panes at once: the
// focused tiled one and the focused floating one. Which of those is in front
// depends on whether the tab's floating panes are currently visible. So:
//
//	the tab is active, and the pane is focused, and the pane's floating-ness
//	matches whether floating panes are being shown
//
// Anything this cannot establish is `cannot-tell` rather than `no`, because the
// caller's rule for the third answer is to notify anyway, and a wrong `no`
// silences a notification that should have fired (R27).
func Focused(zellij Zellij, coordinates json.RawMessage) (container.Verdict, error) {
	var where Coordinates
	if err := json.Unmarshal(coordinates, &where); err != nil || !where.ours() {
		return container.Verdict{Answer: container.CannotTell,
			Detail: "these are not this container's coordinates"}, nil
	}

	panes, err := zellij.Panes(where.Session)
	if err != nil {
		return container.Verdict{Answer: container.CannotTell, Detail: err.Error()}, nil
	}
	tabs, err := zellij.Tabs(where.Session)
	if err != nil {
		return container.Verdict{Answer: container.CannotTell, Detail: err.Error()}, nil
	}
	return Verdict(panes, tabs, where.Pane), nil
}

// Verdict is the pure half of Focused, so that the rule above can be tested
// against a table of panes and tabs rather than against a terminal somebody has
// to be looking at.
func Verdict(panes []Pane, tabs []Tab, pane int) container.Verdict {
	var found *Pane
	for i := range panes {
		if !panes[i].Plugin && panes[i].ID == pane {
			found = &panes[i]
			break
		}
	}
	if found == nil {
		// The pane is gone, so you are certainly not looking at it. This is the
		// one place a definite "no" is safe.
		return container.Verdict{Answer: container.No,
			Detail: fmt.Sprintf("pane %d is not there", pane)}
	}

	for _, tab := range tabs {
		if tab.ID != found.TabID {
			continue
		}
		switch {
		case !tab.Active:
			return container.Verdict{Answer: container.No,
				Detail: fmt.Sprintf("tab %d is not the active one", tab.ID)}
		case !found.Focused:
			return container.Verdict{Answer: container.No,
				Detail: fmt.Sprintf("pane %d is not the focused pane of tab %d", pane, tab.ID)}
		case found.Floating != tab.FloatingVisible:
			// The focused floating pane while floating panes are hidden, or
			// the focused tiled pane while they are shown over it.
			return container.Verdict{Answer: container.No,
				Detail: fmt.Sprintf("pane %d is focused but covered", pane)}
		}
		return container.Verdict{Answer: container.Yes}
	}
	return container.Verdict{Answer: container.CannotTell,
		Detail: fmt.Sprintf("zellij did not report tab %d", found.TabID)}
}

// inFrontOfItsOwnTab reports whether the pane is the one its tab is showing:
// focused, and on the layer that is currently on top.
//
// It is Verdict's rule without the tab-active clause, because it is asked after
// go-to-tab has made that tab the active one. A tab zellij did not report
// answers false — a focus that had nothing to do costs at worst a spurious
// refusal to report, and the mistake on the other side is a person sent to a
// pane they cannot see.
func inFrontOfItsOwnTab(pane Pane, tabs []Tab) bool {
	for _, tab := range tabs {
		if tab.ID == pane.TabID {
			return pane.Focused && pane.Floating == tab.FloatingVisible
		}
	}
	return false
}

func paneOf(panes []Pane, pane int) (Pane, bool) {
	for _, one := range panes {
		if !one.Plugin && one.ID == pane {
			return one, true
		}
	}
	return Pane{}, false
}
