package main

import (
	"fmt"

	"github.com/lassoColombo/agent-notify/session"
	"github.com/lassoColombo/agent-notify/subscribe"
	"github.com/lassoColombo/agent-notify/tool"
)

// Name is the config table, `integrations.tmux-cockpit`.
const Name = "tmux-cockpit"

// me answers nothing core calls, which is what keeps core from ever running
// this as a display: it is started from a key, like the picker. WakeOn is
// what the list's own watch of the store asks for.
var me = subscribe.Integration{Name: Name, WakeOn: WhatToWakeFor()}

// WhatToWakeFor is what the list draws from. A test moves one record field at
// a time and fails when the list comes out different for something not named
// here (D-73). `state_since` is the age a row shows and what orders two agents
// in one state; `captured_context` is where the pane is.
func WhatToWakeFor() []string {
	return []string{"kernel", "rank", "state_since", "name", "cwd", "captured_context", "derived_context"}
}

// DefaultGlyphs are agent-notify-tmux's, so that a state looks the same on a
// pane's border and in the list. Written as escapes: tooling quietly drops
// private-use characters.
var DefaultGlyphs = map[session.Kernel]string{
	session.BlockedOnYou:  "", // warning triangle
	session.Broke:         "", // a cross
	session.FinishedATurn: "", // speech bubble
	session.Working:       "", // circular arrows
	session.Idle:          "",
	session.Ended:         "",
}

// Settings is `integrations.tmux-cockpit.settings`; a key not declared here
// is refused by name.
type Settings struct {
	// Tmux is where tmux is, when it is not on this process's PATH (D-101).
	Tmux string `yaml:"tmux"`
	// Glyphs is the user's table, keyed on `kernel`, layered over
	// DefaultGlyphs.
	Glyphs map[string]string `yaml:"glyphs"`
	// ListWidth is how many columns the list is given when the tab is made.
	ListWidth int `yaml:"list_width"`
}

type Resolved struct {
	Binary    string
	Glyphs    session.Palette
	ListWidth int
}

const (
	defaultListWidth  = 36
	narrowestList     = 20
	widthSettingsName = "integrations." + Name + ".settings.list_width"
)

func Read() (Resolved, error) {
	var settings Settings
	if err := me.Settings(&settings); err != nil {
		return Resolved{}, err
	}
	binary, err := tool.Find("integrations."+Name+".settings.tmux", "tmux", settings.Tmux)
	if err != nil {
		return Resolved{}, err
	}
	width := settings.ListWidth
	switch {
	case width == 0:
		width = defaultListWidth
	case width < narrowestList:
		return Resolved{}, fmt.Errorf("%s is %d, and a list narrower than %d columns shows nothing",
			widthSettingsName, width, narrowestList)
	}
	return Resolved{
		Binary:    binary,
		Glyphs:    session.NewPalette(DefaultGlyphs, settings.Glyphs),
		ListWidth: width,
	}, nil
}
