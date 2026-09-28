package main

import (
	"encoding/json"
	"os"
	"sync"

	"github.com/lassoColombo/agent-notify/container"
	"github.com/lassoColombo/agent-notify/logs"
	"github.com/lassoColombo/agent-notify/session"
	"github.com/lassoColombo/agent-notify/subscribe"
)

func main() {
	// Resolved on first use, so `capture-environment` — on the path the agent
	// waits on — never reads the config or stats zellij.
	resolved := sync.OnceValues(func() (Resolved, error) { return Read(me) })

	os.Exit(subscribe.Main(me, subscribe.Commands{
		Named: map[string]func([]string) int{"install": install},
		Render: func(view session.View) error {
			settings, err := resolved()
			if err != nil {
				return err
			}
			log, closeLog := logs.Open(Name)
			defer closeLog.Close()
			return (&Display{Zellij: settings.Zellij, Glyphs: settings.Glyphs, Logger: log}).Render(view)
		},
		Interpret: func(captured json.RawMessage) (any, error) {
			settings, err := resolved()
			if err != nil {
				return nil, err
			}
			return Interpret(settings.Zellij, captured)
		},
		Focus: func(coordinates json.RawMessage) (container.Outcome, error) {
			settings, err := resolved()
			if err != nil {
				return container.Failed(container.NotRunning, err.Error()), nil
			}
			return Focus(settings.Zellij, coordinates)
		},
		Focused: func(coordinates json.RawMessage) (container.Verdict, error) {
			settings, err := resolved()
			if err != nil {
				return container.Verdict{Answer: container.CannotTell, Detail: err.Error()}, nil
			}
			return Focused(settings.Zellij, coordinates)
		},
	}, os.Args[1:]))
}
