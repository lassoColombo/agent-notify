// Package install sets integrations up, by handing the job to each of them.
//
// Core does not know where Claude keeps its settings, what its hooks are
// called, or which environment variables zellij puts in a pane, and must not
// (R10). What it knows is the naming convention and where its own binary is,
// so it files the second and runs `agent-notify-<name> install` for the rest.
package install

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/lassoColombo/agent-notify/command/internal/onpath"
	"github.com/lassoColombo/agent-notify/internal/config"
	"github.com/lassoColombo/agent-notify/internal/paths"
)

// Command is `agent-notify install`.
func Command() *cobra.Command {
	return &cobra.Command{
		Use:   "install [integration ...] [options]",
		Short: "set up every integration on your PATH, or the ones named",
		Long: `Files where agent-notify itself is, then runs agent-notify-<name> install for
every agent-notify-* program on your PATH, or for the ones named. Each writes
what only it knows into its own file under conf.d; your config.toml is never
touched. Options after one name belong to that program.

  agent-notify install
  agent-notify install claude --print
  agent-notify install macos-bar --sign "agent-notify self-signed"

Press TAB for the integrations that are on your PATH.`,
		DisableFlagParsing: true,
		ValidArgsFunction:  onpath.CompleteThem,
		RunE: func(command *cobra.Command, arguments []string) error {
			if len(arguments) > 0 && (arguments[0] == "--help" || arguments[0] == "-h") {
				return command.Help()
			}
			if code := run("install", arguments, true); code != 0 {
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
		Short: "take every integration on your PATH down, or the ones named",
		Long: `Runs agent-notify-<name> uninstall for every agent-notify-* program on your
PATH, or for the ones named, then removes what agent-notify filed about
itself. Each takes back what its install wrote and nothing else.`,
		DisableFlagParsing: true,
		ValidArgsFunction:  onpath.CompleteThem,
		RunE: func(command *cobra.Command, arguments []string) error {
			if len(arguments) > 0 && (arguments[0] == "--help" || arguments[0] == "-h") {
				return command.Help()
			}
			if code := run("uninstall", arguments, false); code != 0 {
				os.Exit(code)
			}
			return nil
		},
	}
}

// run files core's own drop-in, then hands the verb to each integration in
// turn as a child that owns the terminal. With one name, everything after it
// is that program's; with none, every program on PATH is run with nothing.
func run(verb string, arguments []string, installing bool) int {
	layout, err := paths.FromEnvironment()
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify %s: %v\n", verb, err)
		return 1
	}
	if installing {
		if code := fileWhereCoreIs(layout); code != 0 {
			return code
		}
	}

	names, rest := arguments, []string(nil)
	if len(arguments) > 1 && arguments[1][0] == '-' {
		names, rest = arguments[:1], arguments[1:]
	}
	if len(names) == 0 {
		names = onpath.Integrations()
	}

	code := 0
	for _, name := range names {
		if one := handOver(name, verb, rest); one != 0 {
			code = one
		}
	}
	if !installing {
		if err := config.RemoveDropIn(layout, "agent-notify"); err != nil {
			fmt.Fprintf(os.Stderr, "agent-notify uninstall: %v\n", err)
			return 1
		}
		fmt.Printf("removed %s\n", layout.DropIn("agent-notify"))
	}
	return code
}

// fileWhereCoreIs writes `agent-notify-binary` into core's own drop-in, so
// that a hook and a launchd child find this program without a PATH (D-85).
func fileWhereCoreIs(layout paths.Layout) int {
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify install: cannot find my own path: %v\n", err)
		return 1
	}
	written, err := config.WriteDropIn(layout, "agent-notify",
		fmt.Sprintf("# Written by `agent-notify install`: where agent-notify is, for a hook\n"+
			"# and a launchd child, whose PATH is not yours.\nagent-notify-binary = %q\n", self))
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify install: %v\n", err)
		return 1
	}
	fmt.Printf("wrote %s\n", written)
	return 0
}

func handOver(name, verb string, arguments []string) int {
	program := "agent-notify-" + name
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
