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
//
// Where zellij's display renames panes and tabs, this one writes two user
// options and leaves every name alone. tmux gives each pane a title that the
// program in it may rewrite at any moment — an agent setting its terminal
// title does exactly that — and renaming a window turns its
// `automatic-rename` off for good. An option of our own has neither problem:
// nobody else writes it, so there is nothing to strip, nothing to recover and
// nothing to give back but the option itself. The cost is that it shows only
// where a format names it, which is the tmux configuration's half (README).

// Command is one tmux invocation, as data rather than as an effect.
type Command struct {
	Socket string
	Args   []string
	Why    string
}

// Plan is the whole display: what this tmux server's panes and windows should
// say, minus what they already say. It reads the observed options rather than
// remembering what it last wrote, so it is correct after a restart and after a
// pane moves, and never issues a write that would change nothing.
func Plan(socket string, records []session.Record, rows []Row, glyphs session.Palette) []Command {
	held := map[string]Row{}
	var panes []string
	windows := map[string]Row{}
	var windowIDs []string
	for _, row := range rows {
		if _, seen := held[row.Pane]; !seen {
			held[row.Pane] = row
			panes = append(panes, row.Pane)
		}
		if _, seen := windows[row.WindowID]; !seen {
			windows[row.WindowID] = row
			windowIDs = append(windowIDs, row.WindowID)
		}
	}
	slices.SortFunc(panes, byNumber)
	slices.SortFunc(windowIDs, byNumber)

	// Sorted here rather than trusted from the caller: most urgent first is
	// what decides which of two sessions claiming one pane wins, and a pure
	// function that gives different answers for the same set in a different
	// order is not one.
	ordered := slices.Clone(records)
	session.ByUrgency(ordered)

	var commands []Command
	claimed := map[string]session.Record{}
	var claims []string

	for _, record := range ordered {
		place, found := PlaceOf(record)
		if !found || place.Socket != socket || !record.Kernel.Live() {
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

	for _, pane := range claims {
		record := claimed[pane]
		want := join(glyphs.For(record), record.DisplayName())
		if held[pane].PaneMark == want {
			continue
		}
		commands = append(commands, Command{
			Socket: socket,
			Args:   []string{"set-option", "-p", "-t", pane, paneOption, want},
			Why:    fmt.Sprintf("pane %s is %s: %q", pane, record.State(), want),
		})
	}

	// Giving a pane back is unsetting the option. Nobody else writes it, so a
	// pane that carries it and holds no live agent is one of ours left over —
	// an agent that ended, or a record that is gone — whichever record, if
	// any, remembers it.
	for _, pane := range panes {
		if _, live := claimed[pane]; live || held[pane].PaneMark == "" {
			continue
		}
		commands = append(commands, Command{
			Socket: socket,
			Args:   []string{"set-option", "-p", "-u", "-t", pane, paneOption},
			Why:    fmt.Sprintf("pane %s is no longer an agent's", pane),
		})
	}

	// One glyph per window, not one per agent: the most urgent thing in it.
	// That is the same rule as an LED over the whole machine and a dock badge
	// over everything, computed by the same function (§A5.5, §A12.1, R24).
	inWindow := map[string][]session.Record{}
	for _, pane := range claims {
		window := held[pane].WindowID
		inWindow[window] = append(inWindow[window], claimed[pane])
	}

	for _, window := range windowIDs {
		want := ""
		if here, found := session.MostUrgent(inWindow[window]); found {
			want = glyphs.For(here)
		}
		if want == windows[window].WindowMark {
			continue
		}
		if want == "" {
			commands = append(commands, Command{
				Socket: socket,
				Args:   []string{"set-option", "-w", "-u", "-t", window, windowOption},
				Why:    fmt.Sprintf("window %s has nothing of ours left on it", window),
			})
			continue
		}
		commands = append(commands, Command{
			Socket: socket,
			Args:   []string{"set-option", "-w", "-t", window, windowOption, want},
			Why:    fmt.Sprintf("window %s: %q", window, want),
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

// byNumber orders tmux ids — `%12`, `@3` — by their number, so that `%10`
// comes after `%9`.
func byNumber(a, b string) int {
	return number(a) - number(b)
}

func number(id string) int {
	n, _ := strconv.Atoi(strings.TrimLeft(id, "%@$"))
	return n
}

// Group sorts sessions into the tmux servers they live in, so that each one is
// read and written once however many agents it holds.
func Group(records []session.Record) map[string][]session.Record {
	grouped := map[string][]session.Record{}
	for _, record := range records {
		if place, found := PlaceOf(record); found {
			grouped[place.Socket] = append(grouped[place.Socket], record)
		}
	}
	return grouped
}

// Display is the render function and what it needs.
type Display struct {
	Tmux   Tmux
	Glyphs session.Palette
	Logger *slog.Logger
}

// Render is called with the current state, never with a transition (R22).
func (d *Display) Render(view session.View) error {
	for socket, records := range Group(view.Sessions) {
		rows, err := d.Tmux.Panes(socket)
		if err != nil {
			// A failed read is what guarantees no write is attempted against
			// a server that is not there (R13).
			d.Logger.Debug("skipped a tmux server", "socket", socket, "why", err.Error())
			continue
		}
		for _, command := range Plan(socket, records, rows, d.Glyphs) {
			if err := d.Tmux.Do(command); err != nil {
				d.Logger.Warn("tmux refused", "socket", socket, "why", command.Why,
					"problem", err.Error())
				continue
			}
			d.Logger.Debug("painted", "socket", socket, "what", command.Why)
		}
	}
	return nil
}
