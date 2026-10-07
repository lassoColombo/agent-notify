package main

import (
	"fmt"

	"github.com/lassoColombo/agent-notify/session"
	"github.com/lassoColombo/agent-notify/subscribe"
)

// Name is what this integration calls itself: its config table and its
// handshake.
//
// It was a separate integration from the menu bar rather than a setting on it
// (D-37), because macOS files its decision about notifications against a bundle
// identifier and that decision cannot be unmade — so the program that asked a
// person for permission had to be the one that needed it, with nothing else
// riding on the same identifier. Neither half of that is load bearing any more:
// there is no bundle of ours (D-86) and no menu bar display (D-87).
const Name = "macos-notifier"

var me = subscribe.Integration{Name: Name, WakeOn: WhatToWakeFor()}

// DefaultColours is what the invader on a banner is drawn in.
//
// Rosé Pine. Only the three states that ever notify are ever drawn; the other
// two are here so that the table is a whole table, which is what let it be
// compared with the menu bar display's when there was one to disagree with
// (D-54, D-87).
//
// The alpha is written and then dropped: a banner's picture is composited by
// macOS onto a surface nobody here knows the colour of (invaderpngs.go).
var DefaultColours = map[session.Kernel]string{
	session.BlockedOnYou:  "0xffeb6f92", // love — answer this now
	session.Broke:         "0xfff6c177", // gold — something went wrong
	session.FinishedATurn: "0xff9ccfd8", // foam — there is something to read
	session.Working:       "0xffc4a7e7", // iris — never notifies
	session.Idle:          "0xcce0def4", // text — never notifies
}

// Settings is `[integration.macos-notifier.settings]`, and nothing else in
// the file is ours. A key nobody declared is refused by name.
type Settings struct {
	// Alerter is the program that puts the banner on the screen, absolute,
	// written by `install` from your PATH because nothing core starts has one
	// (D-67). Empty falls back to PATH, which is right for a shell and wrong
	// for a session-watcher.
	Alerter string `toml:"alerter"`

	// Preview is how much of what the agent said a banner carries.
	Preview PreviewSettings `toml:"preview"`

	// Sound is what a banner plays: `true` for the default chime, `false` for
	// silence, or the name of a sound — "Submarine", or anything in
	// ~/Library/Sounds. Absent is `true`.
	//
	// It is here, and nothing else about how a banner is presented is, because
	// this is the one of those things a person has a reason to decide per
	// MACHINE rather than once: the same config on a laptop in a meeting and a
	// desktop at home wants a different noise, or none. macOS's own switch is
	// per-app and has no third setting between chime and silence.
	//
	// `any`, because TOML is typed and this key takes two types; taken apart by
	// theSoundIn (sounds.go), which is also where a name is checked against the
	// files that actually exist.
	//
	// It decides what THIS program attaches, and nothing more. System Settings
	// → Notifications still has the last word — sound turned off there is
	// silence whatever is written here.
	Sound any `toml:"sound"`

	// Colors is the invader's colour per state, as `0xAARRGGBB`. Hex only, and
	// not AppKit's colour names: the invader is drawn into a PNG that macOS
	// composites onto a surface of its own, where a colour that adapts to the
	// appearance has nothing to adapt to.
	Colors map[string]string `toml:"colors"`
}

// PreviewSettings is `[integration.macos-notifier.settings.preview]`.
type PreviewSettings struct {
	Lines *int `toml:"lines"`
	Width *int `toml:"width"`
}

// Resolved is Settings after everything that can fail has been done once, at
// startup, where it can still be reported to a person.
type Resolved struct {
	Preview Preview
	Sound   Sound
	Colours session.Palette
	// Alerter is the program that posts, found once here so that a missing one
	// is a refusal at startup rather than a banner that never arrives.
	Alerter string
}

// The default preview: eight lines of sixty characters. A banner is not a
// document — what it is for is recognising which of your agents this is, and
// whether what it last said is the thing you are waiting for.
var defaultPreview = Preview{Lines: 8, Width: 60}

func Read(given subscribe.Integration) (Resolved, error) {
	var settings Settings
	if err := given.Settings(&settings); err != nil {
		return Resolved{}, err
	}

	resolved := Resolved{
		Preview: defaultPreview,
		Colours: session.NewPalette(DefaultColours, settings.Colors),
	}

	// Loud unless somebody says otherwise: a notification is the one thing in
	// this system whose whole job is to interrupt, and one that arrives
	// silently behind whatever you are looking at is a notification you find
	// later. Asked now, for the reason every other check here is: a name macOS
	// cannot find is a chime that is merely the wrong one, which nobody debugs.
	sound, err := theSoundIn(settings.Sound, SoundDirectories())
	if err != nil {
		return Resolved{}, err
	}
	resolved.Sound = sound
	// Asked now, while there is somebody to tell. A colour that cannot be
	// parsed is not an error at posting time — it is a banner that quietly
	// arrives without its picture, which looks exactly like a banner nobody
	// bothered to draw one for.
	for state, value := range settings.Colors {
		if _, ok := theRGBWithoutTheAlpha(value); !ok {
			return Resolved{}, fmt.Errorf(
				"[integration.%s.settings.colors] %s = %q is not 0xAARRGGBB — AppKit's "+
					"colour names are no use here, because the mark is drawn into a file",
				Name, state, value)
		}
	}
	if settings.Preview.Lines != nil {
		resolved.Preview.Lines = *settings.Preview.Lines
	}
	if settings.Preview.Width != nil {
		resolved.Preview.Width = *settings.Preview.Width
	}
	if problem := resolved.Preview.sane(); problem != "" {
		return Resolved{}, fmt.Errorf("[integration.%s.settings.preview] %s", Name, problem)
	}

	// Refused now rather than when somebody taps a banner.
	if _, err := given.CoreBinary(); err != nil {
		return Resolved{}, fmt.Errorf("tapping a banner has to be able to run something, and %w", err)
	}
	// And refused now rather than at the moment something is worth saying: a
	// display that discovers it cannot post only when it has something to post
	// is a display that fails on the one occasion it mattered.
	alerter, err := Alerter(settings.Alerter)
	if err != nil {
		return Resolved{}, err
	}
	resolved.Alerter = alerter
	return resolved, nil
}
