// Command agent-notify-zellij is agent-notify's zellij integration: it paints
// agent state onto pane and tab titles, and it places and focuses sessions.
// One program because the two halves read the same two variables and drive
// the same tool; wanting titles without focus is a matter of leaving it out
// of `[container] order`.
package main

import (
	"time"

	"github.com/lassoColombo/agent-notify/session"
	"github.com/lassoColombo/agent-notify/subscribe"
	"github.com/lassoColombo/agent-notify/tool"
)

// Name is the config table, the entry in `[container] order`, and the key of
// this program's section of a record.
const Name = "zellij"

var me = subscribe.Integration{
	Name:   Name,
	WakeOn: WhatToWakeFor(),
	// Ended sessions too: the record is the only thing that remembers which
	// pane a finished agent had, and that pane has to be given back.
	WantEnded: true,
	Reads:     Capture,
}

// WhatToWakeFor is what a title can show. A test moves one record field at a
// time and fails when a different plan comes out for something not named here
// (D-73). `captured_context` is where the pane lives.
func WhatToWakeFor() []string {
	return []string{"kernel", "detail", "rank", "name", "cwd", "captured_context"}
}

// DefaultGlyphs are Nerd Font codepoints, chosen so states are told apart by
// shape, since a title carries no colour. Written as escapes: tooling quietly
// drops private-use characters. Idle and ended are empty on purpose.
var DefaultGlyphs = map[session.Kernel]string{
	session.BlockedOnYou:  "", // warning triangle
	session.Broke:         "", // a cross
	session.FinishedATurn: "", // speech bubble
	session.Working:       "", // circular arrows
	session.Idle:          "",
	session.Ended:         "",
}

// Settings is `[integration.zellij.settings]`; a key not declared here is
// refused by name.
type Settings struct {
	// Zellij is where zellij is, as an absolute path.
	Zellij string `toml:"zellij"`
	// Glyphs is the user's table, keyed on `kernel` or `kernel/detail`,
	// layered over DefaultGlyphs.
	Glyphs map[string]string `toml:"glyphs"`
}

type Resolved struct {
	Zellij Zellij
	Glyphs session.Palette
}

// zellijTimeout bounds one zellij invocation. On expiry a render skips that
// zellij session and a focus answers "not running".
const zellijTimeout = 2 * time.Second

func Read(given subscribe.Integration) (Resolved, error) {
	var settings Settings
	if err := given.Settings(&settings); err != nil {
		return Resolved{}, err
	}
	binary, err := tool.AbsolutePath("[integration."+Name+".settings] zellij", settings.Zellij)
	if err != nil {
		return Resolved{}, err
	}
	return Resolved{
		Zellij: Zellij{Binary: binary, Timeout: zellijTimeout},
		Glyphs: session.NewPalette(DefaultGlyphs, settings.Glyphs),
	}, nil
}
