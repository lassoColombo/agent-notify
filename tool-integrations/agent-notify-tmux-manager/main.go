// Command agent-notify-tmux-manager is a tab of tmux given over to agents:
// every agent in this tmux server down the left, grouped by what it wants from
// you, and on the right the selected one's own pane, borrowed from the window
// it lives in for as long as the tab is on screen (D-125).
//
//	agent-notify-tmux-manager open [client]   show the tab, making it if there is none; on the tab, go back
//	agent-notify-tmux-manager close           send every agent home and close the tab
//	agent-notify-tmux-manager restore         send every agent home
//
// and two that the tab runs in its own panes, never a person:
//
//	agent-notify-tmux-manager list            the list, which decides what the stage shows
//	agent-notify-tmux-manager stand-in <sock> the pane an agent is swapped with
//
// What it knows is written in tmux, as options on the panes: the list never
// acts on a memory of what it did, and anything left half done is finished by
// whoever looks next. It is run from a key and never by core.
package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/lassoColombo/agent-notify/subscribe"
)

func main() {
	os.Exit(subscribe.Main(me, subscribe.Commands{
		Named: map[string]func([]string) int{
			"open":     func(arguments []string) int { return fail(open(arguments)) },
			"close":    func([]string) int { return fail(outside(Close)) },
			"restore":  func([]string) int { return fail(outside(sendHome)) },
			"list":     func([]string) int { return fail(runList()) },
			"stand-in": func(arguments []string) int { return fail(runStandIn(arguments)) },
		},
		Default: func(arguments []string) int {
			fmt.Fprintf(os.Stderr, "%s takes one of: open, close, restore\n", Name)
			return 1
		},
	}, os.Args[1:]))
}

func fail(err error) int {
	if err == nil {
		return 0
	}
	fmt.Fprintf(os.Stderr, "%s: %v\n", Name, err)
	return 1
}

// server is the tmux server a command given from a key or a shell is about:
// the one $TMUX names, which tmux sets for `run-shell` too.
func server() (Tmux, error) {
	settings, err := Read()
	if err != nil {
		return Tmux{}, err
	}
	socket, found := SocketOf(os.Getenv("TMUX"))
	if !found {
		return Tmux{}, errors.New("run this from inside tmux: $TMUX does not name a server")
	}
	return Tmux{Binary: settings.Binary, Socket: socket}, nil
}

func open(arguments []string) error {
	tmux, err := server()
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	client := ""
	if len(arguments) > 0 {
		client = arguments[0]
	}
	return Open(tmux, client, []string{executable, "list"})
}

// outside runs what is asked of the tab from outside it.
func outside(act func(Tmux, *slog.Logger) error) error {
	tmux, err := server()
	if err != nil {
		return err
	}
	return act(tmux, me.Logger())
}

func sendHome(tmux Tmux, logger *slog.Logger) error {
	_, err := SendHome(tmux, logger)
	return err
}
