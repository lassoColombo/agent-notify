// Package list is the cold read path: what is running, with no session-watcher
// and no socket anywhere (§A7.6).
//
// It applies the liveness decision as it reads, because a session whose process
// was killed must not be shown as working merely because nothing has got round
// to filing it. It applies it for *display only* and writes nothing: filing an
// ended session is a transition, and transitions belong to the session-watcher
// (M8).
//
// What it prints is [command/internal/rows], shared with `tail` — a person
// comparing the two is comparing them column by column, so they cannot be two
// renderers (R24).
package list

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/lassoColombo/agent-notify/command/internal/exit"
	"github.com/lassoColombo/agent-notify/command/internal/rows"
	"github.com/lassoColombo/agent-notify/internal/core"
)

// Command is `agent-notify list`.
func Command() *cobra.Command {
	var asJSON, all bool
	command := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "what is running, most urgent first",
		Long: `Reads the store directly: no session-watcher, no socket, nothing to be
connected to. A session whose process is gone is shown as ended even if nothing
has got round to filing it, and nothing is written — a command a statusline
polls three times a second has no business writing.`,
		Args: cobra.NoArgs,
		Run: func(command *cobra.Command, arguments []string) {
			exit.TheProcessWith(list(asJSON, all))
		},
	}
	command.Flags().BoolVar(&asJSON, "json", false,
		"the records themselves, for scripts and displays")
	command.Flags().BoolVar(&all, "all", false,
		"include sessions that have ended and are still resumable")
	return command
}

func list(asJSON, all bool) int {
	openedCore, err := core.OpenEverythingACommandNeeds("list")
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify list: %v\n", err)
		return 1
	}
	defer openedCore.Close()

	records := openedCore.WhatIsRunning(all)

	if asJSON {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(records); err != nil {
			fmt.Fprintf(os.Stderr, "agent-notify list: %v\n", err)
			return 1
		}
		return 0
	}
	rows.OneLinePerSession(records, time.Now().UTC())
	return 0
}
