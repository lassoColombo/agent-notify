package main

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// This file is pure: it lays a captured screen out for the view and answers
// where the agent's cursor lands in it.
//
// The agent's pane and the view are different sizes, and nothing is resized:
// the pane belongs to the tab it lives in. So the mirror re-wraps instead.
// Agents draw text already wrapped to their own width, and a line wider than
// the view is broken again at the view's edge rather than cut there, so
// nothing the agent said is lost off the right; a full-width rule breaks in
// two, which is ugly and harmless. A pane taller than the view shows its
// bottom, where an agent's prompt and its newest output are.

// Cursor is a position in cells, from the top left, and whether to draw it.
type Cursor struct {
	X, Y    int
	Visible bool
}

// sgr is a Select Graphic Rendition sequence, the only escapes capture-pane -e
// writes.
var sgr = regexp.MustCompile("\x1b\\[[0-9;:]*m")

// carried is the style in force at the end of s: every SGR since the last one
// that resets everything. A wrapped line's second half starts with it, since
// each row of the view is drawn from a clean slate.
func carried(s string) string {
	var since []string
	for _, code := range sgr.FindAllString(s, -1) {
		if code == "\x1b[m" || code == "\x1b[0m" {
			since = since[:0]
			continue
		}
		since = append(since, code)
	}
	return strings.Join(since, "")
}

// Rows wraps each captured line to width. firstRow[i] is the row source line
// i starts on, which is how a cursor is carried over.
func Rows(lines []string, width int) (rows []string, firstRow []int) {
	width = max(width, 1)
	for _, line := range lines {
		firstRow = append(firstRow, len(rows))
		if ansi.StringWidth(line) <= width {
			rows = append(rows, line)
			continue
		}
		style := ""
		for _, part := range strings.Split(ansi.Hardwrap(line, width, true), "\n") {
			rows = append(rows, style+part)
			style += carried(part)
		}
	}
	return rows, firstRow
}

// Window is which rows the view shows, [top, top+height), and the scroll that
// really is: how many rows above the bottom the view has been scrolled, no
// further than the top. At zero it follows the bottom, unless that would leave
// the agent's cursor above the view.
func Window(total, height, scroll, cursorRow int) (int, int) {
	height = max(height, 1)
	scroll = max(0, min(scroll, total-height))
	bottom := total - scroll
	top := max(0, bottom-height)
	if scroll == 0 && cursorRow >= 0 && cursorRow < top {
		top = cursorRow
	}
	return top, scroll
}

// Frame is what the view draws: height rows of at most width cells, the
// agent's cursor moved to where it now is (or hidden if it is off the view),
// and the scroll clamped to the history there is.
func Frame(lines []string, cursor Cursor, width, height, scroll int) ([]string, Cursor, int) {
	rows, firstRow := Rows(lines, width)
	cursorRow := -1
	at := Cursor{}
	if cursor.Visible && cursor.Y >= 0 && cursor.Y < len(firstRow) {
		width = max(width, 1)
		cursorRow = firstRow[cursor.Y] + cursor.X/width
		at.X = cursor.X % width
	}
	top, scroll := Window(len(rows), height, scroll, cursorRow)
	shown := make([]string, height)
	for i := range height {
		if top+i < len(rows) {
			shown[i] = rows[top+i]
		}
	}
	if cursorRow >= top && cursorRow < top+height {
		at.Y = cursorRow - top
		at.Visible = true
	}
	return shown, at, scroll
}

// Lines is capture-pane's output as one string per screen row: it ends every
// row with a newline, so the last element of a plain split is not a row.
func Lines(captured string) []string {
	lines := strings.Split(captured, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
