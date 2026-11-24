package main

import (
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/lassoColombo/agent-notify/session"
)

// This file is pure: the list as text.

// Rosé Pine, as the picker draws it: the four state hues mark a state and
// nothing else, names are the neutral ramp so they read the same whatever the
// agent is doing, and the selected row sits on overlay with a bright rose bar.
var (
	base02 = lipgloss.Color("#26233a") // overlay — the selected row
	base03 = lipgloss.Color("#6e6a86") // muted — the background, the keys
	base04 = lipgloss.Color("#8c88a6") // subtle — where, age
	base05 = lipgloss.Color("#e0def4") // text — names
	base08 = lipgloss.Color("#eb6f92") // love — needs you
	base09 = lipgloss.Color("#f6c177") // gold — broke, and a turn gone on too long
	base0C = lipgloss.Color("#9ccfd8") // foam — finished a turn
	base13 = lipgloss.Color("#fff0ee") // bright rose — the pointer
	base14 = lipgloss.Color("#609fbb") // bright pine — working
)

var hues = map[session.Kernel]lipgloss.Style{
	session.BlockedOnYou:  lipgloss.NewStyle().Foreground(base08),
	session.Broke:         lipgloss.NewStyle().Foreground(base09),
	session.FinishedATurn: lipgloss.NewStyle().Foreground(base0C),
	session.Working:       lipgloss.NewStyle().Foreground(base14),
	session.Idle:          lipgloss.NewStyle().Foreground(base04),
}

// hueOf is a state's colour. One this build has never heard of is drawn
// subtle rather than not at all (R8).
func hueOf(kernel session.Kernel) lipgloss.Style {
	if hue, known := hues[kernel]; known {
		return hue
	}
	return lipgloss.NewStyle().Foreground(base04)
}

// keys is the list's last line: what it takes besides moving.
const keys = " ⏎ type   o go to   q close"

// Draw is the list, and holds only what decides where you look next (D-132):
// per state a header in its colour, then one line per agent, its name and how
// long it has been so; below them the sessions in the background, each
// wearing its own state's glyph since no header says it (D-128). Where an
// agent lives, its directory and what it said are not here: `o` goes where it
// lives, and the stage is the agent's own screen. Scrolled to keep the cursor
// on screen, with the keys on the last line.
func Draw(sections []Section, background []session.Record, selected string, glyphs session.Palette, width, height int, now time.Time) string {
	width, height = max(width, narrowestList), max(height, 3)
	if len(sections) == 0 && len(background) == 0 {
		return lipgloss.NewStyle().Foreground(base04).Render(" no agents in this tmux server")
	}

	var lines []string
	cursor := 0
	for i, section := range sections {
		if i > 0 {
			lines = append(lines, "")
		}
		// The header wears the state's own glyph, which is the kernel's: a
		// detail's would say one agent's state for the whole group.
		title := section.Title
		if glyph := glyphs.For(session.Record{Kernel: section.Kernel, Rank: section.Agents[0].Record.Rank}); glyph != "" {
			title = glyph + " " + title
		}
		lines = append(lines, hueOf(section.Kernel).Bold(true).Render(" "+fit(title, width-1)))
		for _, agent := range section.Agents {
			if agent.Pane == selected {
				cursor = len(lines)
			}
			lines = append(lines, row(agent.Record, agent.Pane == selected, "", width, now))
		}
	}
	if len(background) > 0 {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, lipgloss.NewStyle().Foreground(base03).Bold(true).Render(" in background"))
		for _, record := range background {
			mark := "  "
			if glyph := glyphs.For(session.Record{Kernel: record.Kernel, Rank: record.Rank}); glyph != "" {
				mark = hueOf(record.Kernel).Render(glyph) + " "
			}
			lines = append(lines, row(record, false, mark, width, now))
		}
	}

	// Two lines are kept for the keys and the gap above them.
	room := height - 2
	top := max(0, min(cursor+2-room, len(lines)-room))
	shown := make([]string, room)
	copy(shown, lines[top:min(len(lines), top+room)])
	return strings.Join(append(shown, "", lipgloss.NewStyle().Foreground(base03).Render(fit(keys, width))), "\n")
}

// row is one session on one line: a bar if it is selected, its name after
// `mark`, and how long it has been in its state at the far edge. Only the
// wait carries a colour, and only when it says something: bold for an agent
// waiting on you, gold for a turn that has run too long. A row with a mark
// is a background one, and its name is dimmed: there is no pane to borrow.
func row(record session.Record, selected bool, mark string, width int, now time.Time) string {
	bar, base := "  ", lipgloss.NewStyle()
	if selected {
		bar = lipgloss.NewStyle().Foreground(base13).Background(base02).Render("▌ ")
		base = base.Background(base02)
	}
	elapsed := record.Elapsed(now)
	age := session.Ago(elapsed)
	wait := base.Foreground(base04)
	switch {
	case record.Kernel == session.BlockedOnYou:
		wait = base.Foreground(base05).Bold(true)
	case record.Kernel == session.Working && elapsed >= session.StuckAfter:
		wait = base.Foreground(base09)
	}
	ink := base05
	if mark != "" {
		ink = base04
	}
	room := max(width-2-lipgloss.Width(mark)-lipgloss.Width(age)-1, 1)
	name := base.Foreground(ink).Bold(selected).Width(room).Render(fit(record.DisplayName(), room))
	return bar + mark + name + wait.Render(" "+age)
}

// fit makes text one clean line of at most `width` cells, cut with an
// ellipsis. A name is an agent's to choose, so it is cleaned before it is
// drawn (§A15); and lipgloss wraps rather than truncates, and a row that
// wraps tears the list in two.
func fit(text string, width int) string {
	text = session.CleanLine(text)
	if width <= 0 || lipgloss.Width(text) <= width {
		return text
	}
	var kept []rune
	for _, r := range text {
		if lipgloss.Width(string(append(kept, r)))+1 > width {
			break
		}
		kept = append(kept, r)
	}
	return string(kept) + "…"
}
