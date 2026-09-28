package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/lassoColombo/agent-notify/container"
)

// Coordinates is what `interpret-environment` returns. The tab is here even
// though it is re-read at focus time, because "pane 17 of tab 1 in session
// home" is something a person can check.
type Coordinates struct {
	Session string `json:"session"`
	Pane    int    `json:"pane"`
	Tab     int    `json:"tab"`
	TabName string `json:"tab_name,omitempty"`
}

func (c Coordinates) ours() bool { return c.Session != "" }

// Interpret asks zellij which tab holds the captured pane.
func Interpret(zellij Zellij, captured json.RawMessage) (any, error) {
	var given Captured
	if err := json.Unmarshal(captured, &given); err != nil {
		return nil, fmt.Errorf("what was captured is not this program's: %w", err)
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

// NotAttached: the session is running, the pane is there, and nobody is
// looking at any of it. Its next move is `zellij attach`, which is neither
// "prune the record" nor "start zellij", so it is its own word (§A11.3).
const NotAttached = container.Problem("not-attached")

// Focus brings the pane to the front: its tab, then the pane, then a read-back.
//
// Measured on 0.45.1: zellij REFUSES to focus a pane that is already focused
// (exit 2), so "already there" is asked first and focus is idempotent; the
// focus actions against a session nobody is attached to exit 0 and move the
// stored focus with nothing on any screen, so `list-clients` decides whether
// there is anybody to move; and the exit code is not evidence either way, so
// the state is read back afterwards. When nobody is attached and this was
// asked from a terminal in another zellij session, that terminal is the
// viewer the session is missing, and zellij will move it (SwitchSession).
func Focus(zellij Zellij, coordinates json.RawMessage) (container.Outcome, error) {
	var where Coordinates
	if err := json.Unmarshal(coordinates, &where); err != nil || !where.ours() {
		return container.Failed(container.NeverPlaced,
			"these are not this program's coordinates"), nil
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
	// A tab is `active` only while a client is on it, so this can be answered
	// before asking who is attached.
	if Verdict(panes, tabs, where.Pane).Answer == container.Yes {
		return container.Done(), nil
	}

	clients, err := zellij.Clients(where.Session)
	if err != nil {
		return container.Failed(container.NotRunning, err.Error()), nil
	}

	// Which session THIS program is running in: empty for every caller but a
	// terminal.
	here := os.Getenv(sessionVariable)

	switch {
	case len(clients) > 0:
		// Somebody is looking, so move their tab and pane; never a switch,
		// which would take the terminal somebody is typing in.
		if err := zellij.GoToTab(where.Session, pane.TabID); err != nil {
			return container.Failed(container.Refused, err.Error()), nil
		}
		// "In front of its own tab", not `pane.Focused`: a tab has two focused
		// panes, and the focused tiled one is Focused and behind the floating
		// layer at once. `focus-pane-id` on it is what lowers that layer.
		if !inFrontOfItsOwnTab(pane, tabs) {
			if err := zellij.FocusPane(where.Session, where.Pane); err != nil {
				return container.Failed(container.Refused, err.Error()), nil
			}
		}

	case here != "" && here != where.Session:
		if err := zellij.SwitchSession(where.Session, where.Pane); err != nil {
			return container.Failed(container.Refused, err.Error()), nil
		}
		// zellij answers before the terminal has arrived — measured at 25ms
		// out and 130ms in — so wait for a client, inside core's five seconds.
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
// Anything it cannot establish is `cannot-tell` rather than `no`: a wrong `no`
// silences a notification that should have fired (R27).
func Focused(zellij Zellij, coordinates json.RawMessage) (container.Verdict, error) {
	var where Coordinates
	if err := json.Unmarshal(coordinates, &where); err != nil || !where.ours() {
		return container.Verdict{Answer: container.CannotTell,
			Detail: "these are not this program's coordinates"}, nil
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

// Verdict is the pure half of Focused. `is_focused` is per tab, and a tab has
// two focused panes at once, tiled and floating; which is in front depends on
// whether the tab is showing its floating panes.
func Verdict(panes []Pane, tabs []Tab, pane int) container.Verdict {
	var found *Pane
	for i := range panes {
		if !panes[i].Plugin && panes[i].ID == pane {
			found = &panes[i]
			break
		}
	}
	if found == nil {
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
			return container.Verdict{Answer: container.No,
				Detail: fmt.Sprintf("pane %d is focused but covered", pane)}
		}
		return container.Verdict{Answer: container.Yes}
	}
	return container.Verdict{Answer: container.CannotTell,
		Detail: fmt.Sprintf("zellij did not report tab %d", found.TabID)}
}

// inFrontOfItsOwnTab is Verdict's rule without the tab-active clause, asked
// after go-to-tab has made that tab the active one.
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
