// Package watcher is `agent-notify watcher …`: the control surface for the one
// long-lived process, which is [internal/sessionwatcher].
package watcher

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/lassoColombo/agent-notify/command/internal/exit"
	"github.com/lassoColombo/agent-notify/internal/paths"
	"github.com/lassoColombo/agent-notify/internal/sessionwatcher"
)

func Command() *cobra.Command {
	command := &cobra.Command{
		Use:   "watcher",
		Short: "the one long-lived process",
		Long: `It watches for agents dying, sweeps up behind them, and runs the displays.
One per machine; starting a second is refused by the first. doctor says
whether it is running.`,
	}

	var foreground bool
	var state, runtime, configFile string
	run := &cobra.Command{
		Use:   "run",
		Short: "be the watcher, here, in this process",
		Long: `Normally started for you. Run it yourself to watch what it does, or to point
it at directories that are not the ones it would resolve.`,
		Args: cobra.NoArgs,
		Run: func(command *cobra.Command, arguments []string) {
			exit.TheProcessWith(watcherRun(foreground, state, runtime, configFile))
		},
	}
	run.Flags().BoolVar(&foreground, "foreground", false, "stay attached and log to the terminal")
	run.Flags().StringVar(&state, "state", "", "the state directory, when not resolved from the environment")
	run.Flags().StringVar(&runtime, "runtime", "", "the runtime directory")
	run.Flags().StringVar(&configFile, "config", "", "the configuration file")

	command.AddCommand(
		run,
		&cobra.Command{
			Use: "start", Short: "start it if it is not already running", Args: cobra.NoArgs,
			Run: func(*cobra.Command, []string) { exit.TheProcessWith(watcherStart()) },
		},
		&cobra.Command{
			Use: "stop", Short: "ask it to stop, and never kill it", Args: cobra.NoArgs,
			Run: func(*cobra.Command, []string) { exit.TheProcessWith(watcherStop()) },
		},
		&cobra.Command{
			Use: "restart", Short: "stop it, then start it", Args: cobra.NoArgs,
			Run: func(*cobra.Command, []string) {
				// Exit 3 is "there was nothing running", which is fine to
				// restart from.
				if code := watcherStop(); code != 0 && code != 3 {
					exit.TheProcessWith(code)
					return
				}
				exit.TheProcessWith(watcherStart())
			},
		},
		&cobra.Command{
			Use: "reload", Short: "re-read the config without restarting", Args: cobra.NoArgs,
			Run: func(*cobra.Command, []string) { exit.TheProcessWith(watcherReload()) },
		},
	)
	return command
}

func watcherRun(foreground bool, state, runtime, configFile string) int {
	layout, err := paths.FromEnvironment()
	if err != nil && state == "" {
		fmt.Fprintf(os.Stderr, "agent-notify watcher run: %v\n", err)
		return 1
	}
	// The spawner passes these because it also sanitised the environment that
	// path resolution reads (§A9.2).
	if state != "" {
		layout.State = state
	}
	if runtime != "" {
		layout.Runtime = runtime
	}
	if configFile != "" {
		layout.ConfigFile = configFile
	}

	running, err := sessionwatcher.Start(layout, foreground)
	if err != nil {
		if errors.Is(err, sessionwatcher.ErrAlreadyRunning) {
			// Losing the race is the ordinary outcome, not a failure.
			if foreground {
				fmt.Fprintf(os.Stderr, "agent-notify watcher run: %v\n", err)
			}
			return 0
		}
		fmt.Fprintf(os.Stderr, "agent-notify watcher run: %v\n", err)
		return 1
	}

	if err := running.Run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify watcher run: %v\n", err)
		return 1
	}
	return 0
}

func watcherStart() int {
	layout, err := paths.FromEnvironment()
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify watcher: %v\n", err)
		return 1
	}
	if err := layout.Create(); err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify watcher: %v\n", err)
		return 1
	}
	if sessionwatcher.Running(layout) {
		fmt.Println("already running")
		return 0
	}
	if err := sessionwatcher.Spawn(layout, ""); err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify watcher: %v\n", err)
		return 1
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if sessionwatcher.Running(layout) {
			if held, err := sessionwatcher.WhoHolds(layout); err == nil {
				fmt.Printf("started: pid %d\n", held.PID)
			}
			return 0
		}
		time.Sleep(20 * time.Millisecond)
	}
	fmt.Fprintln(os.Stderr, "agent-notify watcher: started something, but nothing took the lock")
	return 1
}

func watcherStop() int {
	layout, err := paths.FromEnvironment()
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify watcher: %v\n", err)
		return 1
	}
	if !sessionwatcher.Running(layout) {
		fmt.Println("not running")
		return 3
	}
	if err := sessionwatcher.Stop(layout, 5*time.Second); err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify watcher: %v\n", err)
		return 1
	}
	fmt.Println("stopped")
	return 0
}

// watcherReload sends SIGHUP. There is no file watching: picking up a
// half-saved file mid-write is a real failure (§A14). Asking is also how a
// person retries a container that was broken or misspelled.
func watcherReload() int {
	layout, err := paths.FromEnvironment()
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify watcher: %v\n", err)
		return 1
	}
	held, err := sessionwatcher.WhoHolds(layout)
	if err != nil || held.PID == 0 || !sessionwatcher.Running(layout) {
		fmt.Println("not running")
		return 3
	}
	if err := syscall.Kill(held.PID, syscall.SIGHUP); err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify watcher: cannot signal pid %d: %v\n", held.PID, err)
		return 1
	}
	fmt.Printf("asked pid %d to re-read %s\n", held.PID, layout.ConfigFile)
	return 0
}
