// Command agent-notify-zellij-display paints agent state onto zellij's pane and tab
// titles: a glyph for what the session is doing, then what the session is
// called, with the tab wearing the glyph of the most urgent thing inside it.
//
// It is a tool-integration: a long-lived process that connects to the
// session-watcher once, declares what it is, and renders whatever it is handed
// (§A10.2). Nothing it does can slow an agent down, and nothing it fails to do
// costs anybody else anything (R13).
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	agentnotify "github.com/lassoColombo/agent-notify"
	"github.com/lassoColombo/agent-notify/capture"
	"github.com/lassoColombo/agent-notify/subscribe"
)

func main() {
	arguments := os.Args[1:]
	if len(arguments) > 0 {
		switch arguments[0] {
		case "install":
			os.Exit(install(arguments[1:]))
		case capture.Command:
			// Dispatched here, before anything is read or resolved: this one
			// runs as a child of the hook, on the path the agent waits on, and
			// it needs nothing but the environment it was started in (R1).
			//
			// It is the capture package and not the container SDK, because this
			// is a display and capture belongs to no role (D-59).
			os.Exit(capture.Main(Name, Capture))
		case "repaint":
			os.Exit(repaint())
		case "-h", "--help", "help":
			usage(os.Stdout)
			os.Exit(0)
		default:
			fmt.Fprintf(os.Stderr, "agent-notify-zellij-display: %q is not a command\n\n", arguments[0])
			usage(os.Stderr)
			os.Exit(1)
		}
	}
	os.Exit(paint())
}

func usage(to *os.File) {
	fmt.Fprint(to, `agent-notify-zellij-display — agent state on zellij pane and tab titles

  agent-notify-zellij-display              stay connected and paint (the usual way to run it)
  agent-notify-zellij-display repaint      paint once from the store, with no session-watcher
  agent-notify-zellij-display capture-environment
                                           what pane this process is in; run by the hook, not by you
  agent-notify-zellij-display install      add [integration.zellij-display] to agent-notify's config
  agent-notify-zellij-display install --print   show what it would add, and change nothing
`)
}

// logger writes to stderr, which is where a supervised child's diagnostics
// belong: the session-watcher that starts one captures it, and a person running
// it by hand simply sees it.
func logger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

// paint is the whole program: about ten lines of it are the display and the
// rest is saying so out loud.
func paint() int {
	log := logger()
	settings, err := Read(me)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	display := &Display{Zellij: Zellij{Binary: settings.Binary, Timeout: ZellijTimeout},
		Glyphs: settings.Glyphs, Logger: log}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Info("painting", "zellij", settings.Binary, "timeout", ZellijTimeout.String())
	if err := subscribe.Run(ctx, subscribe.Integration{
		Name:  Name,
		Roles: []string{"display"},

		WakeOn: WhatToWakeFor(),

		// Ended sessions, for a display that will never draw one. It owns a
		// piece of a UI it did not create, and the record is the only thing
		// that remembers which piece, so this is how it learns to give a pane
		// back rather than leaving a dead agent's glyph on a live shell.
		WantEnded: true,

		OnChange: display.Render,
		Logger:   log,
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// repaint paints once, straight from the store, with no session-watcher and no
// socket. It is the cold path (§A7.6): what `zellij` itself would want after a
// restart, and what a person wants when asking "why does that say that".
//
// It cannot give a pane back, because the cold read returns live sessions only
// and giving one back needs the ended record that remembers the pane.
func repaint() int {
	log := logger()
	settings, err := Read(me)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	sessions, err := me.Read()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	display := &Display{Zellij: Zellij{Binary: settings.Binary, Timeout: ZellijTimeout},
		Glyphs: settings.Glyphs, Logger: log}
	_ = display.Render(subscribe.View{Sessions: sessions, Why: "repaint"})
	return 0
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
	Glyphs agentnotify.Palette
	Logger *slog.Logger
}

// Render is called with the current state, never with a transition, so it has
// nothing to remember and nothing to get out of step with (R22).
func (d *Display) Render(view subscribe.View) error {
	for session, records := range Group(view.Sessions) {
		panes, err := d.Zellij.Panes(session)
		if err != nil {
			// One zellij session being gone says nothing about the others, and
			// nothing at all about the agents (R13). The read failing is also
			// what guarantees no rename is attempted against it.
			d.Logger.Debug("skipped a zellij session", "session", session, "why", err.Error())
			continue
		}
		for _, command := range Plan(session, records, panes, d.Glyphs) {
			if err := d.Zellij.Do(command); err != nil {
				d.Logger.Warn("zellij refused", "session", session, "why", command.Why,
					"problem", err.Error())
				continue
			}
			d.Logger.Debug("painted", "session", session, "what", command.Why)
		}
	}
	return nil
}
