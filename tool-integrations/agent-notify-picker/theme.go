package main

import (
	_ "embed"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/lassoColombo/agent-notify/session"
)

// Rosé Pine, by role rather than by hue — and each hue means ONE thing on this
// screen.
//
// These twelve are the DEFAULTS, and a hue is the only thing about the palette
// a person can change: `[integration.picker.settings.colors]` says what a slot
// IS, and this file says what a slot is FOR. That asymmetry is the whole of it
// — two things drawn in one slot move together when it changes, and the table
// in the config file is twelve lines because this file is disciplined about
// which things share a slot.
//
// The values are the scheme's own (~/.config/tinted-theming/scheme.yaml, the
// single source of truth on this machine). What changed here is the discipline
// rather than the palette: a hue that says "this session is broken" in the list
// must not also say "tokens" in the facts and "inline code" in the message,
// because a reader cannot hold three meanings for one colour in the two seconds
// this window is open. So:
//
//   - the four STATE hues (love, gold, foam, bright pine) appear on the state
//     glyph and the state's detail, and nowhere else;
//   - identity and prose are the neutral ramp, always, so a session's name does
//     not change colour when its state does;
//   - iris is paths and chrome, rose is branches and matched characters, and
//     neither is ever a state.
//
// The greys are measured rather than chosen. base02 is a BACKGROUND slot —
// 1.16:1 against base00 — so it draws nothing legible as a line; every rule,
// border and separator here is base03 (3.42:1), which is what scheme.yaml says
// base03 is for. base04 (5.2:1) is the readability floor and the lowest thing
// any actual word is allowed to be drawn in.
//
// The canvas is not among them. `bg:-1` is the one colour decision nobody can
// make here: this opens in a floating pane over somebody's work, and a picker
// that repaints the background reads as a different application rather than as
// a layer on top of one.
var (
	base02 = "#26233a" // overlay — the selected row, the chip behind inline code
	base03 = "#6e6a86" // muted — rules, borders, the marks between words
	base04 = "#8c88a6" // subtle — dim text, the readability floor
	base05 = "#e0def4" // text
	base08 = "#eb6f92" // love — wants you
	base09 = "#f6c177" // gold — what broke, and a string in a fenced block
	base0A = "#ebbcba" // rose — branches, matched characters
	base0B = "#31748f" // pine — a keyword, inside a fenced block and nowhere else
	base0C = "#9ccfd8" // foam — finished a turn
	base0D = "#c4a7e7" // iris — paths, prompt, labels
	base13 = "#fff0ee" // bright rose — the pointer, headings
	base14 = "#609fbb" // bright pine — working
)

// The styles, as lipgloss rather than as escape sequences written by hand.
//
// This file used to build its own SGR strings and pad with strings.Repeat over
// runes, which is two width models in one program: mine, and the one fzf uses
// to lay out what I hand it. lipgloss measures the way a terminal does — and
// it is already here, underneath glamour — so the columns are its problem now.
var (
	// Rule is every line and every mark that separates rather than speaks.
	Rule lipgloss.Style
	// Subtle is a word that is worth reading second: an age, a fact's name.
	Subtle lipgloss.Style
	// Text is identity and prose. A session's name is this colour whatever it
	// is doing, so that a name can be learned.
	Text lipgloss.Style
	// Path and Branch are where a session is, in the two colours this machine
	// already writes a path and a branch in — the shell prompt's own pair.
	Path   lipgloss.Style
	Branch lipgloss.Style
)

// restyle builds those five from the hues in force. It runs at startup, so that
// a program which never reads a configuration still draws in the defaults, and
// again from Repaint once the user's table has been read.
//
// It is the only place a style is built, which is the reason the block above
// declares rather than initialises: two lists of the same five assignments is
// two lists that will differ once, and the one that differs would be the one a
// person never sees.
//
// They are values rather than functions because they are used as values
// twenty-eight times across two files. A package variable assigned at startup
// is what that costs, and it is safe here for a reason that is true of this
// program and not of the daemons beside it: one job per process, and then it
// exits.
func init() { restyle() }

func restyle() {
	Rule = lipgloss.NewStyle().Foreground(lipgloss.Color(base03))
	Subtle = lipgloss.NewStyle().Foreground(lipgloss.Color(base04))
	Text = lipgloss.NewStyle().Foreground(lipgloss.Color(base05))
	Path = lipgloss.NewStyle().Foreground(lipgloss.Color(base0D))
	Branch = lipgloss.NewStyle().Foreground(lipgloss.Color(base0A))
}

// StateStyle is what a state is worth looking at in, and it is the bar's
// answer so that a session which is red there is red here (R24).
func StateStyle(rank int) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(StateColour(rank)))
}

// StateColour is the same answer as a hex string, for the places that need one.
//
// Working is BRIGHT pine rather than pine, and that is a legibility fix rather
// than a preference. Pine (#31748f) is 3.38:1 against this canvas and muted
// (#6e6a86) is 3.42:1 — the two commonest rows in a real list were the same
// weight, distinguished only by hue, at a contrast where hue barely resolves.
// Bright pine is 6.03:1 and keeps the family.
func StateColour(rank int) string {
	switch {
	case rank >= session.RankBlockedOnYou:
		return base08
	case rank >= session.RankBroke:
		return base09
	case rank >= session.RankFinishedATurn:
		return base0C
	case rank >= session.RankWorking:
		return base14
	case rank >= session.RankIdle:
		return base04
	}
	return base03
}

