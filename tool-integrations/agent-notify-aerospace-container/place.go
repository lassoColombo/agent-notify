package main

import (
	"encoding/json"
	"fmt"
	"github.com/lassoColombo/agent-notify/subscribe"
	"os"
	"strings"

	"github.com/lassoColombo/agent-notify/container"
)

// Name is what this integration calls itself: its config table, its entry in
// `[container] order`, and its section of a record.
//
// The role is in the name for the same reason zellij's is (D-37), even though
// aerospace is a container and nothing else. A person reading
// `order = ["aerospace-container", "zellij-container"]` should not have to know
// which of those two spellings means what.
const Name = "aerospace-container"

// me is this integration: how everything here asks where agent-notify's files
// are, and what its own settings say.
//
// Root is left empty on purpose — empty means "wherever this process's
// environment says", which is right everywhere but a test (D-69).
var me = subscribe.Integration{Name: Name}

// ── why this container looks nothing like zellij's ───────────────────────────
//
// zellij's container asks the agent's own environment where it is and is told:
// ZELLIJ_PANE_ID is simply there. A WINDOW is not, and three measurements say
// there is no walk from an agent to its window at all
// [verified 2026-09-18, macOS 26, aerospace 0.20.3, Ghostty 1.3.1]:
//
//   - Nothing in a shell's environment names a window. Ghostty hands down
//     GHOSTTY_BIN_DIR, GHOSTTY_RESOURCES_DIR and TERM_PROGRAM and no id of any
//     kind, and its CLI will not help either: `ghostty +new-window` answers
//     "not supported on this platform", which is also the answer to every other
//     way of asking it about a window.
//   - A pid names an APPLICATION, not a window, and aerospace answers in those
//     terms: `list-windows` reports an `app-pid` per window, and two windows of
//     one application carry the same one. Measured with a stand-in that could
//     be opened and closed safely — two TextEdit windows, one process, one pid
//     under both of them. So a pid narrows the window list to one application
//     and no further, and whether that is one window or five is a person's
//     window habits rather than anything the session knows.
//   - Inside a multiplexer the chain does not even reach the terminal. An
//     agent's ancestry ends at the zellij SERVER, whose parent is 1; the client
//     that draws it is in a different tree entirely, under the terminal. This
//     one is not a subtlety — it is the ordinary case, and it is what the
//     title key exists for.
//
// So the window is found rather than known, and it is found from two keys,
// neither of which is sufficient alone:
//
//   - **The process chain**, captured inside the agent. The nearest ancestor
//     that owns any window is the terminal, and when it owns exactly one the
//     answer is exact. This is the whole of the no-multiplexer case, and it is
//     the only key that is exact.
//   - **A window-title prefix**, built from the agent's environment by a
//     template. It is what survives a multiplexer: zellij titles its terminal
//     window `<session> | <active tab>`, so the session name the agent already
//     carries is an index into the window list. It is a lookup key rather than
//     a fact — nothing here would be believed if the title said something else.
//
// The template is configuration, not code, so that the next multiplexer is a
// line in a file (§A11.4). The default is zellij's convention.
//
// **The window is never stored, and that is the design decision M15 turned on
// (D-44).** Every other coordinate in this system is a property of the session:
// a pane belongs to its zellij session for as long as both exist. A window is
// not — it merely SHOWS a session, right now, and stops the moment somebody
// detaches. A stored window id would therefore be a cached guess rather than a
// coordinate, and §A11.3 forbids trusting those. So interpretation here is
// pure: it shapes the keys and asks aerospace nothing. The lookup happens in
// `focus` and `focused`, where the answer is used in the same millisecond it is
// obtained.
//
// The idea this replaced, for the record: capture `list-windows --focused` once
// when the session starts, on the theory that you are looking at the terminal
// when you type `claude`. A hook fires whenever the agent does something, and
// what is focused then is where the PERSON is, not where the agent is — and a
// captured context is kept until the agent's process changes, so one wrong
// answer would be wrong for the life of the session.

// Ancestor is one rung of the process chain: enough to look a pid up in a
// window list, and enough for a person to recognise what they are reading.
type Ancestor struct {
	PID     int    `json:"pid"`
	Command string `json:"command,omitempty"`
}

// Captured is what `capture-environment` returns: the process chain, and the
// values of whatever variables the title template names.
//
// Nothing here asks aerospace anything. That is the rule for this half and it
// is not a style preference: this runs inside the agent's process tree, on the
// path the agent waits on (R1, R3).
type Captured struct {
	Chain  []Ancestor        `json:"chain,omitempty"`
	Values map[string]string `json:"values,omitempty"`
}

