package main

import (
	"fmt"
	"time"

	"github.com/lassoColombo/agent-notify/session"
	"github.com/lassoColombo/agent-notify/subscribe"
)

// Name is what this integration calls itself: its config table and its
// handshake.
//
// The role is in the name (D-37) even though macOS is only ever going to be a
// display here, because `macos` alone would be the obvious name for a container
// over the same window system — and that container is a real possibility, with
// the same measurements behind it as this display.
const Name = "macos-bar"

// me is this integration: how everything here asks where agent-notify's files
// are, and what its own settings say.
//
// Root is left empty on purpose — empty means "wherever this process's
// environment says", which is right everywhere but a test (D-69).
var me = subscribe.Integration{Name: Name}

// DefaultGlyphs is the mark on each count.
//
// Geometric shapes from the Unicode block every Mac font has, and NOT the Nerd
// Font private-use codepoints the other displays use. That is the one thing
// about a native bar that is less free than sketchybar's: sketchybar is told
// which font to draw an item in, and the menu bar is drawn in the system font,
// which has no idea what `` is and shows a box. A person who wants their
// Nerd Font glyphs can have them by naming a font in the settings.
//
// The shapes are different from each other as shapes, not only as colours, so
// that the item still says something with the colour taken away.
var DefaultGlyphs = map[session.Kernel]string{
	session.BlockedOnYou:  "?", // it is asking you
	session.Broke:         "!", // it went wrong
	session.FinishedATurn: "●", // solid: there is something to read
	session.Working:       "◌", // dotted: still going
	session.Idle:          "○", // hollow: nothing
}

// DefaultSymbols is the mark each state wears in the menu.
//
// All of them the same: this program's own sprite, in that state's colour, and
// never moving — the flicker belongs to the bar, where it is a signal somebody
// might be trying to catch out of the corner of an eye. A menu is already being
// looked at. Something blinking inside one is just something hard to read.
//
// The rows do not need their icons to name their states, because the section
// headers do that and the colours do it again. What they need is to look like
// one program, and five different SF Symbols did not: a triangle, a bubble and
// three circles read as five unrelated things stacked up.
//
// It stays a palette rather than becoming a constant so that anybody who wants
// SF Symbols back can have them, one state at a time, in the same table every
// other appearance setting lives in. A name this macOS does not know is refused
// when the config is read.
var DefaultSymbols = map[session.Kernel]string{
	session.BlockedOnYou:  Invader,
	session.Broke:         Invader,
	session.FinishedATurn: Invader,
	session.Working:       Invader,
	session.Idle:          Invader,
}

// DefaultColours is what each state is worth looking at in.
//
// **Rosé Pine**, which is the palette this machine is themed in end to end —
// the terminal, zellij, the editor — so the item belongs to the bar it sits on
// rather than looking like a notification from another application. The values
// are the official ones, quoted here rather than read from
// `~/.config/tinted-theming/scheme.yaml`, because a display that cannot draw
// until somebody else's config file is present is a display that breaks on a
// machine that never had it.
//
// **Every state has its own hue, and that is a reversal.** The rule here used
// to be that only the states wanting something were coloured and the rest were
// plain text, because a translucent menu bar carrying five hues all day is a
// light nobody can stop noticing. That was right for an item that showed ONE
// mark: the colour was the signal, so it had to be rationed. It is wrong for an
// item that shows one mark per live state, because then five identical sprites
// sit side by side and colour is the only thing telling them apart. What is
// rationed now is MOVEMENT: only the states waiting on a person flicker, and
// the rest sit perfectly still. A hue that never moves is quiet in a way a hue
// on its own is not.
//
// The hues are chosen against each other rather than one at a time, because
// they are on the bar together. What has to be distinguishable is anything in
// the same MOVEMENT class: the three that flicker are Love, Gold and Foam, and
// the two that sit still are Iris and dimmed text. Working is Iris and not
// Pine — the sketchybar display's choice, and the one this started with —
// because `#31748f` is built for an opaque background, and brightening it to
// `base14` put it a shade away from the Foam beside it. Two blues on a
// translucent bar are one blue.
//
// The cost of a fixed palette is light mode: these are the values for a dark
// menu bar, and somebody who switches appearance wants `labelColor` and friends
// back, which `[…settings.colors]` still takes by name.
var DefaultColours = map[session.Kernel]string{
	session.BlockedOnYou:  "0xffeb6f92", // love — answer this now
	session.Broke:         "0xfff6c177", // gold — something went wrong
	session.FinishedATurn: "0xff9ccfd8", // foam — there is something to read
	session.Working:       "0xffc4a7e7", // iris — getting on with it
	// Idle is the text colour at 80%, and not the palette's Subtle or Muted
	// tones, which are the one thing that does not survive the move onto a menu
	// bar: they are dim tones designed for an opaque `#191724`, and the bar is a
	// translucent grey that is lighter than that, so both of them come out
	// darker than the system's own secondary label — which was already too dark
	// to read. Dimming the text colour keeps the quiet state quiet without
	// making it a colour nobody can see.
	session.Idle: "0xcce0def4",
}

