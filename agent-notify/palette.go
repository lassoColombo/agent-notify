package agentnotify

import "sort"

// Palette is the user's table of what a state looks like: a glyph, a colour, a
// class name — whatever short string a display paints per state.
//
// It is one type rather than one per kind of value because the resolution is
// the interesting part and it is identical either way: most specific first, and
// a fallback by rank for a state this build has never met. A display asks for
// two of these — glyphs and colours — and gets the same rules for both.
//
// It is here rather than in each display for the reason R24 gives: a rule
// reimplemented per display is a rule that will differ per display, and a user
// who writes `"working/compacting" = "~"` once expects it to mean the same
// thing in a tab title, a bar item and a picker row.
//
// The table is the user's, keyed on the pair (§A5.3) — `blocked-on-you` or
// `blocked-on-you/permission-prompt`, most specific first — over defaults the
// display brought with it. Core never reads it: this runs inside the
// integration's own process, compiled in from the SDK, which is what keeps R7
// true of the session-watcher while still answering §A10.4.
type Palette struct {
	fallback  map[Kernel]string
	overrides map[string]string
}

// NewPalette layers a user's table over a display's own defaults.
//
// Both may be nil: a display with no defaults and a user with no opinion get
// no glyphs, which is a legitimate configuration and not an error.
func NewPalette(fallback map[Kernel]string, overrides map[string]string) Palette {
	table := Palette{
		fallback:  make(map[Kernel]string, len(fallback)),
		overrides: make(map[string]string, len(overrides)),
	}
	for kernel, mark := range fallback {
		table.fallback[kernel] = mark
	}
	for state, mark := range overrides {
		table.overrides[state] = mark
	}
	return table
}

// For is the mark this record wears.
//
// The order is most specific first: the exact (kernel, detail) pair, then the
// kernel, then — and this is the part that exists for a version of core this
// display has never met — the nearest known rank.
//
// That last step is the whole reason the rank is on the wire (§A5.5, R21). A
// display built today meeting `needs-a-decision` tomorrow paints it the way it
// paints the state nearest in urgency, which is "something roughly this
// important" rather than nothing at all. It is a possibility core enables, not
// a promise that it will look right.
func (g Palette) For(record Record) string {
	if mark, found := g.overrides[record.State()]; found {
		return mark
	}
	if mark, found := g.forKernel(record.Kernel); found {
		return mark
	}
	if nearest, found := g.nearest(record.Rank); found {
		if mark, found := g.forKernel(nearest); found {
			return mark
		}
	}
	return ""
}

func (g Palette) forKernel(kernel Kernel) (string, bool) {
	if mark, found := g.overrides[string(kernel)]; found {
		return mark, true
	}
	mark, found := g.fallback[kernel]
	return mark, found
}

// nearest is the known kernel closest in urgency to a rank this build has no
// name for: the most urgent one at or below it, because over-stating a state's
// urgency is the worse mistake — and failing that the least urgent one above,
// because something is still better than nothing.
func (g Palette) nearest(rank int) (Kernel, bool) {
	if rank == RankUnknown {
		return "", false
	}
	var below, above Kernel
	for _, kernel := range Kernels() {
		switch {
		case kernel.Rank() <= rank && (below == "" || kernel.Rank() > below.Rank()):
			below = kernel
		case kernel.Rank() > rank && (above == "" || kernel.Rank() < above.Rank()):
			above = kernel
		}
	}
	if below != "" {
		return below, true
	}
	if above != "" {
		return above, true
	}
	return "", false
}

// Marks is every distinct non-empty glyph this table can paint, longest first.
//
// It is for the display that writes into a namespace it does not own — a
// zellij tab name, a tmux window name, a terminal's title — and must therefore
// be able to recognise its own leftovers and take them off again before
// writing. Longest first so that stripping is greedy and a one-rune glyph that
// happens to prefix a two-rune one cannot strip half of it.
func (g Palette) Marks() []string {
	seen := map[string]bool{}
	for _, mark := range g.fallback {
		seen[mark] = true
	}
	for _, mark := range g.overrides {
		seen[mark] = true
	}
	delete(seen, "")

	marks := make([]string, 0, len(seen))
	for mark := range seen {
		marks = append(marks, mark)
	}
	sort.Slice(marks, func(a, b int) bool {
		if len(marks[a]) != len(marks[b]) {
			return len(marks[a]) > len(marks[b])
		}
		return marks[a] < marks[b]
	})
	return marks
}