// Coordinates is what `interpret-environment` returns: the two keys, ready to
// look up. It is deliberately not a place — see the header.
type Coordinates struct {
	Chain []Ancestor `json:"chain,omitempty"`
	Title string     `json:"title,omitempty"`
}

// ours reports whether a blob is this container's coordinates at all.
//
// Either key alone is enough to recognise them, because either key alone is
// enough to find a window. Decoding is not strict: a record written by a newer
// version of this program carries fields this one has never heard of and must
// still be usable (R12).
func (c Coordinates) ours() bool { return len(c.Chain) > 0 || c.Title != "" }

// Capture reads the process chain and the variables the template names.
//
// It cannot fail in a way worth reporting. A session whose template variables
// are all unset returns a chain and no values, which is the ordinary case for
// an agent in a bare terminal rather than an error anybody should see.
func Capture(template string) (any, error) {
	values := map[string]string{}
	for _, name := range placeholders(template) {
		if value, present := os.LookupEnv(name); present && value != "" {
			values[name] = value
		}
	}
	if len(values) == 0 {
		values = nil
	}
	return Captured{Chain: Ancestry(Self()), Values: values}, nil
}

// Interpret shapes what Capture returned into the two keys.
//
// It is pure, and the header says why: there is nothing about a window that is
// worth remembering. What it does do is decide, once, whether this session has
// any key at all — and answer `null` when it has none, which core reads as
// "this layer has nothing to do for this session" and steps past (D-45).
func Interpret(template string, captured json.RawMessage) (any, error) {
	var given Captured
	if err := json.Unmarshal(captured, &given); err != nil {
		return nil, fmt.Errorf("what was captured is not this container's: %w", err)
	}

	coordinates := Coordinates{Chain: given.Chain}
	if title, complete := substitute(template, given.Values); complete {
		coordinates.Title = title
	}
	if !coordinates.ours() {
		return nil, nil
	}
	return coordinates, nil
}

// Placement is which windows could be this session, and why that is not an
// answer when it is not one.
//
// Windows is in preference order and the first is the one to go to. It is a
// list rather than a single window because `focused` has a use for the rest:
// two terminal windows attached to the same multiplexer session both show it,
// so either being in front means you are looking at it.
type Placement struct {
	Windows []Window
	Problem container.Problem
	Detail  string
}

// Resolve picks the window, out of every window aerospace knows about.
//
// It is pure, which is what lets the whole of the interesting behaviour be
// tested on a machine with no window manager on it: the world arrives as a list
// and what comes back is a decision.
//
// The order of preference is the order of specificity. Both keys agreeing is
// the best evidence there is; the title alone is next, because it is about the
// session rather than about a process id that may have been recycled; the chain
// alone is last, and is only an answer when the terminal has exactly one
// window.
func Resolve(windows []Window, c Coordinates) Placement {
	if !c.ours() {
		return Placement{Problem: container.NeverPlaced,
			Detail: "these are not " + Name + "'s coordinates"}
	}

	byTitle := withTitle(windows, c.Title)
	byApp := ofNearestOwner(windows, c.Chain)

	switch {
	case len(byApp) > 0 && len(byTitle) > 0:
		if both := intersect(byApp, byTitle); len(both) > 0 {
			return Placement{Windows: both}
		}
		// The two keys disagree, which means the chain is stale: a terminal
		// that was restarted hands its pid to somebody else, while the title
		// still names the session. Believe the key that is about the session.
		return Placement{Windows: byTitle}

	case len(byTitle) > 0:
		return Placement{Windows: byTitle}

	case len(byApp) == 1:
		return Placement{Windows: byApp}

	case len(byApp) > 1:
		return Placement{Windows: byApp, Problem: container.Ambiguous,
			Detail: fmt.Sprintf(
				"%d %s windows could be this session and nothing tells them apart",
				len(byApp), byApp[0].Name)}

	case c.Title != "":
		return Placement{Problem: container.PlaceIsGone,
			Detail: fmt.Sprintf("no window is titled %q — nothing is showing this session", c.Title)}

	default:
		return Placement{Problem: container.NeverPlaced,
			Detail: "nothing in this session's process chain owns a window"}
	}
}

