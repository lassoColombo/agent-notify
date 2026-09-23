package main

// WhatTheBarShows is what the menu bar should look like, as data.
//
// It is the whole boundary between Go and AppKit. This document is marshalled
// to JSON, handed over as one string, and applied by an Objective-C file that
// makes no decisions at all — so everything that decides anything is on this
// side of the line and can be tested on a machine with no screen, which is the
// same bargain sketchybar's `Render` struck with its argument list.
//
// One string rather than a struct mapped field by field through cgo, because a
// tree of items with a tree of colours under it is exactly the kind of thing
// that turns a boundary into a second program. NSJSONSerialization is in the
// system and costs nothing at the rate a menu bar changes.
type WhatTheBarShows struct {
	// Title is the status item itself, in pieces, so that a piece can wear its
	// own colour.
	Title []TitlePiece `json:"title"`
	// Menu is what drops down.
	//
	// It is NOT built when it is painted. A paint stores it and the menu is
	// constructed when somebody opens it, which is both less work — nobody is
	// looking at it most of the time — and the only way to avoid rebuilding a
	// menu underneath a person who has it open.
	Menu []MenuRow `json:"menu"`
	// Tooltip is what hovering the status item says — and, because the item is
	// a picture with no text in it, what it is called to the accessibility API
	// as well. It is therefore the counts: the bar says whether anything wants
	// you, and this says how many of what.
	Tooltip string `json:"tooltip,omitempty"`
}

// TitlePiece is a run of text in one colour.
//
// Colour is either `0xAARRGGBB`, the spelling the rest of this system uses for
// a colour, or the name of one of AppKit's own — `systemRed`, `labelColor` —
// which is the spelling that adapts when somebody switches to light mode. An
// empty Colour is the ordinary menu bar text colour.
type TitlePiece struct {
	Text string `json:"text"`
	// Symbol is an SF Symbol name drawn ahead of the text, and it is what makes
	// this look like a menu bar item rather than like text somebody typed into
	// one. A symbol is drawn by the system at the bar's own optical size and
	// weight, hinted for the size it ends up at; a character from a font is
	// not, and next to Bluetooth and the battery it shows.
	//
	// Text is still the fallback: a symbol this version of macOS has never
	// heard of draws nothing, and a row with nothing in it is worse than a
	// circle.
	Symbol string `json:"symbol,omitempty"`
	Colour string `json:"colour,omitempty"`
	// Flicker says this piece should be asking for somebody, and it is the only
	// thing in this document that is about TIME rather than about what is on the
	// screen. The drawing side decides what flickering looks like; this says which
	// pieces do it, which keeps the judgement — is anything waiting on a person
	// — on the side of the boundary that can be tested.
	//
	// It is per piece and not per item because they are not all the same: three
	// agents working and one waiting is one mark sitting still beside one mark
	// asking for you, and an item that flickered as a whole would be asking on
	// behalf of the three that are perfectly happy.
	Flicker bool `json:"flicker,omitempty"`
}

// WhatKindOfRow is what a menu row is for. A menu is a flat list rather than a
// tree because that is what a semaphore is: states, and the sessions in them.
type WhatKindOfRow string

const (
	// Section names a state. It is drawn as a real menu section header — small,
	// grey and set apart — which is why it carries no colour and no glyph: the
	// header is structure, and the colour belongs on the rows it separates.
	Section WhatKindOfRow = "section"
	// Row is one session. Choosing it focuses that session.
	Row WhatKindOfRow = "row"
	// Note is a line that says something rather than offering something: "no
	// agents", "and 3 more".
	Note WhatKindOfRow = "note"
	// Separator is the rule between one state's rows and the next's. A section
	// header alone turned out not to divide them: it is small, grey and set in
	// the same column as everything else, and a menu of eight rows read as one
	// list with grey words in it.
	Separator WhatKindOfRow = "separator"
)

// MenuRow is one row of the menu.
type MenuRow struct {
	Kind WhatKindOfRow `json:"kind"`
	Text string        `json:"text,omitempty"`
	// Symbol is the SF Symbol for this row's state, drawn in the menu item's
	// own image well — which is where AppKit puts an icon, and where every
	// other menu on the system puts one.
	Symbol string `json:"symbol,omitempty"`
	// Glyph is the fallback when the symbol is not available, drawn in Colour
	// ahead of the text. It is separate from Text so that the mark can be
	// coloured and the session's own name left in the ordinary menu colour.
	Glyph  string `json:"glyph,omitempty"`
	Colour string `json:"colour,omitempty"`
	// Age is drawn right-aligned against the far edge of the menu, on its own
	// tab stop. It is a separate field rather than part of Text because a menu
	// is set in a proportional font, where "name        4m" assembled with
	// spaces is ragged and assembled with a tab stop is not.
	Age string `json:"age,omitempty"`
	// Key is the session this row is about, in the form `focus-session` takes.
	// Only a Row has one, and a Row without one cannot be chosen.
	Key string `json:"key,omitempty"`
	// Lit marks the row an announcement is about, so that the menu opened
	// during one says which session it was talking about.
	Lit bool `json:"lit,omitempty"`
}
