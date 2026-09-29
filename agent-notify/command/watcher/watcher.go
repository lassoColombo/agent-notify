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

	"github.com/lassoColombo/agent-notify/internal/onewatcher"
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
	run := &cobra.Command{
		Use:   "run",
		Short: "be the watcher, here, in this process",
		Long:  `Normally started for you. Run it yourself to watch what it does.`,
		Args:  cobra.NoArgs,
		Run: func(command *cobra.Command, arguments []string) {
			exitWith(watcherRun(foreground))
		},
	}
	run.Flags().BoolVar(&foreground, "foreground", false, "stay attached and log to the terminal")

	command.AddCommand(
		run,
		&cobra.Command{
			Use: "start", Short: "start it if it is not already running", Args: cobra.NoArgs,
			Run: func(*cobra.Command, []string) { exitWith(watcherStart()) },
		},
		&cobra.Command{
			Use: "stop", Short: "ask it to stop, and never kill it", Args: cobra.NoArgs,
			Run: func(*cobra.Command, []string) { exitWith(watcherStop()) },
		},
		&cobra.Command{
			Use: "restart", Short: "stop it, then start it", Args: cobra.NoArgs,
			Run: func(*cobra.Command, []string) {
				// Exit 3 is "there was nothing running", which is fine to
				// restart from.
				if code := watcherStop(); code != 0 && code != 3 {
					exitWith(code)
					return
				}
				exitWith(watcherStart())
			},
		},
		&cobra.Command{
			Use: "reload", Short: "re-read the config without restarting", Args: cobra.NoArgs,
			Run: func(*cobra.Command, []string) { exitWith(watcherReload()) },
		},
	)
	return command
}

func exitWith(code int) {
	if code != 0 {
		os.Exit(code)
	}
}

func watcherRun(foreground bool) int {
	layout, err := paths.FromEnvironment()
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify watcher run: %v\n", err)
		return 1
	}
	running, err := sessionwatcher.Start(layout, foreground)
	if err != nil {
		if errors.Is(err, onewatcher.ErrAlreadyRunning) {
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
	if onewatcher.Running(layout) {
		fmt.Println("already running")
		return 0
	}
	if err := onewatcher.Spawn(layout, ""); err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify watcher: %v\n", err)
		return 1
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if onewatcher.Running(layout) {
			if held, err := onewatcher.WhoHolds(layout); err == nil {
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
	if !onewatcher.Running(layout) {
		fmt.Println("not running")
		return 3
	}
	if err := onewatcher.Stop(layout, 5*time.Second); err != nil {
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
	held, err := onewatcher.WhoHolds(layout)
	if err != nil || held.PID == 0 || !onewatcher.Running(layout) {
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
