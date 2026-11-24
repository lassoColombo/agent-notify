// Command agent-notify-tmux is agent-notify's tmux integration: it writes
// each agent's state, glyph and name on the pane it runs in, and it places and
// focuses sessions. One program because the two halves read the same two
// variables and drive the same tool; wanting the marks without focus is a
// matter of leaving it out of `containers`. Being on PATH is the whole of
// installing it (D-102).
package main

import (
	"time"

	"github.com/lassoColombo/agent-notify/session"
	"github.com/lassoColombo/agent-notify/subscribe"
	"github.com/lassoColombo/agent-notify/tool"
)

// Name is the config table, the entry in `containers`, and the key of
// this program's section of a record.
const Name = "tmux"

var me = subscribe.Integration{
	Name:   Name,
	WakeOn: WhatToWakeFor(),
	// Ended sessions too: a server whose agents have all ended still carries
	// their marks, and only a record that places a session there brings the
	// render back to that server to take them off.
	WantEnded: true,
	Reads:     Capture,
}

// WhatToWakeFor is what a mark can show. A test moves one record field at a
// time and fails when a different plan comes out for something not named here
// (D-73). `captured_context` is where the pane lives.
func WhatToWakeFor() []string {
	return []string{"kernel", "detail", "rank", "name", "cwd", "captured_context"}
}

// Settings is `integrations.tmux.settings`; a key not declared here is
// refused by name.
type Settings struct {
	// Tmux is where tmux is, when it is not on this process's PATH (D-101).
	Tmux string `yaml:"tmux"`
	// Glyphs is the user's table, keyed on `kernel` or `kernel/detail`,
	// layered over session.DefaultGlyphs.
	Glyphs map[string]string `yaml:"glyphs"`
}

type Resolved struct {
	Tmux   Tmux
	Glyphs session.Palette
}

// tmuxTimeout bounds one tmux invocation. On expiry a render skips that tmux
// server and a focus answers "not running".
const tmuxTimeout = 2 * time.Second

func Read(given subscribe.Integration) (Resolved, error) {
	var settings Settings
	if err := given.Settings(&settings); err != nil {
		return Resolved{}, err
	}
	binary, err := tool.Find("integrations."+Name+".settings.tmux", "tmux", settings.Tmux)
	if err != nil {
		return Resolved{}, err
	}
	return Resolved{
		Tmux:   Tmux{Binary: binary, Timeout: tmuxTimeout},
		Glyphs: session.NewPalette(session.DefaultGlyphs, settings.Glyphs),
	}, nil
}
