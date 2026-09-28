// Command agent-notify-aerospace-container finds and focuses the WINDOW an
// [agent-notify] session is in, using the aerospace window manager.
//
// It is the outermost rung of a focus: a session is at a path, and every layer
// of it has to be right for any of it to be visible.
//
//	aerospace   ──▶  Ghostty  ──▶  zellij     ──▶  pane 17
//	window 34                      session home
//
// Focusing a pane in a window you cannot see changes nothing on your screen,
// which is the whole reason this program exists. How it finds the window — and
// why it stores nothing about it — is in place.go.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/lassoColombo/agent-notify/container"
	"github.com/lassoColombo/agent-notify/subscribe"
	"github.com/lassoColombo/agent-notify/tool"
)

// Settings is `[integration.aerospace-container.settings]`, and nothing else in
// the file is ours. An undeclared key is refused by name.
type Settings struct {
	// Aerospace is where aerospace is, as an absolute path. Required, and named
	// after the tool rather than called `binary` because the table above it has
	// a `binary` of its own — this program's — and install prints both
	// together (D-67).
	Aerospace string `toml:"aerospace"`

	// Title is how to build the window-title key, with {VARIABLE} filled in
	// from the agent's environment. It is configuration because the coupling it
	// encodes belongs to whoever set the two programs up: zellij titles its
	// terminal window `<session> | <active tab>`, and that convention is not
	// aerospace's business to know and not ours to hard-code (§A11.4).
	//
	// Setting it to "" turns the key off, leaving the process chain as the only
	// way in — which is the right setting for somebody who runs no multiplexer.
	Title string `toml:"title"`
}

// The zellij convention, which is the one people actually have. The separator
// is part of the key and is what makes it specific enough to trust: `home`
// would match any window whose title happens to start that way.
const defaultTitle = "{ZELLIJ_SESSION_NAME} | "

// aerospaceTimeout bounds one `aerospace` invocation. It has a name and a
// defined behaviour on expiry, which is what R18 asks for: the subcommand
// answers "I could not", core reads that as a container that did not run, and
// nothing waits.
//
// It is not configuration. Nobody editing a config file knows better than this
// how long `aerospace list-windows` should be allowed to take.
const aerospaceTimeout = 2 * time.Second

func main() {
	if len(os.Args) > 1 && os.Args[1] == "install" {
		os.Exit(install(os.Args[2:]))
	}
	settings, err := resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", Name, err)
		os.Exit(1)
	}

	me.Reads = func() (any, error) { return Capture(settings.Title) }
	os.Exit(subscribe.Main(me, subscribe.Commands{
		Named:     map[string]func([]string) int{"install": install},
		Interpret: func(captured json.RawMessage) (any, error) { return Interpret(settings.Title, captured) },

		// The binary is looked up HERE and not before dispatch, which is the
		// one place this differs from zellij's container on purpose. Capture
		// and interpret do not touch aerospace, and capture runs on the hook
		// path: a window manager that is not installed must not be able to
		// fail an agent's hook. Where it does matter, not finding it is a
		// typed outcome rather than a crash.
		Focus: func(coordinates json.RawMessage) (container.Outcome, error) {
			aerospace, err := settings.aerospace()
			if err != nil {
				return container.Failed(container.NotRunning, err.Error()), nil
			}
			return Focus(aerospace, coordinates)
		},
		Focused: func(coordinates json.RawMessage) (container.Verdict, error) {
			aerospace, err := settings.aerospace()
			if err != nil {
				return cannotTell(err.Error()), nil
			}
			return Focused(aerospace, coordinates)
		},
	}, os.Args[1:]))
}

// resolve reads this integration's own section of agent-notify's config.
//
// Its defaults are the whole configuration for the setup this was built
// against, so the table in the config file is one line — the binary — and even
// that is only there because a hook's PATH is not your shell's PATH.
func resolve() (Settings, error) {
	settings := Settings{Title: defaultTitle}
	if err := me.Settings(&settings); err != nil {
		return Settings{}, err
	}
	return settings, nil
}

func (s Settings) aerospace() (Aerospace, error) {
	binary, err := tool.AbsolutePath("[integration."+Name+".settings] aerospace", s.Aerospace)
	if err != nil {
		return Aerospace{}, err
	}
	return Aerospace{Binary: binary, Timeout: aerospaceTimeout}, nil
}
