package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/lassoColombo/agent-notify/session"
	"github.com/lassoColombo/agent-notify/subscribe"
)

// Name is what this integration calls itself: in the handshake, in the log, in
// the key of its own table in agent-notify's configuration file, and in the
// section of a record where its captured context is stored.
//
// The role is part of the name because zellij is two programs (D-37). This one
// paints titles; agent-notify-zellij-container, installed separately, is the
// one that can move your focus. Each captures the two variables it needs for
// itself, so either works with the other absent.
const Name = "zellij-display"

// me is this integration: how everything here asks where agent-notify's files
// are, and what its own settings say.
//
// Root is left empty on purpose — empty means "wherever this process's
// environment says", which is right everywhere but a test (D-69).
var me = subscribe.Integration{Name: Name}

// DefaultGlyphs is what a state looks like on a zellij title bar.
//
// Nerd Font, Font Awesome's BMP block, chosen so the states are told apart by
// SHAPE: a zellij title carries no colour, so shape has to do the whole job.
// Two of them are the ones the previous nushell implementation settled on after
// living with them, which is a better reason than taste.
//
// Written as ESCAPES, never as literal characters. These codepoints live in the
// Unicode Private Use Area, and a great deal of tooling — editors, clipboards,
// terminal multiplexers, JSON pretty-printers — quietly drops them. Pasting
// them in produced, in the implementation before this one, a table whose six
// entries were all the empty string, and a display where every state looked the
// same.
//
// `idle` and `ended` are empty on purpose: a quiet agent shows its name and
// nothing else, and a session that has ended shows nothing at all (D-26).
var DefaultGlyphs = map[session.Kernel]string{
	session.BlockedOnYou:  "\uf071", // warning triangle — blocked, needs a hand
	session.Broke:         "\uf00d", // a cross — the turn died
	session.FinishedATurn: "\uf075", // speech bubble — it is talking to you
	session.Working:       "\uf021", // circular arrows — turning, in progress
	session.Idle:          "",
	session.Ended:         "",
}

// Settings is `[integration.zellij-display.settings]`, and nothing else in the file is
// ours. A key here that this struct does not declare is refused by name rather
// than ignored (subscribe.Settings), because a misspelled glyph that changes
// nothing and says nothing is the config bug people give up on.
type Settings struct {
	// Zellij is where zellij is, as an absolute path.
	//
	// Required, and named after the tool rather than called `binary` because
	// the table above it has a `binary` of its own — this program's — and the
	// install prints both together (D-67).
	Zellij string `toml:"zellij"`
	// Glyphs is the user's table, keyed on `kernel` or `kernel/detail`,
	// layered over DefaultGlyphs (§A5.3).
	Glyphs map[string]string `toml:"glyphs"`
}

// Resolved is Settings after the one thing that can fail has been done once, at
// startup, where it can still be reported to a person.
type Resolved struct {
	Binary string
	Glyphs session.Palette
}

// ZellijTimeout bounds one zellij invocation. It has a name and a defined
// behaviour on expiry, which is what R18 asks for: this render gives up on that
// zellij session, and the next change tries again.
//
// It is not configuration. Nobody editing a config file knows better than this
// how long `zellij action list-panes` should be allowed to take, and the answer
// does not vary by machine in a way a person could act on.
const ZellijTimeout = 2 * time.Second

// Read loads this integration's own section and resolves it.
func Read(given subscribe.Integration) (Resolved, error) {
	var settings Settings
	if err := given.Settings(&settings); err != nil {
		return Resolved{}, err
	}

	binary, err := TheZellijToRun(settings.Zellij)
	if err != nil {
		return Resolved{}, err
	}
	return Resolved{
		Binary: binary,
		Glyphs: session.NewPalette(DefaultGlyphs, settings.Glyphs),
	}, nil
}

// TheZellijToRun is the path out of the configuration, checked.
//
// There is no search here and no PATH fallback, and the absence is the design
// (D-67). This program is started by the session-watcher, whose PATH is not
// your shell's — a launchd job's is /usr/bin:/bin and nothing else — so a
// lookup performed HERE is a lookup performed in the one context that cannot
// answer it. The list of Homebrew prefixes that used to sit at this line was an
// attempt to guess what the environment would not say.
//
// The lookup happens at install instead, which runs in your shell, where the
// answer is simply available. What is left at runtime is reading a string
// somebody has seen and agreeing that it points at something.
func TheZellijToRun(given string) (string, error) {
	const key = "[integration." + Name + ".settings] zellij"
	switch {
	case given == "":
		return "", fmt.Errorf("%s is not set — run `agent-notify install %s`, which finds "+
			"zellij in your own shell and prints the line to add", key, Name)
	case !filepath.IsAbs(given):
		// A relative name would be resolved against whatever PATH this
		// happened to be started with, which is the whole problem restated.
		return "", fmt.Errorf("%s = %q must be an absolute path: a supervised child's PATH "+
			"is not yours, so a bare name means something different here than it does to you",
			key, given)
	}
	if _, err := os.Stat(given); err != nil {
		return "", fmt.Errorf("%s = %q: %w", key, given, err)
	}
	return given, nil
}
