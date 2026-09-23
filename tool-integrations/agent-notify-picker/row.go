package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	agentnotify "github.com/lassoColombo/agent-notify"
)

// The columns are capped, not fixed. A cap stops one enormous name pushing
// everything after it off the row; measuring the rest against what is actually
// in the list is what keeps a screenful of short names from leaving a gutter of
// air. It is the rule `agent-notify list` already uses (R24).
const (
	glyphColumn = 2
	ageColumn   = 5
	widestName  = 24
	// A path wider than this is elided from the left, a whole component at a
	// time. 44 fits every ordinary ~/projects/<area>/<repo> untouched.
	widestPath = 44
	// 26, because "blocked/permission-prompt" is 25 and the state that wants
	// you is exactly the one that must not be the one cut short.
	widestState = 26
	// The branch glyph, and the space either side of it. It is a MARK rather
	// than a word — the path and the branch used to run together as one
	// undifferentiated string with only whitespace between them.
	branchMark = ""
)

// Columns is how wide each one has to be for this particular list.
type Columns struct{ Name, State int }

// Measure sizes the columns against the sessions that are actually in the
// list, capped so that one outlier cannot push the rest out of line.
func Measure(sessions []agentnotify.Record) Columns {
	columns := Columns{}
	for _, record := range sessions {
		columns.Name = max(columns.Name, lipgloss.Width(oneLine(record.DisplayName())))
		columns.State = max(columns.State, lipgloss.Width(State(record)))
	}
	columns.Name = min(columns.Name, widestName)
	columns.State = min(columns.State, widestState)
	return columns
}

// Row is one line of the list, and it is also what the matcher matches against.
//
// It reads left to right as: what state this is, what precisely it is waiting
// on, how long it has been that way, who it is, and where it lives. The first
// three are a narrow status gutter that answers "which of these wants me", and
// they are adjacent because they are one question — the age used to sit on the
// far side of the name column, where it floated in the middle of the row with
// whitespace either side of it.
//
// THE STATE IS A WORD AS WELL AS A SHAPE, and it took a screenshot to settle
// that. A glyph-only column with a legend above the list was tried, and the
// legend read as a sixth row while three identical speech bubbles in a row of
// five still had to be looked up. A word costs eight columns and needs no key.
// The glyph stays because a list is scanned before it is read and a shape
// resolves before a word does; the two together are a badge, which is the form
// every other tool that has to say "what kind of thing is this" has settled on.
//
// Only that badge is coloured. The name is base05 whatever the session is
// doing, so that a name can be learned — a row painted end to end in the
// state's hue makes identity a moving target, and four such rows read as four
// kinds of object rather than four instances of one.
func Row(record agentnotify.Record, at time.Time, columns Columns) string {
	state := StateStyle(record.Rank)

	// The glyph's column is a constant rather than a measurement. These are
	// private-use codepoints: lipgloss counts them as one column and a terminal
	// may draw them as one or two depending on the font, so measuring would
	// make the row's width a property of somebody's font, while budgeting for
	// it costs at worst a space.
	return strings.Join([]string{
		state.Width(glyphColumn).Render(Glyph(record)),
		state.Width(columns.State).Render(fit(State(record), columns.State)),
		Subtle.Width(ageColumn).MaxWidth(ageColumn).Align(lipgloss.Right).
			Render(agentnotify.Ago(record.Elapsed(at))),
		Text.Width(columns.Name).Render(fit(record.DisplayName(), columns.Name)),
		Where(record),
	}, "  ")
}

// State is the row's word for a state: the kernel, shortened where core's own
// name is a sentence, and the detail after it when there is one.
//
// It is a LABEL rather than a second vocabulary. The canonical string — the one
// the user's palette table is keyed on and the bar and the tab titles use — is
// what the preview's pane label spells out in full, for the row you are on.
func State(record agentnotify.Record) string {
	word := short(record.Kernel)
	if record.Detail == "" {
		return word
	}
	return word + "/" + oneLine(record.Detail)
}

// Where is the path and the branch, in the two colours this machine writes a
// path and a branch in everywhere else, with a dim mark between them so that
// they are two things rather than one long string.
func Where(record agentnotify.Record) string {
	where := Directory(record.Cwd)
	if record.Branch != "" {
		where += " " + Rule.Render(branchMark) + " " + Branch.Render(record.Branch)
	}
	return where
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

// Directory is a path with its last component lit and everything above it
// dimmed.
//
// A working directory is four or five components long and only the last one
// tells two sessions apart. Drawn in one colour it is the longest and loudest
// thing on the row, competing with the name for a glance it does not deserve;
// dimmed down to its leaf it becomes what it is, which is context.
func Directory(path string) string {
	written := elide(Home(path), widestPath)
	if at := strings.LastIndex(written, "/"); at >= 0 && at < len(written)-1 {
		return Rule.Render(written[:at+1]) + Path.Render(written[at+1:])
	}
	return Path.Render(written)
}

// elide drops leading components from a path until it fits, which is the one
// way of shortening a path that keeps the part that identifies it.
//
// Cutting the tail would take the repository's name off; cutting mid-word would
// leave "…hell/modules/nu-http-client-generator". A whole component at a time
// leaves a path a person can still read aloud.
func elide(path string, width int) string {
	if lipgloss.Width(path) <= width {
		return path
	}
	components := strings.Split(path, "/")
	for first := 1; first < len(components)-1; first++ {
		shorter := "…/" + strings.Join(components[first:], "/")
		if lipgloss.Width(shorter) <= width {
			return shorter
		}
	}
	// A single component wider than the column: there is nothing above it to
	// drop, so it is cut like any other value that will not fit.
	return fit(path, width)
}

// Tokens is what a session has spent, in the few characters a line has room
// for: 640, 127k, 2.5M.
func Tokens(total uint64) string {
	switch {
	case total < 1000:
		return fmt.Sprintf("%d", total)
	case total < 1_000_000:
		return fmt.Sprintf("%dk", total/1000)
	}
	return fmt.Sprintf("%.1fM", float64(total)/1_000_000)
}

// Home writes a path the way a person writes it.
func Home(path string) string {
	directory, err := homeDirectory()
	if err != nil || directory == "" || path == "" {
		return path
	}
	if path == directory {
		return "~"
	}
	if strings.HasPrefix(path, directory+"/") {
		return "~" + path[len(directory):]
	}
	return path
}

// homeDirectory is a variable so that a test can decide what home is rather
// than render differently on whoever's machine it runs.
var homeDirectory = os.UserHomeDir
