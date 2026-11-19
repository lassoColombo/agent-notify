// Command agent-notify-tmux-manager is a tab of tmux given over to agents, the
// way zellij's inbox is: every agent in this tmux server down the left, grouped
// by what it wants from you, and on the right a live mirror of the selected
// one's pane, which takes what you type. Alt Tab (tmux.conf) moves between the
// two.
//
// It is three commands of one program, because the tab is two panes:
//
//	agent-notify-tmux-manager        lay the tab out: this pane becomes the list
//	                                 and the view is split off beside it
//	agent-notify-tmux-manager list   the list, which says what is selected
//	agent-notify-tmux-manager view   the mirror of whatever the list selected
//
// The two talk through tmux and nothing else: the list writes the selected
// pane's id into the tab's @agents_selected option, and the view reads it on
// every frame. Nothing is moved: the agent's pane stays where it lives and
// the view only reads its screen and types into it, so closing the tab, or
// either half of it, can never take an agent with it.
//
// It is run from a keybinding and never by core.
package main

import (
	"fmt"
	"os"

	"github.com/lassoColombo/agent-notify/subscribe"
)

const Name = "tmux-manager"

// me is what core sees: a program that answers nothing core calls, which is
// what keeps core from ever running it as a display.
var me = subscribe.Integration{Name: Name}

// watching is what the list subscribes with: everything a row shows, and
// captured_context, which is where an agent's pane is.
var watching = subscribe.Integration{
	Name:   Name,
	WakeOn: []string{"kernel", "detail", "rank", "name", "cwd", "branch", "captured_context"},
}

func main() {
	os.Exit(subscribe.Main(me, subscribe.Commands{
		Named: map[string]func([]string) int{
			"list": func([]string) int { return fail(runList()) },
			"view": func([]string) int { return fail(runView()) },
		},
		Default: func(arguments []string) int {
			if len(arguments) > 0 {
				fmt.Fprintf(os.Stderr, "%s: %q is not a command; it takes list, view or nothing\n",
					Name, arguments[0])
				return 1
			}
			return fail(open())
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

// open lays the tab out from the pane it was started in, which becomes the
// list. The view is split off to its right, and both are labelled with the
// options tmux.conf's Alt Tab and Alt v read.
func open() error {
	t, list, err := here()
	if err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	view, err := t.run("split-window", "-h", "-d", "-l", "75%", "-t", list,
		"-P", "-F", "#{pane_id}", self, "view")
	if err != nil {
		return err
	}
	view = trimmed(view)
	if _, err := t.run(
		"set-option", "-w", "-t", list, optionList, list, ";",
		"set-option", "-w", "-t", list, optionView, view, ";",
		"set-option", "-p", "-t", list, optionRole, roleList, ";",
		"set-option", "-p", "-t", view, optionRole, roleView, ";",
		"set-option", "-p", "-t", list, "@name", "agents",
	); err != nil {
		return err
	}
	return runList()
}
