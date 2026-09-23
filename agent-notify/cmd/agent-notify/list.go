package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	agentnotify "github.com/lassoColombo/agent-notify"
	"github.com/lassoColombo/agent-notify/internal/core"
)

// list is the cold read path: what is running, with no session-watcher and no
// socket anywhere (§A7.6).
//
// It applies the liveness decision as it reads, because a session whose process
// was killed must not be shown as working merely because nothing has got round
// to filing it. It applies it for *display only* and writes nothing: filing an
// ended session is a transition, and transitions belong to the session-watcher
// (M8).
func listCommand() *cobra.Command {
	var asJSON, all bool
	command := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "what is running, most urgent first",
		Long: `Reads the store directly: no session-watcher, no socket, nothing to be
connected to. A session whose process is gone is shown as ended even if nothing
has got round to filing it, and nothing is written — a command a statusline
polls three times a second has no business writing.`,
		GroupID: groupForLookingAtWhatIsHappening,
		Args:    cobra.NoArgs,
		Run: func(command *cobra.Command, arguments []string) {
			endTheProcessWith(list(asJSON, all))
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
	printOneLinePerSession(records, time.Now().UTC())
	return 0
}

// printOneLinePerSession is the human view of what `--json` hands a display:
// most urgent first, one row each.
func printOneLinePerSession(records []agentnotify.Record, now time.Time) {
	if len(records) == 0 {
		fmt.Println("no sessions")
		return
	}

	// Both columns are measured rather than guessed. A fixed width that the
	// longest value overflows does not truncate it — it pushes every column
	// after it out of line, which is worse than either.
	names, states := widestValueInTheColumn(records, 24, agentnotify.Record.DisplayName),
		widestValueInTheColumn(records, 34, agentnotify.Record.State)

	for _, record := range records {
		row := fmt.Sprintf("%-3d %-*s  %-*s  %-7s",
			record.Rank,
			names, cutToWidthWithAnEllipsis(record.DisplayName(), names),
			states, cutToWidthWithAnEllipsis(record.State(), states),
			howLongItHasBeenInThisState(record.StateSince, now))
		if message := oneLine(record.Message); message != "" {
			row += "  " + cutToWidthWithAnEllipsis(message, 60)
		}
		fmt.Println(strings.TrimRight(row, " "))
	}
}

// howLongItHasBeenInThisState is rendering rather than state. A display may
// show elapsed time however it likes; the state itself never moves because the
// clock moved (D-12).
func howLongItHasBeenInThisState(at, now time.Time) string {
	if at.IsZero() {
		return ""
	}
	elapsed := now.Sub(at)
	switch {
	case elapsed < time.Minute:
		return fmt.Sprintf("%ds", int(elapsed.Seconds()))
	case elapsed < time.Hour:
		return fmt.Sprintf("%dm", int(elapsed.Minutes()))
	case elapsed < 24*time.Hour:
		return fmt.Sprintf("%dh%dm", int(elapsed.Hours()), int(elapsed.Minutes())%60)
	default:
		return fmt.Sprintf("%dd", int(elapsed.Hours())/24)
	}
}

// oneLine flattens for a row. Core kept the newlines because a preview pane
// wants them; flattening is rendering, and this is a renderer (R24).
func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// widestValueInTheColumn is capped, so that one enormous name cannot push the
// message off the screen.
func widestValueInTheColumn(
	records []agentnotify.Record, most int, of func(agentnotify.Record) string,
) int {
	widest := 0
	for _, record := range records {
		if size := len([]rune(of(record))); size > widest {
			widest = size
		}
	}
	return min(widest, most)
}

func cutToWidthWithAnEllipsis(text string, width int) string {
	if len([]rune(text)) <= width {
		return text
	}
	return string([]rune(text)[:width-1]) + "…"
}
