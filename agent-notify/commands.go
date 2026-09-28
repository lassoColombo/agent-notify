package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/lassoColombo/agent-notify/command/annotate"
	"github.com/lassoColombo/agent-notify/command/doctor"
	"github.com/lassoColombo/agent-notify/command/focus"
	"github.com/lassoColombo/agent-notify/command/install"
	"github.com/lassoColombo/agent-notify/command/list"
	"github.com/lassoColombo/agent-notify/command/reportevent"
	"github.com/lassoColombo/agent-notify/command/tail"
	"github.com/lassoColombo/agent-notify/command/watcher"
	"github.com/lassoColombo/agent-notify/session"
)

// The command line, as a tree.
//
// It is cobra rather than a `switch` over `os.Args[1]`, and the trade is worth
// stating: three dependencies where there were two, against a great deal of
// code that is no longer written here. Gone with the switch are a hand-kept
// usage string that had to be edited whenever a flag changed, a `partition`
// helper that existed only so a flag after a positional argument would still
// be a flag, and the per-command `FlagSet` boilerplate. What arrives with it is
// `--help` on every command, "did you mean" on a typo, and completion.
//
// **Completion is the point, not a side effect.** `agent-notify focus-session
// <TAB>` offers the sessions that are actually running, most urgent first, each
// annotated with what it is doing — which is the thing this program is FOR, and
// it would be a hundred lines of shell per shell to write by hand.
//
// Each command is a package under [command], and this file is the only thing
// that imports all of them. A command declares what it does; where it appears
// in the help is decided here, because the four headings are a claim about who
// is reading rather than a property of any one command — and because the
// grouping is only legible if you can see it all at once.

const (
	groupForLookingAtWhatIsHappening = "looking"
	groupForDoingSomethingAboutIt    = "doing"
	groupForSettingItUp              = "setting-up"
	groupForWhatAnAgentCalls         = "agents"
)

func theWholeCommandTree() *cobra.Command {
	command := &cobra.Command{
		Use:   "agent-notify",
		Short: "the semaphore over your coding agents",
		Long: `agent-notify keeps one answer up to date — which of your agents wants you —
and hands it to whatever displays it: a menu bar, a pane title, a notification.

This command is the client half of the same library an integration links, and
never a second implementation of anything.`,
		Version:       session.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		// A bare `agent-notify` says what there is rather than failing, because
		// the first thing anybody types is the name of the program.
		RunE: func(command *cobra.Command, arguments []string) error {
			return command.Help()
		},
	}
	command.SetVersionTemplate("{{.Version}}\n")

	command.AddGroup(
		&cobra.Group{ID: groupForLookingAtWhatIsHappening, Title: "Looking at what is happening:"},
		&cobra.Group{ID: groupForDoingSomethingAboutIt, Title: "Doing something about it:"},
		&cobra.Group{ID: groupForSettingItUp, Title: "Setting it up, and keeping it running:"},
		&cobra.Group{ID: groupForWhatAnAgentCalls, Title: "What an agent-integration calls:"},
	)

	fileUnder(command, groupForLookingAtWhatIsHappening,
		versionCommand(), list.Command(), tail.Command(), focus.Focused(), doctor.Command())
	fileUnder(command, groupForDoingSomethingAboutIt,
		focus.FocusSession(), annotate.Command())
	fileUnder(command, groupForSettingItUp,
		install.Command(), watcher.Command())
	fileUnder(command, groupForWhatAnAgentCalls,
		reportevent.Command())

	return command
}

// fileUnder adds commands to the tree under one heading.
//
// A command with no group is filed by cobra under "Additional Commands", which
// is where a command goes to be missed — so the heading is not optional and
// there is a test that says so.
func fileUnder(tree *cobra.Command, group string, commands ...*cobra.Command) {
	for _, command := range commands {
		command.GroupID = group
		tree.AddCommand(command)
	}
}

// versionCommand is `version` as a command as well as a flag, because it was a
// command before the tree existed and something out there types it.
func versionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "what every integration handshakes against",
		Args:  cobra.NoArgs,
		Run: func(*cobra.Command, []string) {
			fmt.Println(session.Version)
		},
	}
}
