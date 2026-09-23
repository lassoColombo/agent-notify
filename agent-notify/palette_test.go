package agentnotify_test

import (
	"slices"
	"testing"

	agentnotify "github.com/lassoColombo/agent-notify"
)

func marked(kernel agentnotify.Kernel, detail string) agentnotify.Record {
	return agentnotify.Record{Kernel: kernel, Rank: kernel.Rank(), Detail: detail}
}

var painted = map[agentnotify.Kernel]string{
	agentnotify.BlockedOnYou:  "!",
	agentnotify.Broke:         "x",
	agentnotify.FinishedATurn: ">",
	agentnotify.Working:       "*",
	agentnotify.Idle:          "",
	agentnotify.Ended:         "",
}

func TestGlyphsResolveMostSpecificFirst(t *testing.T) {
	glyphs := agentnotify.NewPalette(painted, map[string]string{
		"blocked-on-you/permission-prompt": "?",
		"working":                          "~",
	})

	for _, want := range []struct {
		record agentnotify.Record
		glyph  string
		why    string
	}{
		{marked(agentnotify.BlockedOnYou, "permission-prompt"), "?", "the pair wins over the kernel"},
		{marked(agentnotify.BlockedOnYou, "waiting-on-input"), "!", "an unnamed detail falls back to its kernel"},
		{marked(agentnotify.Working, "compacting"), "~", "a user's kernel override beats the display's default"},
		{marked(agentnotify.FinishedATurn, ""), ">", "the display's own default when the user said nothing"},
		{marked(agentnotify.Idle, ""), "", "an empty glyph is a choice, not a miss"},
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
	glyphs := agentnotify.NewPalette(painted, map[string]string{"working": "~"})

	tomorrow := agentnotify.Record{Kernel: "needs-a-decision", Rank: 45}
	if got := glyphs.For(tomorrow); got != "x" {
		t.Errorf("a rank-45 state unknown to this build painted %q, want %q — the glyph of "+
			"the nearest known state at or below it, which is broke at 40", got, "x")
	}

	// Below everything known: the least urgent known state, because something
	// is still better than nothing.
	if got := glyphs.For(agentnotify.Record{Kernel: "archived", Rank: -5}); got != "" {
		t.Errorf("a state below every known rank painted %q, want the ended glyph", got)
	}

	// No rank at all is the one case with nothing to reason from.
	if got := glyphs.For(agentnotify.Record{Kernel: "mystery", Rank: agentnotify.RankUnknown}); got != "" {
		t.Errorf("a state with no rank painted %q, want nothing", got)
	}
}

// TestMarksAreLongestFirst is what a display strips with, and greedy stripping
// only works if the longer of two glyphs is tried first.
func TestMarksAreLongestFirst(t *testing.T) {
	glyphs := agentnotify.NewPalette(
		map[agentnotify.Kernel]string{agentnotify.Working: "*", agentnotify.Broke: "", agentnotify.Idle: ""},
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
	fallback := map[agentnotify.Kernel]string{agentnotify.Working: "*"}
	overrides := map[string]string{"idle": "."}
	glyphs := agentnotify.NewPalette(fallback, overrides)

	fallback[agentnotify.Working] = "CHANGED"
	overrides["idle"] = "CHANGED"

	if got := glyphs.For(marked(agentnotify.Working, "")); got != "*" {
		t.Errorf("mutating the caller's map changed the table: got %q", got)
	}
	if got := glyphs.For(marked(agentnotify.Idle, "")); got != "." {
		t.Errorf("mutating the caller's overrides changed the table: got %q", got)
	}
}