// Focus brings the window forward.
func Focus(a Aerospace, raw json.RawMessage) (container.Outcome, error) {
	var coordinates Coordinates
	if err := json.Unmarshal(raw, &coordinates); err != nil {
		return container.Failed(container.NeverPlaced,
			"these are not "+Name+"'s coordinates"), nil
	}

	windows, err := a.Windows()
	if err != nil {
		return container.Failed(container.NotRunning, err.Error()), nil
	}

	placement := Resolve(windows, coordinates)
	if placement.Problem != "" {
		return container.Failed(placement.Problem, placement.Detail), nil
	}

	window := placement.Windows[0]
	if err := a.Focus(window.ID); err != nil {
		// Refused rather than place-is-gone even though a window that died
		// between the listing and the focus is the likeliest cause. The word
		// is there so that a caller can act on it, and the action for
		// place-is-gone is to offer to prune the coordinates — and there is
		// nothing here to prune, because the window was never stored. What is
		// useful is aerospace's own sentence, which is on the end of this one.
		return container.Failed(container.Refused, err.Error()), nil
	}
	return container.Done(), nil
}

// Focused answers whether that window is the one in front.
//
// The three answers are not a formality here. "No" is only said when the window
// in front is demonstrably not one of the candidates; when several windows
// could be this session and one of them is focused, the honest answer is that
// nobody can tell which one you are looking at (R27).
func Focused(a Aerospace, raw json.RawMessage) (container.Verdict, error) {
	var coordinates Coordinates
	if err := json.Unmarshal(raw, &coordinates); err != nil {
		return cannotTell("these are not " + Name + "'s coordinates"), nil
	}

	windows, err := a.Windows()
	if err != nil {
		return cannotTell(err.Error()), nil
	}
	placement := Resolve(windows, coordinates)
	if len(placement.Windows) == 0 {
		return cannotTell(placement.Detail), nil
	}

	front, any, err := a.Focused()
	if err != nil {
		return cannotTell(err.Error()), nil
	}
	if !any {
		return cannotTell("aerospace says no window is focused"), nil
	}

	for _, candidate := range placement.Windows {
		if candidate.ID != front.ID {
			continue
		}
		if placement.Problem == container.Ambiguous {
			return cannotTell(placement.Detail + ", and this is one of them"), nil
		}
		return container.Verdict{Answer: container.Yes}, nil
	}
	return container.Verdict{Answer: container.No,
		Detail: fmt.Sprintf("the window in front is %s's, not this session's", front.Name)}, nil
}

func cannotTell(detail string) container.Verdict {
	return container.Verdict{Answer: container.CannotTell, Detail: detail}
}

// withTitle is every window whose title starts with the key.
//
// A prefix rather than a substring, and the key carries its own separator:
// zellij's `home | ` is specific enough to trust, where `home` would match any
// window whose title happens to begin that way.
func withTitle(windows []Window, prefix string) []Window {
	if prefix == "" {
		return nil
	}
	var found []Window
	for _, window := range windows {
		if strings.HasPrefix(window.Title, prefix) {
			found = append(found, window)
		}
	}
	return found
}

// ofNearestOwner is every window belonging to the nearest ancestor that owns
// any window at all.
//
// Nearest first, for the reason core walks that way when it looks for an agent:
// the innermost match is the one you are actually inside. It is also what makes
// the multiplexer case fall out rather than be special-cased — a zellij server
// owns no windows, so the walk simply runs off the end of the chain and the
// title key is what is left.
func ofNearestOwner(windows []Window, chain []Ancestor) []Window {
	for _, ancestor := range chain {
		var owned []Window
		for _, window := range windows {
			if window.App == ancestor.PID {
				owned = append(owned, window)
			}
		}
		if len(owned) > 0 {
			return owned
		}
	}
	return nil
}

func intersect(left, right []Window) []Window {
	in := map[int]bool{}
	for _, window := range right {
		in[window.ID] = true
	}
	var both []Window
	for _, window := range left {
		if in[window.ID] {
			both = append(both, window)
		}
	}
	return both
}

// placeholders is every {NAME} in a template, which is also the list of
// variables `capture-environment` reads. The template naming them is what keeps
// the two from drifting: there is no second list to keep in step.
func placeholders(template string) []string {
	var names []string
	for rest := template; ; {
		open := strings.IndexByte(rest, '{')
		if open < 0 {
			return names
		}
		close := strings.IndexByte(rest[open:], '}')
		if close < 0 {
			return names
		}
		if name := rest[open+1 : open+close]; name != "" {
			names = append(names, name)
		}
		rest = rest[open+close+1:]
	}
}

// substitute fills a template in, and says whether every placeholder had a
// value. An incomplete one is not a half-built key — it is no key, because a
// prefix with a hole in it matches windows that have nothing to do with this
// session.
func substitute(template string, values map[string]string) (string, bool) {
	if template == "" {
		return "", false
	}
	filled := template
	for _, name := range placeholders(template) {
		value, present := values[name]
		if !present || value == "" {
			return "", false
		}
		filled = strings.ReplaceAll(filled, "{"+name+"}", value)
	}
	return filled, true
}