// Glyphs are the marks the bar and the zellij tab titles already wear — Font
// Awesome's BMP block, written as escapes rather than literals because they
// are private-use codepoints and a great deal of tooling drops them silently.
//
// A glyph is not decoration here. The list is scanned rather than read, and a
// shape resolves before a word does: the question is "which of these wants me",
// and a triangle answers it before a sentence can. It sits beside the word
// rather than instead of it — a glyph-only column was tried, with a legend
// above the list, and the legend read as a sixth row while three identical
// speech bubbles still had to be looked up.
var Glyphs = map[session.Kernel]string{
	session.BlockedOnYou:  "", // warning triangle
	session.Broke:         "", // a cross
	session.FinishedATurn: "", // speech bubble
	session.Working:       "", // circular arrows
	session.Idle:          "", // a dot
	session.Ended:         "", // a hollow dot
}

// Glyph is the mark for a state, falling back to a dot for a kernel this build
// has never met — which is what the rank on the wire is for (§A5.5, R21).
func Glyph(record session.Record) string {
	if mark, known := Glyphs[record.Kernel]; known {
		return mark
	}
	return ""
}

// short is the row's word for a kernel: core's own, except where core's own is
// a sentence. "blocked-on-you" and "finished-a-turn" are exactly right on the
// wire and too wide to repeat down a column beside a detail.
func short(kernel session.Kernel) string {
	switch kernel {
	case session.BlockedOnYou:
		return "blocked"
	case session.FinishedATurn:
		return "finished"
	}
	return string(kernel)
}

// fzfColours is fzf's chrome, in the hues in force.
//
// Two things here are fixes rather than taste. The borders, separators and
// scrollbars were base02, which is 1.16:1 against the canvas — the preview's
// top rule and the list's divider were not dim, they were invisible, and the
// window had no structure at all. And `fg+`/`hl+` do nothing in this program:
// every row arrives pre-coloured under --ansi, so there is nothing left for fzf
// to repaint, which is why the selected row needs --highlight-line and a solid
// pointer rail instead of a text colour.
//
// The background is left alone (`bg:-1`). This opens in a floating pane over
// somebody's work, and a picker that repaints the canvas reads as a different
// application rather than as a layer on top of one.
func fzfColours() string {
	return strings.Join([]string{
		"bg:-1",
		"fg:" + base04,
		"fg+:" + base05,
		"bg+:" + base02,
		"hl:" + base0A,
		"hl+:" + base13,
		"pointer:" + base13,
		"marker:" + base13,
		"prompt:" + base0D,
		"spinner:" + base0D,
		"ghost:" + base03,
		"info:" + base04,
		"query:" + base05,
		// The footer carries the legend, in its own colours; this is what the
		// spaces between them are.
		"footer:" + base04,
		"footer-border:" + base03,
		// The chrome that separates rather than speaks. base03, because base03 is
		// what scheme.yaml calls a border and base02 is a background.
		"border:" + base03,
		"separator:" + base03,
		"scrollbar:" + base03,
		"preview-border:" + base03,
		"preview-scrollbar:" + base03,
		"preview-label:" + base04,
		"label:" + base0D,
		// The gutter is fzf's rail on every row that is not the current one, and
		// there is nothing for it to say: the pointer marks the current row and
		// the glyph column marks every row. Drawn in the canvas's own colour so
		// that only the pointer shows.
		"gutter:-1",
		"preview-bg:-1",
		// What the window says about a configuration it could not use in full.
		// Gold, because a config file that does not say what it meant to say is
		// the same kind of news as a session that broke.
		"header:" + base09,
	}, ",")
}

// Markdown is what an agent said, in the same palette by the same roles — and
// by the same discipline: headings are bright rose rather than iris (iris is
// paths), inline code is text on a raised chip rather than gold (gold is a
// state), links are iris because a link is a path.
//
//go:embed rose-pine.json
var Markdown []byte

// stylesheet is that file with its slot names replaced by the hues in force.
//
// It is written in slots rather than in colours so that ONE table covers the
// whole window: a person who moves base0D moves the links in a message as well
// as the paths in a row. A window half in your palette and half in mine is
// worse than a window wholly in mine, and it is what an embedded file of
// literal hexes would have given.
//
// The chroma block below `code_block` is the one place a slot appears that
// nothing else on screen spends — base0B, a keyword — because syntax
// highlighting is the scheme's business rather than this program's.
func stylesheet() []byte {
	return []byte(strings.NewReplacer(
		"{base02}", base02,
		"{base03}", base03,
		"{base04}", base04,
		"{base05}", base05,
		"{base08}", base08,
		"{base09}", base09,
		"{base0A}", base0A,
		"{base0B}", base0B,
		"{base0C}", base0C,
		"{base0D}", base0D,
		"{base13}", base13,
		"{base14}", base14,
	).Replace(string(Markdown)))
}
