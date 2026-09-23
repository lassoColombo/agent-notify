package main

import (
	"fmt"
	"strings"
	"time"

	agentnotify "github.com/lassoColombo/agent-notify"
)

// This file is pure. It turns the sessions into one WhatTheBarShows and returns
// it as data; nothing here touches AppKit, and every question this display
// answers about what should be on the screen is answered somewhere in it.

// Bar is everything the renderer needs that is not the sessions.
type Bar struct {
	// Rows bounds how many sessions a state lists before it says "and n more".
	// A menu scrolls when it has to, so this is about a menu being readable
	// rather than about one fitting.
	Rows    int
	Glyphs  agentnotify.Palette
	Symbols agentnotify.Palette
	Colours agentnotify.Palette
	Now     time.Time
	// Announce is how long a state change adds the name of the session it
	// happened to, after the counts. Zero leaves the item passive: counts that
	// only count.
	Announce time.Duration
	// Resting is the colour of the mark when there is nothing running at all.
	// It is the one colour here that does not belong to a state.
	Resting string
}

// Invader is the name a paint asks for this program's own mark by, in the same
// field an SF Symbol name goes in.
//
// It is not an SF Symbol and never will be — it is eleven cells by nine, drawn
// by hand — and it is spelled like one so that a person who wants it on a menu
// row can put it there the same way they would put anything else there.
const Invader = "invader"

// Painted is every state that can appear, most urgent first, and it is core's
// own order rather than one written down here.
//
// `ended` is absent: an ended session is not displayed, period (D-26), and
// there is nothing to count.
func Painted() []agentnotify.Kernel {
	var painted []agentnotify.Kernel
	for _, kernel := range agentnotify.Kernels() {
		if kernel != agentnotify.Ended {
			painted = append(painted, kernel)
		}
	}
	return painted
}

// Render is the whole display.
//
// It returns the announcement as well as the paint, and for one reason only:
// the caller has a clock to re-arm, because the paint that ENDS an announcement
// has to land when it ends rather than at the next refresh (D-47). Nothing
// about the paint depends on what was painted before it — unlike M14's, which
// had to remember, because opening and closing a chip are edges. There are no
// edges here. A menu that is not open has no state to disturb, and the title is
// a pure function of the records and the clock.
func Render(bar Bar, sessions []agentnotify.Record) (WhatTheBarShows, agentnotify.Arrival) {
	byKernel := map[agentnotify.Kernel][]agentnotify.Record{}
	live := 0
	for _, record := range sessions {
		if !record.Kernel.Live() {
			continue
		}
		byKernel[record.Kernel] = append(byKernel[record.Kernel], record)
		live++
	}
	for _, here := range byKernel {
		agentnotify.ByUrgency(here)
	}

	now := agentnotify.JustArrived(sessions, bar.Now, bar.Announce)
	return WhatTheBarShows{
		Title:   bar.title(byKernel, now),
		Menu:    bar.menu(byKernel, now),
		Tooltip: bar.tooltip(byKernel),
	}, now
}

// anyOfThemIsAskingForYou reports whether a state is waiting on a person.
//
// It is core's own rule rather than a list of states written here (D-64), so an
// agent-integration that adds a state does not have to come back and edit this.
// It is the same predicate that decides what is worth a notification and what
// lights up a sketchybar chip — not by agreement but by construction, which is
// the point: three displays cannot disagree about what may take somebody's
// attention if there is only one of it.
func anyOfThemIsAskingForYou(here []agentnotify.Record) bool {
	for _, record := range here {
		if record.WantsYou() {
			return true
		}
	}
	return false
}

