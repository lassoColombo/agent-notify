package main

import (
	"fmt"
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
	base03 = lipgloss.Color("#6e6a86") // muted — counts, the keys
	base04 = lipgloss.Color("#8c88a6") // subtle — where, age
	base05 = lipgloss.Color("#e0def4") // text — names
	base08 = lipgloss.Color("#eb6f92") // love — needs you
	base09 = lipgloss.Color("#f6c177") // gold — broke
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

// Draw is the list: per state a header with its count, then two lines per
// agent, its name and age and then where it lives, scrolled to keep the
// cursor on screen, with the keys on the last line.
func Draw(sections []Section, selected string, glyphs session.Palette, width, height int, now time.Time) string {
	width, height = max(width, narrowestList), max(height, 3)
	if len(sections) == 0 {
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
		lines = append(lines, hueOf(section.Kernel).Bold(true).Render(" "+title)+
			lipgloss.NewStyle().Foreground(base03).Render(fmt.Sprintf(" %d", len(section.Agents))))
		for _, agent := range section.Agents {
			if agent.Pane == selected {
				cursor = len(lines)
			}
			lines = append(lines, row(agent, agent.Pane == selected, width, now)...)
		}
	}

	// Two lines are kept for the keys and the gap above them.
	room := height - 2
	top := max(0, min(cursor+2-room, len(lines)-room))
	shown := make([]string, room)
	copy(shown, lines[top:min(len(lines), top+room)])
	return strings.Join(append(shown, "", lipgloss.NewStyle().Foreground(base03).Render(fit(keys, width))), "\n")
}

// row is one agent: a bar if it is selected, its name, its age at the far
// edge, and under it the session:window and directory it lives in.
func row(agent Agent, selected bool, width int, now time.Time) []string {
	bar, base := "  ", lipgloss.NewStyle()
	if selected {
		bar = lipgloss.NewStyle().Foreground(base13).Background(base02).Render("▌ ")
		base = base.Background(base02)
	}
	inside := width - 2
	age := session.Ago(agent.Record.Elapsed(now))
	room := max(inside-lipgloss.Width(age)-1, 1)

	name := base.Foreground(base05).Bold(selected).Width(room).Render(fit(agent.Record.DisplayName(), room))
	when := base.Foreground(base04).Render(" " + age)

	where := agent.Where
	if leaf := leafOf(agent.Record.Cwd); leaf != "" {
		where += "  " + leaf
	}
	under := base.Foreground(base04).Width(inside).Render(fit(where, inside))
	return []string{bar + name + when, bar + under}
}

func leafOf(path string) string {
	path = strings.TrimRight(path, "/")
	if at := strings.LastIndex(path, "/"); at >= 0 {
		return path[at+1:]
	}
	return path
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
