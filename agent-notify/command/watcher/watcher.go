// Package watcher is `agent-notify watcher …`: run, start, stop, restart,
// reload and status for the one long-lived process.
//
// It is the control surface and not the process. What the session-watcher
// actually does is [internal/sessionwatcher], which has that name rather than
// `watcher` precisely so that these two can both be called what they are.
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

// Command is `agent-notify watcher` and its six subcommands.
func Command() *cobra.Command {
	command := &cobra.Command{
		Use:   "watcher",
		Short: "the one long-lived process",
		Long: `It watches for agents dying and sweeps up behind them, and it is what every
display connects to. One per machine; starting a second is refused by the first.`,
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
				// Exit 3 is "there was nothing running", which is a fine state
				// to restart from and not a reason to stop.
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
		&cobra.Command{
			Use: "status", Short: "is it running, since when, and does its socket answer", Args: cobra.NoArgs,
			Run: func(*cobra.Command, []string) { exit.TheProcessWith(watcherStatus()) },
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
	// path resolution reads. Without them a detached session-watcher can end up
	// watching a different directory than its clients write to, and nothing
	// errors — it simply never sees anything (§A9.2).
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
			// Losing the race is the ordinary outcome, not a failure: anyone
			// may start one, and the lock is what makes that harmless.
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
	// Spawning is asynchronous; say so only once it has actually taken the lock.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if sessionwatcher.Running(layout) {
			return watcherStatus()
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

// watcherReload sends SIGHUP, which is how the configuration is re-read.
//
// There is no file watching, deliberately: picking up a half-saved file
// mid-write is a real failure and not a theoretical one (§A14). Asking is also
// how a person retries — a container that was broken, or misspelled, or not yet
// installed gets another go rather than being written off for the life of the
// process.
func watcherReload() int {
	layout, err := paths.FromEnvironment()
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify watcher: %v\n", err)
		return 1
	}
	held, err := sessionwatcher.WhoHolds(layout)
	if err != nil || held.PID == 0 {
		fmt.Println("not running")
		return 3
	}
	if err := syscall.Kill(held.PID, syscall.SIGHUP); err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify watcher: cannot signal pid %d: %v\n", held.PID, err)
		return 1
	}
	fmt.Printf("asked pid %d to re-read %s\n", held.PID, layout.ConfigFile)
	fmt.Println("Values that cannot change under a running process — the socket paths, the")
	fmt.Println("state directory — keep what they had; everything else takes effect now.")
	return 0
}

func watcherStatus() int {
	layout, err := paths.FromEnvironment()
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify watcher: %v\n", err)
		return 1
	}
	if !sessionwatcher.Running(layout) {
		fmt.Println("not running")
		return 3
	}
	held, err := sessionwatcher.WhoHolds(layout)
	if err != nil {
		fmt.Printf("running, but the lock file says nothing: %v\n", err)
		return 0
	}
	fmt.Printf("running: pid %d, version %s, since %s\n",
		held.PID, held.Version, held.Since.Format(time.RFC3339))
	return 0
}
