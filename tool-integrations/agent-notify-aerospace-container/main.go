// Command agent-notify-aerospace-container finds and focuses the WINDOW an
// [agent-notify] session is in, using the aerospace window manager.
//
// It is the outermost rung of a focus: a session is at a path, and every layer
// of it has to be right for any of it to be visible.
//
//	aerospace   ──▶  Ghostty  ──▶  tmux       ──▶  pane %17
//	window 34                      session home
//
// Focusing a pane in a window you cannot see changes nothing on your screen,
// which is the whole reason this program exists. How it finds the window — and
// why it stores nothing about it — is in place.go.
package main

import (
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/lassoColombo/agent-notify/container"
	"github.com/lassoColombo/agent-notify/session"
	"github.com/lassoColombo/agent-notify/subscribe"
	"github.com/lassoColombo/agent-notify/tool"
)

// Settings is `integrations.aerospace-container.settings`, and nothing else in
// the file is ours. An undeclared key is refused by name.
type Settings struct {
	// Aerospace is where aerospace is, when it is not on this process's PATH
	// (D-101).
	Aerospace string `yaml:"aerospace"`

	// Title is how to build the window-title key, with {VARIABLE} filled in
	// from the agent's environment. It is configuration because the coupling it
	// encodes belongs to whoever set the two programs up: a multiplexer that
	// titles its terminal window `<session> | …` and puts the session's name
	// in the environment, and that convention is not aerospace's business to
	// know and not ours to hard-code (§A11.4).
	//
	// Empty, the default, turns the key off, leaving the process chain as the
	// only way in.
	Title string `yaml:"title"`
}

// aerospaceTimeout bounds one `aerospace` invocation. It has a name and a
// defined behaviour on expiry, which is what R18 asks for: the subcommand
// answers "I could not", core reads that as a container that did not run, and
// nothing waits.
//
// It is not configuration. Nobody editing a config file knows better than this
// how long `aerospace list-windows` should be allowed to take.
const aerospaceTimeout = 2 * time.Second

func main() {
	// Read on first use, so that `capture-environment`, on the path the agent
	// waits on, reads the file only when it must.
	settings := sync.OnceValues(func() (Settings, error) { return Read(me) })
	me.Reads = func() (any, error) {
		resolved, err := settings()
		if err != nil {
			return nil, err
		}
		return Capture(resolved.Title)
	}
	os.Exit(subscribe.Main(me, subscribe.Commands{
		Interpret: func(captured json.RawMessage, ancestry []session.Ancestor) (any, error) {
			resolved, err := settings()
			if err != nil {
				return nil, err
			}
			return Interpret(resolved.Title, captured, ancestry)
		},
		// The binary is looked up here and not before dispatch: capture and
		// interpret do not touch aerospace, and a window manager that is not
		// installed must not be able to fail an agent's hook. Where it does
		// matter, not finding it is a typed outcome rather than a crash.
		Focus: func(coordinates json.RawMessage) (container.Outcome, error) {
			resolved, err := settings()
			if err == nil {
				var aerospace Aerospace
				if aerospace, err = resolved.aerospace(); err == nil {
					return Focus(aerospace, coordinates)
				}
			}
			return container.Failed(container.NotRunning, err.Error()), nil
		},
		Focused: func(coordinates json.RawMessage) (container.Verdict, error) {
			resolved, err := settings()
			if err == nil {
				var aerospace Aerospace
				if aerospace, err = resolved.aerospace(); err == nil {
					return Focused(aerospace, coordinates)
				}
			}
			return cannotTell(err.Error()), nil
		},
	}, os.Args[1:]))
}

// Read is this integration's own section of agent-notify's config. Its
// defaults are the whole configuration for an agent run in a terminal with no
// multiplexer.
func Read(given subscribe.Integration) (Settings, error) {
	settings := Settings{}
	if err := given.Settings(&settings); err != nil {
		return Settings{}, err
	}
	return settings, nil
}

func (s Settings) aerospace() (Aerospace, error) {
	binary, err := tool.Find("integrations."+Name+".settings.aerospace", "aerospace", s.Aerospace)
	if err != nil {
		return Aerospace{}, err
	}
	return Aerospace{Binary: binary, Timeout: aerospaceTimeout}, nil
}
