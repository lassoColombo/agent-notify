// Package rows is the terminal's view of a set of sessions: one line each,
// most urgent first.
//
// Two commands print it — `list` reads the store once and `tail` is handed a
// new view whenever anything moves — and they must agree, because a person
// comparing the two is comparing them column by column. This is the same rule
// R24 applies to displays, held to inside core's own command line.
package rows

import (
	"fmt"
	"strings"
	"time"

	"github.com/lassoColombo/agent-notify/session"
)

// OneLinePerSession is the human view of what `--json` hands a display: most
// urgent first, one row each.
func OneLinePerSession(records []session.Record, now time.Time) {
	if len(records) == 0 {
		fmt.Println("no sessions")
		return
	}

	// Both columns are measured rather than guessed. A fixed width that the
	// longest value overflows does not truncate it — it pushes every column
	// after it out of line, which is worse than either.
	names, states := widestValueInTheColumn(records, 24, session.Record.DisplayName),
		widestValueInTheColumn(records, 34, session.Record.State)

	for _, record := range records {
		age := ""
		if !record.StateSince.IsZero() {
			age = session.Ago(now.Sub(record.StateSince))
		}
		row := fmt.Sprintf("%-3d %-*s  %-*s  %-7s",
			record.Rank,
			names, CutToWidthWithAnEllipsis(record.DisplayName(), names),
			states, CutToWidthWithAnEllipsis(record.State(), states),
			age)
		if message := OneLine(record.Message); message != "" {
			row += "  " + CutToWidthWithAnEllipsis(message, 60)
		}
		fmt.Println(strings.TrimRight(row, " "))
	}
}

// OneLine flattens for a row. Core kept the newlines because a preview pane
// wants them; flattening is rendering, and this is a renderer (R24).
func OneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// widestValueInTheColumn is capped, so that one enormous name cannot push the
// message off the screen.
func widestValueInTheColumn(
	records []session.Record, most int, of func(session.Record) string,
) int {
	widest := 0
	for _, record := range records {
		if size := len([]rune(of(record))); size > widest {
			widest = size
		}
	}
	return min(widest, most)
}

// CutToWidthWithAnEllipsis marks the cut, so a row shows a cut rather than a
// value that mysteriously stops.
func CutToWidthWithAnEllipsis(text string, width int) string {
	if len([]rune(text)) <= width {
		return text
	}
	return string([]rune(text)[:width-1]) + "…"
}
