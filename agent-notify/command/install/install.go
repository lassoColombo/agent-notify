// Package install sets the agents up, by handing the job to each of them.
//
// Core does not know where Claude keeps its settings or what its hooks are
// called, and must not (R10). What it knows is the naming convention, so it
// asks every agent-notify-<name> on PATH what it answers and runs `install`
// on the ones that answer it: the agent-integrations, which write the hook
// into their agent's file. Nothing here writes any configuration (D-101).
package install

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"slices"

	"github.com/spf13/cobra"

	"github.com/lassoColombo/agent-notify/internal/config"
	"github.com/lassoColombo/agent-notify/internal/handshake"
	"github.com/lassoColombo/agent-notify/internal/onpath"
	"github.com/lassoColombo/agent-notify/session"
	"github.com/lassoColombo/agent-notify/tool"
)

// Command is `agent-notify install`.
func Command() *cobra.Command {
	return &cobra.Command{
		Use:   "install [integration ...] [options]",
		Short: "wire every agent on your PATH up, or the ones named",
		Long: `Runs agent-notify-<name> install for every agent-notify-* program on your PATH
that has one, or for the ones named. An agent-integration writes the hook into
its agent's own settings; nothing writes agent-notify's configuration, which is
yours. A tool-integration has nothing to install: it is on PATH, so it runs.
Options after one name belong to that program.

  agent-notify install
  agent-notify install claude --print

Press TAB for the integrations that are on your PATH.`,
		DisableFlagParsing: true,
		ValidArgsFunction:  completeThem,
		RunE: func(command *cobra.Command, arguments []string) error {
			if len(arguments) > 0 && (arguments[0] == "--help" || arguments[0] == "-h") {
				return command.Help()
			}
			if code := run("install", arguments); code != 0 {
				os.Exit(code)
			}
			return nil
		},
	}
}

// Uninstall is `agent-notify uninstall`.
func Uninstall() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall [integration ...] [options]",
		Short: "take every agent on your PATH out of its hooks, or the ones named",
		Long: `Runs agent-notify-<name> uninstall for every agent-notify-* program on your
PATH that has one, or for the ones named. Each takes back what its install
wrote and nothing else.`,
		DisableFlagParsing: true,
		ValidArgsFunction:  completeThem,
		RunE: func(command *cobra.Command, arguments []string) error {
			if len(arguments) > 0 && (arguments[0] == "--help" || arguments[0] == "-h") {
				return command.Help()
			}
			if code := run("uninstall", arguments); code != 0 {
				os.Exit(code)
			}
			return nil
		},
	}
}

// run hands the verb to each program in turn as a child that owns the
// terminal. With one name, everything after it is that program's and the
// program is run whether or not it says it answers; with none, every program
// on PATH that answers the verb is run with nothing.
func run(verb string, arguments []string) int {
	names, rest := arguments, []string(nil)
	if len(arguments) > 1 && arguments[1][0] == '-' {
		names, rest = arguments[:1], arguments[1:]
	}
	if len(names) == 0 {
		for _, name := range onpath.Integrations() {
			if answers(name, verb) {
				names = append(names, name)
			}
		}
		if len(names) == 0 {
			fmt.Printf("nothing on PATH answers `%s`; a tool-integration is set up by its table alone\n", verb)
			return 0
		}
	}

	code := 0
	for _, name := range names {
		if one := handOver(name, verb, rest); one != 0 {
			code = one
		}
	}
	return code
}

// completeThem offers the programs that are actually there, which is the same
// question `install` asks a moment later.
func completeThem(
	command *cobra.Command, arguments []string, whatHasBeenTypedSoFar string,
) ([]string, cobra.ShellCompDirective) {
	if len(arguments) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return onpath.Integrations(), cobra.ShellCompDirectiveNoFileComp
}

// answers is whether the program lists the verb among its methods. One that
// cannot be asked does not.
func answers(name, verb string) bool {
	raw, err := tool.Ask(config.Program(name), session.CapabilitiesCommand, nil, handshake.Timeout)
	if err != nil {
		return false
	}
	var answered session.Capabilities
	return json.Unmarshal(raw, &answered) == nil && slices.Contains(answered.Methods, verb)
}

func handOver(name, verb string, arguments []string) int {
	program := config.Program(name)
	found, err := exec.LookPath(program)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify %s: cannot find %s on PATH.\n"+
			"An integration is its own program; build it first.\n", verb, program)
		return 1
	}
	fmt.Printf("== %s %s\n", program, verb)
	child := exec.Command(found, append([]string{verb}, arguments...)...)
	child.Stdin, child.Stdout, child.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := child.Run(); err != nil {
		if exited, isExit := err.(*exec.ExitError); isExit {
			return exited.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "agent-notify %s: cannot run %s: %v\n", verb, found, err)
		return 1
	}
	return 0
}