// Settings is `[integration.macos-bar.settings]`, and nothing else in the
// file is ours. A key nobody declared is refused by name.
type Settings struct {
	// Rows bounds how many sessions a state lists before it says "and n more".
	Rows int `toml:"rows"`
	// Announce is how long a state change adds the name of the session it
	// happened to, in that state's colour, after the counts — which stay where
	// they are. Off by default — see the constant — because macOS has a
	// notification system and this is not it.
	//
	// Written the way a person writes a duration, which is one spelling for
	// every duration in this system rather than this display's own: "8s" is
	// eight seconds, "0s" keeps the item passive (session.Duration, D-78).
	//
	// A POINTER because "0s" and an absent key are different requests and a
	// value cannot tell them apart.
	Announce *session.Duration `toml:"announce"`
	// Resting is the colour of the mark on the bar while nothing wants you.
	Resting string `toml:"resting"`
	// Sign is the code signing identity `install` gives the bundle, remembered
	// here so that re-running install after a rebuild does not need the flag
	// again. Nothing at runtime reads it; an empty one means ad-hoc, which
	// works for everything except notifications (bundle.go).
	Sign string `toml:"sign"`
	// Font is the family the item is drawn in, for somebody who wants their
	// own glyphs. Empty is the menu bar's own font, which is the right answer
	// for the shapes this ships with.
	Font string `toml:"font"`

	Glyphs  map[string]string `toml:"glyphs"`
	Symbols map[string]string `toml:"symbols"`
	Colors  map[string]string `toml:"colors"`
}

// Resolved is Settings after everything that can fail has been done once, at
// startup, where it can still be reported to a person.
type Resolved struct {
	Core     string
	Rows     int
	Announce time.Duration
	Resting  string
	Font     string
	Glyphs   session.Palette
	Symbols  session.Palette
	Colours  session.Palette
}

// Refresh is how often the item is repainted with nothing new to say, so that
// the ages in the menu stay true. Elapsed time is rendering and is never a
// state change (R26), so keeping it current is this display's own business —
// and how often it does it is this display's business too, not a person's.
const Refresh = 30 * time.Second

const (
	defaultRows = 8
	// Off. The item taking itself over to spell out a session's name was this
	// display's own idea of a notification, and it was a bad one: it is
	// invisible if you are not looking at the menu bar, it changes the item's
	// width on a bar where width is the scarcest thing there is, and macOS has
	// a notification system that does all of it better and is configurable in
	// System Settings. Set `announce = "8s"` to have it back.
	defaultAnnounce = 0
	// Subtle, the palette's dim text tone, and the one place in this display
	// where it is the right answer: with nothing running at all the mark is pure
	// identity and no signal, there to be found with a pointer and to say which
	// program this is. It is also the one hue no state wears, so an empty
	// machine cannot be mistaken for an agent doing something.
	defaultResting = "0xff8c88a6"
)

func Read(given subscribe.Integration) (Resolved, error) {
	var settings Settings
	if err := given.Settings(&settings); err != nil {
		return Resolved{}, err
	}

	resolved := Resolved{
		Rows: defaultRows, Announce: defaultAnnounce,
		Resting: defaultResting,
		Glyphs:  session.NewPalette(DefaultGlyphs, settings.Glyphs),
		Symbols: session.NewPalette(DefaultSymbols, settings.Symbols),
		Colours: session.NewPalette(DefaultColours, settings.Colors),
	}
	if settings.Rows > 0 {
		resolved.Rows = settings.Rows
	}
	// Asked now, while there is somebody to tell. A colour AppKit cannot draw
	// is not an error at paint time — it is a state quietly drawn in the
	// ordinary menu colour, which looks exactly like a state nobody bothered to
	// configure.
	for state, value := range settings.Colors {
		if !ColourKnown(value) {
			return Resolved{}, fmt.Errorf(
				"[integration.%s.settings.colors] %s = %q is neither 0xAARRGGBB nor a colour "+
					"AppKit knows (systemRed, systemOrange, labelColor, secondaryLabelColor, …)",
				Name, state, value)
		}
	}
	for state, name := range settings.Symbols {
		if !SymbolKnown(name) {
			return Resolved{}, fmt.Errorf(
				"[integration.%s.settings.symbols] %s = %q is not an SF Symbol this macOS has "+
					"(look them up in Apple's SF Symbols app)", Name, state, name)
		}
	}
	if !FontKnown(settings.Font) {
		return Resolved{}, fmt.Errorf(
			"[integration.%s.settings] font = %q is not a font on this machine",
			Name, settings.Font)
	}
	if settings.Resting != "" {
		if !ColourKnown(settings.Resting) {
			return Resolved{}, fmt.Errorf(
				"[integration.%s.settings] resting = %q is neither 0xAARRGGBB nor a colour "+
					"AppKit knows", Name, settings.Resting)
		}
		resolved.Resting = settings.Resting
	}
	resolved.Font = settings.Font
	if settings.Announce != nil {
		// An absent key takes the default; "0s" is not "unset", it is a request —
		// the item stays passive — and is honoured. What is left to refuse is
		// text that is not a duration at all, and a negative one.
		if bad := settings.Announce.Unreadable(); bad != "" {
			return Resolved{}, fmt.Errorf(
				"[integration.%s.settings] announce = %s "+
					"(\"8s\" is eight seconds, \"0s\" keeps the item passive)",
				Name, settings.Announce)
		}
		if settings.Announce.Duration() < 0 {
			return Resolved{}, fmt.Errorf(
				"[integration.%s.settings] announce = %s cannot be negative "+
					"(\"8s\" is eight seconds, \"0s\" keeps the item passive)",
				Name, settings.Announce)
		}
		resolved.Announce = settings.Announce.Duration()
	}

	// What choosing a row runs. Without it the menu is a list you cannot act
	// on, so this is a startup error rather than something discovered when
	// somebody picks a session.
	core, err := given.CoreBinary()
	if err != nil {
		return Resolved{}, fmt.Errorf("choosing a session has to be able to run something, and %w", err)
	}
	resolved.Core = core
	return resolved, nil
}
