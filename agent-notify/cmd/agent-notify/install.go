package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/spf13/cobra"
)

// install sets an integration up, by handing the whole job to that integration.
//
// Core does not know where Claude keeps its settings, what its hooks are
// called, or which environment variables zellij puts in a pane — and must not
// (R10). What it knows is the naming convention, so this is a dispatcher and
// nothing more: one command for a person to remember, and all the tool
// knowledge on the other side of an exec. It works for both families for the
// same reason it is this short — it never learns which kind it just ran.
//
// It replaces this process rather than waiting on a child, so that the
// integration owns the terminal, the exit code, and anything it wants to ask.
func installCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "install <integration> [options]",
		Short: "hand over to that integration to set itself up",
		Long: `Core does not know where Claude keeps its settings, what its hooks are
called, or which environment variables zellij puts in a pane — and must not. So
this is a dispatcher and nothing more: it execs agent-notify-<integration>
install, and every option after the name belongs to that program.

  agent-notify install claude --print
  agent-notify install macos-bar

Press TAB for the integrations that are on your PATH.`,
		GroupID: groupForSettingItUp,
		Args:    cobra.MinimumNArgs(1),
		// The options after the name are the integration's, not ours: parsing
		// them here would mean core had an opinion about a tool it must not
		// know anything about.
		DisableFlagParsing: true,
		ValidArgsFunction:  completeTheIntegrationsOnPath,
		RunE: func(command *cobra.Command, arguments []string) error {
			// Not parsing the flags means not answering --help either, and
			// there is no reason for this command to have its own idea of what
			// its help looks like.
			if arguments[0] == "--help" || arguments[0] == "-h" {
				return command.Help()
			}
			endTheProcessWith(install(arguments))
			return nil
		},
	}
}

func install(arguments []string) int {
	tool := arguments[0]
	program := "agent-notify-" + tool
	found, err := exec.LookPath(program)
	if err != nil {
		fmt.Fprintf(os.Stderr,
			"agent-notify install: cannot find %s on PATH.\n"+
				"An integration is its own program, in its own repository; install it first.\n", program)
		return 1
	}

	command := append([]string{program, "install"}, arguments[1:]...)
	if err := syscall.Exec(found, command, os.Environ()); err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify install: cannot run %s: %v\n", found, err)
		return 1
	}
	return 0
}
