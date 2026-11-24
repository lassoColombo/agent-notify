package main

import (
	"encoding/json"
	"os"
	"sync"

	"github.com/lassoColombo/agent-notify/container"
	"github.com/lassoColombo/agent-notify/session"
	"github.com/lassoColombo/agent-notify/subscribe"
)

func main() {
	// Resolved on first use, so `capture-environment` — on the path the agent
	// waits on — never reads the config or stats tmux.
	resolved := sync.OnceValues(func() (Resolved, error) { return Read(me) })

	os.Exit(subscribe.Main(me, subscribe.Commands{
		Render: func(view session.View) error {
			settings, err := resolved()
			if err != nil {
				return err
			}
			return (&Display{Tmux: settings.Tmux, Glyphs: settings.Glyphs, Logger: me.Logger()}).Render(view)
		},
		// Interpretation asks tmux nothing, so it needs no settings.
		Interpret: func(captured json.RawMessage, _ []session.Ancestor) (any, error) {
			return Interpret(captured)
		},
		Focus: func(coordinates json.RawMessage, window *container.Window) (container.Outcome, error) {
			settings, err := resolved()
			if err != nil {
				return container.Failed(container.NotRunning, err.Error()), nil
			}
			return Focus(settings.Tmux, coordinates, window)
		},
		WindowsShowing: func(coordinates json.RawMessage) ([]container.Window, error) {
			settings, err := resolved()
			if err != nil {
				return nil, err
			}
			return WindowsShowing(settings.Tmux, coordinates)
		},
		Focused: func(coordinates json.RawMessage) (container.Verdict, error) {
			settings, err := resolved()
			if err != nil {
				return container.Verdict{Answer: container.CannotTell, Detail: err.Error()}, nil
			}
			return Focused(settings.Tmux, coordinates)
		},
	}, os.Args[1:]))
}
