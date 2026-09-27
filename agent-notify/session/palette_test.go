package session_test

import (
	"slices"
	"testing"

	"github.com/lassoColombo/agent-notify/session"
)

func marked(kernel session.Kernel, detail string) session.Record {
	return session.Record{Kernel: kernel, Rank: kernel.Rank(), Detail: detail}
}

var painted = map[session.Kernel]string{
	session.BlockedOnYou:  "!",
	session.Broke:         "x",
	session.FinishedATurn: ">",
	session.Working:       "*",
	session.Idle:          "",
	session.Ended:         "",
}

func TestGlyphsResolveMostSpecificFirst(t *testing.T) {
	glyphs := session.NewPalette(painted, map[string]string{
		"blocked-on-you/permission-prompt": "?",
		"working":                          "~",
	})

	for _, want := range []struct {
		record session.Record
		glyph  string
		why    string
	}{
		{marked(session.BlockedOnYou, "permission-prompt"), "?", "the pair wins over the kernel"},
		{marked(session.BlockedOnYou, "waiting-on-input"), "!", "an unnamed detail falls back to its kernel"},
		{marked(session.Working, "compacting"), "~", "a user's kernel override beats the display's default"},
		{marked(session.FinishedATurn, ""), ">", "the display's own default when the user said nothing"},
		{marked(session.Idle, ""), "", "an empty glyph is a choice, not a miss"},
	} {
		if got := glyphs.For(want.record); got != want.glyph {
			t.Errorf("%s: For(%s) = %q, want %q", want.why, want.record.State(), got, want.glyph)
		}
	}
}

// TestAnUnknownKernelIsPaintedFromItsRank is the whole reason the rank is on
// the wire (§A5.5, R21): a display built today meeting a state core ships
// tomorrow paints it as something rather than as nothing.
func TestAnUnknownKernelIsPaintedFromItsRank(t *testing.T) {
	glyphs := session.NewPalette(painted, map[string]string{"working": "~"})

	tomorrow := session.Record{Kernel: "needs-a-decision", Rank: 45}
	if got := glyphs.For(tomorrow); got != "x" {
		t.Errorf("a rank-45 state unknown to this build painted %q, want %q — the glyph of "+
			"the nearest known state at or below it, which is broke at 40", got, "x")
	}

	// Below everything known: the least urgent known state, because something
	// is still better than nothing.
	if got := glyphs.For(session.Record{Kernel: "archived", Rank: -5}); got != "" {
		t.Errorf("a state below every known rank painted %q, want the ended glyph", got)
	}

	// No rank at all is the one case with nothing to reason from.
	if got := glyphs.For(session.Record{Kernel: "mystery", Rank: session.RankUnknown}); got != "" {
		t.Errorf("a state with no rank painted %q, want nothing", got)
	}
}

// TestMarksAreLongestFirst is what a display strips with, and greedy stripping
// only works if the longer of two glyphs is tried first.
func TestMarksAreLongestFirst(t *testing.T) {
	glyphs := session.NewPalette(
		map[session.Kernel]string{session.Working: "*", session.Broke: "", session.Idle: ""},
		map[string]string{"blocked-on-you": "**!"},
	)
	marks := glyphs.Marks()
	if !slices.Equal(marks, []string{"**!", "*"}) {
		t.Errorf("Marks() = %q, want the longer glyph first and no empties", marks)
	}
}

// TestGlyphsAreCopied: a table is a value, and a caller that keeps mutating the
// map it passed in must not be able to repaint a display from underneath it.
func TestGlyphsAreCopied(t *testing.T) {
	fallback := map[session.Kernel]string{session.Working: "*"}
	overrides := map[string]string{"idle": "."}
	glyphs := session.NewPalette(fallback, overrides)

	fallback[session.Working] = "CHANGED"
	overrides["idle"] = "CHANGED"

	if got := glyphs.For(marked(session.Working, "")); got != "*" {
		t.Errorf("mutating the caller's map changed the table: got %q", got)
	}
	if got := glyphs.For(marked(session.Idle, "")); got != "." {
		t.Errorf("mutating the caller's overrides changed the table: got %q", got)
	}
}
