package main

import (
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/lassoColombo/agent-notify/session"
)

const (
	// glyphColumn is the glyph and the space after it. A constant rather than a
	// measurement: these are private-use codepoints, which lipgloss counts as
	// one column and a terminal may draw as one or two depending on the font, so
	// measuring would make the row's width a property of somebody's font, while
	// budgeting for it costs at worst a space.
	glyphColumn = 2
	// ageColumn is the widest wait session.Ago writes ("59m", "23h", "12d").
	ageColumn = 4
)

// Row is one line of the list, and it is also what the matcher matches against:
// the state's glyph, the name, and how long it has been that way, in `width`
// columns so that every wait lines up at the list's right edge.
//
// It is the cockpit's row (D-132, D-133). The state is the glyph in its hue
// and not a word: the list is scanned for which of these wants you, and a shape
// in a colour answers that before a word can. The directory, the branch and the
// detail are gone; a name is a directory's more often than not, and the name is
// what anybody types at this list.
//
// Only the glyph is coloured by state. The name is base05 whatever the session
// is doing, so that a name can be learned. The wait is dim, except where it is
// the reason to look: bold for an agent waiting on you, gold for a turn that has
// gone on too long.
func Row(record session.Record, at time.Time, width int) string {
	elapsed := record.Elapsed(at)
	wait := Subtle
	switch {
	case record.Kernel == session.BlockedOnYou:
		wait = Text.Bold(true)
	case record.Kernel == session.Working && elapsed >= session.StuckAfter:
		wait = Long
	}
	name := max(width-glyphColumn-1-ageColumn, 1)
	return StateStyle(record.Rank).Width(glyphColumn).Render(Glyph(record)) +
		Text.Width(name).Render(fit(record.DisplayName(), name)) + " " +
		wait.Width(ageColumn).Align(lipgloss.Right).Render(session.Ago(elapsed))
}

// fit is a value cut to its column, with an ellipsis to say it was cut.
//
// It exists because lipgloss WRAPS rather than truncates: a name wider than its
// Width comes back as two lines, and a row that is two lines high is a row that
// has torn the list in half under every row after it. A real store found this
// within a second — `dmilog3-rollout-dashboard` is 25 characters — and every
// session below it was drawn a line lower than the cursor thought it was.
func fit(text string, width int) string {
	text = oneLine(text)
	if width <= 0 || lipgloss.Width(text) <= width {
		return text
	}
	var kept []rune
	for _, glyph := range text {
		if lipgloss.Width(string(append(kept, glyph)))+1 > width {
			break
		}
		kept = append(kept, glyph)
	}
	return string(kept) + "…"
}

// oneLine flattens a value that arrived with whitespace in it. A name is
// somebody else's string and may contain anything, a newline included.
func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
