package main

import (
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"

	"github.com/lassoColombo/agent-notify/session"
)

// This file is pure: it decides what should be run and returns it as data.

// Command is one zellij invocation, as data rather than as an effect.
type Command struct {
	Session string
	Args    []string
	Why     string
}

// Plan is the whole display: what this zellij session's panes and tabs should
// say, minus what they already say. It reads the observed titles rather than
// remembering what it last wrote, so it is correct after a restart and after a
// pane moves, and never issues a rename that would change nothing.
func Plan(zellijSession string, records []session.Record, panes []Pane, glyphs session.Palette) []Command {
	held := map[int]Pane{}
	var tabs []int
	tabNames := map[int]string{}
	for _, pane := range panes {
		if pane.Plugin {
			continue
		}
		held[pane.ID] = pane
		if _, seen := tabNames[pane.TabID]; !seen {
			tabs = append(tabs, pane.TabID)
		}
		tabNames[pane.TabID] = pane.TabName
	}
	slices.Sort(tabs)

	// Sorted here rather than trusted from the caller: most urgent first is
	// what decides which of two sessions claiming one pane wins, and a pure
	// function that gives different answers for the same set in a different
	// order is not one.
	ordered := slices.Clone(records)
	session.ByUrgency(ordered)

	var commands []Command
	claimed := map[int]session.Record{}
	var claims []int

	for _, record := range ordered {
		place, found := PlaceOf(record)
		if !found || place.Session != zellijSession || !record.Kernel.Live() {
			continue
		}
		if _, taken := claimed[place.Pane]; taken {
			continue
		}
		if _, exists := held[place.Pane]; !exists {
			// The pane is gone and the session is not. That is not this
			// display's business to conclude anything from: whether a session
			// is still alive is decided by core, from the agent's process, and
			// a display that started guessing would eventually disagree with
			// it (R4).
			continue
		}
		claimed[place.Pane] = record
		claims = append(claims, place.Pane)
	}

	for _, id := range claims {
		record := claimed[id]
		want := join(glyphs.For(record), record.DisplayName())
		if held[id].Title == want {
			continue
		}
		commands = append(commands, Command{
			Session: zellijSession,
			Args:    []string{"action", "rename-pane", "--pane-id", Address(id), want},
			Why:     fmt.Sprintf("pane %d is %s: %q", id, record.State(), want),
		})
	}

	// Giving a pane back. A display that writes into a UI it does not own has
	// to be told when to stop, and the record is what remembers which pane it
	// was — which is the whole reason this asks for ended sessions it will
	// never draw (D-26 says do not show them; it does not say do not clean up
	// after them).
	released := map[int]bool{}
	for _, record := range ordered {
		place, found := PlaceOf(record)
		if !found || place.Session != zellijSession || record.Kernel.Live() {
			continue
		}
		if _, taken := claimed[place.Pane]; taken || released[place.Pane] {
			continue
		}
		pane, exists := held[place.Pane]
		if !exists || !ours(pane.Title, record, glyphs) {
			continue
		}
		released[place.Pane] = true
		commands = append(commands, Command{
			Session: zellijSession,
			Args:    []string{"action", "undo-rename-pane", "--pane-id", Address(place.Pane)},
			Why:     fmt.Sprintf("pane %d is no longer an agent's", place.Pane),
		})
	}

	// One glyph per tab, not one per agent: the most urgent thing in it. That
	// is the same rule as an LED over the whole machine and a dock badge over
	// everything, computed by the same function (§A5.5, §A12.1, R24).
	inTab := map[int][]session.Record{}
	for _, id := range claims {
		tab := held[id].TabID
		inTab[tab] = append(inTab[tab], claimed[id])
	}

	for _, tab := range tabs {
		// The tab's name is the user's, recovered rather than remembered:
		// whatever it says now, minus any glyph of ours on the front.
		base := strip(tabNames[tab], glyphs.Marks())
		want := base
		if here, found := session.MostUrgent(inTab[tab]); found {
			want = join(glyphs.For(here), base)
		}
		if want == tabNames[tab] {
			continue
		}
		if want == "" {
			// Never write an empty name: zellij has its own idea of what an
			// unnamed tab is called, and undo is how you ask for it back.
			commands = append(commands, Command{
				Session: zellijSession,
				Args:    []string{"action", "undo-rename-tab", "--tab-id", strconv.Itoa(tab)},
				Why:     fmt.Sprintf("tab %d has nothing of ours left on it", tab),
			})
			continue
		}
		commands = append(commands, Command{
			Session: zellijSession,
			Args:    []string{"action", "rename-tab-by-id", strconv.Itoa(tab), want},
			Why:     fmt.Sprintf("tab %d: %q", tab, want),
		})
	}

	return commands
}

// join puts a glyph in front of a name, and copes with either being absent: an
// idle session has no glyph by default and shows only its name, and a session
// with no name at all still shows its state.
func join(glyph, name string) string {
	switch {
	case glyph == "":
		return name
	case name == "":
		return glyph
	}
	return glyph + " " + name
}

// strip takes our own glyphs back off the front of a name, greedily and
// longest-first.
//
// It is a heuristic, and it is bounded: it only ever removes a string this
// configuration would itself have written. A user whose tab genuinely starts
// with a warning triangle loses it, which is the price of not needing a
// sidecar file to remember what we painted.
func strip(name string, marks []string) string {
	trimmed := strings.TrimSpace(name)
	for {
		cut := false
		for _, mark := range marks {
			if strings.HasPrefix(trimmed, mark) {
				trimmed = strings.TrimSpace(trimmed[len(mark):])
				cut = true
				break
			}
		}
		if !cut {
			return trimmed
		}
	}
}

// ours reports whether this pane's title is one we wrote, so that giving a pane
// back never clobbers a name somebody else chose.
//
// Two ways to recognise it: it starts with one of our glyphs, or — for the
// states whose glyph is deliberately empty — it is exactly the name we would
// have written.
func ours(title string, record session.Record, glyphs session.Palette) bool {
	trimmed := strings.TrimSpace(title)
	for _, mark := range glyphs.Marks() {
		if strings.HasPrefix(trimmed, mark) {
			return true
		}
	}
	return trimmed == record.DisplayName()
}

// Group sorts sessions into the zellij sessions they live in, so that each one
// is read and written once however many agents it holds.
func Group(records []session.Record) map[string][]session.Record {
	grouped := map[string][]session.Record{}
	for _, record := range records {
		if place, found := PlaceOf(record); found {
			grouped[place.Session] = append(grouped[place.Session], record)
		}
	}
	return grouped
}

// Display is the render function and what it needs.
type Display struct {
	Zellij Zellij
	Glyphs session.Palette
	Logger *slog.Logger
}

// Render is called with the current state, never with a transition (R22).
func (d *Display) Render(view session.View) error {
	for zellijSession, records := range Group(view.Sessions) {
		panes, err := d.Zellij.Panes(zellijSession)
		if err != nil {
			// A failed read is what guarantees no rename is attempted against
			// a session that is not there (R13).
			d.Logger.Debug("skipped a zellij session", "session", zellijSession, "why", err.Error())
			continue
		}
		for _, command := range Plan(zellijSession, records, panes, d.Glyphs) {
			if err := d.Zellij.Do(command); err != nil {
				d.Logger.Warn("zellij refused", "session", zellijSession, "why", command.Why,
					"problem", err.Error())
				continue
			}
			d.Logger.Debug("painted", "session", zellijSession, "what", command.Why)
		}
	}
	return nil
}