// title is what sits in the menu bar: one alien per state that has anybody in
// it, most urgent first, each in that state's own colour.
//
// It has been four designs and this is the first one that answers both of the
// questions a person actually has. A count per state answered "what is
// happening" and never "do I need to do something". One mark answered "do I
// need to do something" and threw the rest away. This is the marks without the
// numbers: the states that are live, in their own colours, and the ones waiting
// on you flicker while the ones getting on with it sit still.
//
// The shape never changes, which is the property the count designs could not
// keep: every mark is the same sprite, so the bar is read by colour and by
// movement rather than by being decoded. How many are in each state is in the
// menu and in the tooltip; a menu bar is not where a list goes.
func (b Bar) title(
	byKernel map[agentnotify.Kernel][]agentnotify.Record, now agentnotify.Arrival,
) []TitlePiece {
	var spans []TitlePiece
	for _, kernel := range Painted() {
		here := byKernel[kernel]
		if len(here) == 0 {
			continue
		}
		mark := TitlePiece{
			Symbol:  Invader,
			Colour:  b.Colours.For(here[0]),
			Flicker: anyOfThemIsAskingForYou(here),
		}
		if len(spans) > 0 {
			// The gap rides on the front of the text, where the drawing side knows
			// to put it before the mark rather than after it.
			mark.Text = gap
		}
		spans = append(spans, mark)
	}

	if len(spans) == 0 {
		// Nothing live at all. Still a mark, because a status item with nothing
		// in it is a few points of blank menu bar that a person can neither see
		// nor click, which reads as the display having died.
		return []TitlePiece{{Symbol: Invader, Colour: b.Resting}}
	}

	// An announcement is added AFTER the marks and never instead of them (D-50).
	//
	// **It does not open the menu**, and that is a decision rather than an
	// omission. `[button performClick:]` opens one, and an NSMenu opening
	// starts a modal tracking loop that takes the keyboard for as long as it is
	// up. A display that did that on its own would eat what you were typing,
	// several times an hour, for a notification you did not ask for.
	// sketchybar's chip is harmless because it is a drawing; a menu is not.
	if now.Announced() {
		spans = append(spans, TitlePiece{
			Text:   gap + now.Record.DisplayName(),
			Colour: b.Colours.For(now.Record),
		})
	}
	return spans
}

// tooltip is the counts, which is what the item stopped saying when it became
// one mark. It is also what the item is CALLED to the accessibility API, since
// a picture has no name of its own — so it is written as a sentence somebody
// would not mind hearing read out.
func (b Bar) tooltip(byKernel map[agentnotify.Kernel][]agentnotify.Record) string {
	var counted []string
	for _, kernel := range Painted() {
		if here := len(byKernel[kernel]); here > 0 {
			counted = append(counted, fmt.Sprintf("%d %s", here, name(kernel)))
		}
	}
	if len(counted) == 0 {
		return "agent-notify — nothing running"
	}
	return "agent-notify — " + strings.Join(counted, ", ")
}

// gap is what separates the mark from anything written after it.
const gap = "  "

// menu is the list behind the item: the states as sections, the sessions as
// rows under them, in the same order as the counts.
func (b Bar) menu(
	byKernel map[agentnotify.Kernel][]agentnotify.Record, now agentnotify.Arrival,
) []MenuRow {
	var menu []MenuRow
	for _, kernel := range Painted() {
		here := byKernel[kernel]
		if len(here) == 0 {
			continue
		}
		if len(menu) > 0 {
			menu = append(menu, MenuRow{Kind: Separator})
		}
		menu = append(menu, MenuRow{Kind: Section, Text: name(kernel)})
		for index, record := range here {
			if b.Rows > 0 && index == b.Rows {
				menu = append(menu, MenuRow{
					Kind: Note,
					Text: fmt.Sprintf("and %d more", len(here)-index),
				})
				break
			}
			menu = append(menu, b.row(record, now))
		}
	}
	if len(menu) == 0 {
		// A menu with nothing in it looks like a display that has broken rather
		// than like a machine with nothing running on it.
		menu = append(menu, MenuRow{Kind: Note, Text: "no agents"})
	}
	return menu
}

// row is a session: what it is called, what state it is in, and how long it has
// been in it.
//
// It does NOT carry what the agent said, and that is the change a day of using
// it forced. The message was drawn under the name as a truncated line and it
// was useless — a sentence fragment cut at sixty characters tells you nothing
// you did not already know from the state — and it was also behind a hover, as
// a system tooltip, which is a surface nobody can style and everybody waits a
// second for. What an agent SAID now goes where a thing somebody said belongs:
// in the notification, which arrives when it is said and is a proper piece of
// user interface. The menu is for finding a session and going to it.
func (b Bar) row(record agentnotify.Record, now agentnotify.Arrival) MenuRow {
	return MenuRow{
		Kind:   Row,
		Text:   record.DisplayName(),
		Symbol: b.Symbols.For(record),
		Glyph:  b.Glyphs.For(record),
		Colour: b.Colours.For(record),
		Age:    agentnotify.Ago(record.Elapsed(b.Now)),
		Key:    record.Key.String(),
		Lit:    now.Announced() && now.Record.Key == record.Key,
	}
}

// name is how a state is written when there is room for the words.
//
// A section header is the one place in this whole system with room for them,
// so `blocked-on-you` is spelled the way somebody would say it rather than the
// way it is keyed.
func name(kernel agentnotify.Kernel) string {
	return strings.ReplaceAll(string(kernel), "-", " ")
}
