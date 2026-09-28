// Command agent-notify-zellij-display paints agent state onto zellij's pane and tab
// titles: a glyph for what the session is doing, then what the session is
// called, with the tab wearing the glyph of the most urgent thing inside it.
//
// It is a tool-integration, and it is run rather than kept: the session-watcher
// hands it a view on stdin, it paints, it exits. Nothing it does can slow an
// agent down, and nothing it fails to do costs anybody else anything (R13).
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/lassoColombo/agent-notify/capture"
	"github.com/lassoColombo/agent-notify/logs"
	"github.com/lassoColombo/agent-notify/session"
)

func main() {
	arguments := os.Args[1:]
	if len(arguments) > 0 {
		switch arguments[0] {
		case "install":
			os.Exit(install(arguments[1:]))
		case session.CapabilitiesCommand:
			// The handshake, and the first thing core ever runs here: what
			// this program answers, so that nothing has to be inferred from
			// the user's file about what kind of program it is.
			os.Exit(capabilities.Answer(os.Stdout))
		case capture.Command:
			// Dispatched here, before anything is read or resolved: this one
			// runs as a child of the hook, on the path the agent waits on, and
			// it needs nothing but the environment it was started in (R1).
			//
			// It is the capture package and not the container SDK, because this
			// is a display and capture belongs to no role (D-59).
			os.Exit(capture.Main(Name, Capture))
		case session.MethodRender:
			os.Exit(render())
		case "-h", "--help", "help":
			usage(os.Stdout)
			os.Exit(0)
		default:
			fmt.Fprintf(os.Stderr, "agent-notify-zellij-display: %q is not a command\n\n", arguments[0])
			usage(os.Stderr)
			os.Exit(1)
		}
	}
	// There is no bare invocation any more. This program used to connect to
	// the session-watcher and stay there, and now the session-watcher runs it:
	// `render`, with the view on stdin, once per change it asked about. Running
	// it by hand with no arguments would have been a second display painting
	// the same panes.
	usage(os.Stderr)
	os.Exit(1)
}

func usage(to *os.File) {
	fmt.Fprint(to, `agent-notify-zellij-display — agent state on zellij pane and tab titles

  agent-notify-zellij-display render       paint once: the view on stdin, or the store if there is none
  agent-notify-zellij-display capture-environment
                                           what pane this process is in; run by the hook, not by you
  agent-notify-zellij-display capabilities what this program answers; run by the session-watcher, not by you
  agent-notify-zellij-display install      print the [integration.zellij-display] table to add; writes nothing
`)
}

// capabilities is what this display answers `capabilities` with, and it is the
// whole of what core decides with: whether to run this program at all, which
// changes are worth running it for, and whether the view it is handed carries
// the ended sessions it needs to give a pane back.
var capabilities = session.Capabilities{
	Methods: []string{session.MethodRender},
	WakeOn:  WhatToWakeFor(),

	// Ended sessions, for a display that will never draw one. It owns a piece
	// of a UI it did not create, and the record is the only thing that
	// remembers which piece, so this is how it learns to give a pane back
	// rather than leaving a dead agent's glyph on a live shell.
	WantEnded: true,
}

// render paints once and exits: the whole of this display, for a core that runs
// it rather than keeps it.
//
// The view arrives on stdin, the way every other subcommand takes its argument.
// It has to be handed over rather than read, because a render is not only about
// the sessions that exist: this display owns panes it did not create, and the
// only thing that remembers which pane a finished agent had is that agent's
// ended record. A process started fresh for one render remembers nothing at
// all, so what it is told is all it knows.
//
// With nothing on stdin it reads the store instead, which is the cold path
// (§A7.6) and what a person wants when asking "why does that say that". That
// one cannot give a pane back: the cold read returns live sessions only.
func render() int {
	log, closeLog := logs.Open(Name)
	defer closeLog.Close()
	settings, err := Read(me)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	view, err := viewOnStdinOrTheStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	display := &Display{Zellij: Zellij{Binary: settings.Binary, Timeout: ZellijTimeout},
		Glyphs: settings.Glyphs, Logger: log}
	_ = display.Render(view)
	return 0
}

// viewOnStdinOrTheStore is the view core handed over, or — for a person running
// this by hand — whatever the store says right now.
func viewOnStdinOrTheStore() (session.View, error) {
	handed, err := io.ReadAll(os.Stdin)
	if err != nil {
		return session.View{}, err
	}
	if len(bytes.TrimSpace(handed)) > 0 {
		var view session.View
		if err := json.Unmarshal(handed, &view); err != nil {
			return session.View{}, fmt.Errorf("what arrived on stdin is not a view: %w", err)
		}
		return view, nil
	}

	sessions, err := me.Read()
	if err != nil {
		return session.View{}, err
	}
	// No changes to name: a view read straight out of the store is the world
	// arriving, and nothing here was shown it before.
	return session.View{Sessions: sessions}, nil
}

// WhatToWakeFor is what this display asks the session-watcher to wake it for.
//
// What a pane title can actually show, and nothing else. The message, the
// sequence and what the agent last said change many times in a turn and change
// nothing on a title bar (R23). `captured_context` is on the list and is the
// one nobody would guess: it is where the pane this session lives in is
// recorded, so a session that moves has to repaint somewhere new.
//
// It is a function so a test can read it, and the test is the point: it moves
// one record field at a time, plans twice, and fails when a different set of
// renames comes out for something not named here (D-73). A list kept by hand
// drifts from the render the first time somebody adds to a title — which is
// how sketchybar came to draw the agent's message without asking to be told
// when it changed (D-72).
func WhatToWakeFor() []string {
	return []string{"kernel", "detail", "rank", "name", "cwd", "captured_context"}
}

// Display is the render function and what it needs.
type Display struct {
	Zellij Zellij
	Glyphs session.Palette
	Logger *slog.Logger
}

// Render is called with the current state, never with a transition, so it has
// nothing to remember and nothing to get out of step with (R22).
func (d *Display) Render(view session.View) error {
	for zellijSession, records := range Group(view.Sessions) {
		panes, err := d.Zellij.Panes(zellijSession)
		if err != nil {
			// One zellij session being gone says nothing about the others, and
			// nothing at all about the agents (R13). The read failing is also
			// what guarantees no rename is attempted against it.
			d.Logger.Debug("skipped a zellij session", "session", zellijSession, "why", err.Error())
			continue
		}
		for _, command := range Plan(zellijSession, records, panes, d.Glyphs) {
			if err := d.Zellij.Do(command); err != nil {
				d.Logger.Warn("zellij refused", "session", zellijSession, "why", command.Why,
					"problem", err.Error())
				continue
			}
			d.Logger.Debug("painted", "session", zellijSession, "what", command.Why)
		}
	}
	return nil
}
