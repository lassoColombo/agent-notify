package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestAWideLineWrapsAtTheViewsEdge(t *testing.T) {
	rows, first := Rows([]string{"short", "abcdefghij", "end"}, 4)
	// "short" is five cells, so it wraps too
	want := []string{"shor", "t", "abcd", "efgh", "ij", "end"}
	if !slices.Equal(rows, want) {
		t.Errorf("rows = %q, want %q", rows, want)
	}
	if !slices.Equal(first, []int{0, 2, 5}) {
		t.Errorf("first rows = %v, want [0 2 5]", first)
	}
}

// TestAWrappedLineKeepsItsColour: each row is drawn from a reset, so the second
// half of a red line has to say it is red.
func TestAWrappedLineKeepsItsColour(t *testing.T) {
	red := "\x1b[31m"
	rows, _ := Rows([]string{red + "abcdef\x1b[0m"}, 3)
	if len(rows) != 2 {
		t.Fatalf("rows = %q, want two", rows)
	}
	if !strings.HasPrefix(rows[1], red) {
		t.Errorf("second row %q does not start red", rows[1])
	}
	for _, row := range rows {
		if width := ansi.StringWidth(row); width > 3 {
			t.Errorf("row %q is %d cells, wider than the view", row, width)
		}
	}
}

func TestAResetEndsTheCarriedStyle(t *testing.T) {
	if got := carried("\x1b[1m\x1b[31mab\x1b[0mcd"); got != "" {
		t.Errorf("carried = %q after a reset, want nothing", got)
	}
	if got := carried("\x1b[0m\x1b[34mab"); got != "\x1b[34m" {
		t.Errorf("carried = %q, want the blue", got)
	}
}

func TestTheViewFollowsTheBottom(t *testing.T) {
	lines := []string{"1", "2", "3", "4", "5"}
	rows, _, _ := Frame(lines, Cursor{}, 10, 2, 0)
	if !slices.Equal(rows, []string{"4", "5"}) {
		t.Errorf("rows = %q, want the last two", rows)
	}
	rows, _, scroll := Frame(lines, Cursor{}, 10, 2, 99)
	if !slices.Equal(rows, []string{"1", "2"}) || scroll != 3 {
		t.Errorf("scrolled past the top: rows = %q, scroll = %d; want the first two and 3", rows, scroll)
	}
}

// TestTheCursorIsCarriedThroughTheWrap: the cursor is in cells of the agent's
// pane, and lands where that cell went.
func TestTheCursorIsCarriedThroughTheWrap(t *testing.T) {
	lines := []string{"abcdefghij", "> typing"}
	_, at, _ := Frame(lines, Cursor{X: 6, Y: 0, Visible: true}, 4, 10, 0)
	if at != (Cursor{X: 2, Y: 1, Visible: true}) {
		t.Errorf("cursor = %+v, want x 2 on the second row", at)
	}
	_, at, _ = Frame(lines, Cursor{X: 3, Y: 1, Visible: true}, 4, 10, 0)
	if at != (Cursor{X: 3, Y: 3, Visible: true}) {
		t.Errorf("cursor = %+v, want x 3 on the fourth row", at)
	}
}

func TestACursorAboveTheBottomIsKeptOnScreen(t *testing.T) {
	lines := []string{"$ ", "", "", "", ""}
	rows, at, _ := Frame(lines, Cursor{X: 2, Y: 0, Visible: true}, 10, 2, 0)
	if rows[0] != "$ " || !at.Visible || at.Y != 0 {
		t.Errorf("rows = %q, cursor = %+v; want the prompt on screen with the cursor on it", rows, at)
	}
}

func TestAHiddenCursorStaysHidden(t *testing.T) {
	if _, at, _ := Frame([]string{"x"}, Cursor{X: 0, Y: 0}, 10, 2, 0); at.Visible {
		t.Errorf("cursor = %+v, want hidden", at)
	}
}

func TestLinesDropsTheFinalNewline(t *testing.T) {
	if got := Lines("a\n\nb\n"); !slices.Equal(got, []string{"a", "", "b"}) {
		t.Errorf("Lines = %q", got)
	}
}
