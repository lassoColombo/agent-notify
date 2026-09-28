// Package tail watches sessions change, in a terminal, through the same loop a
// display that owns its process uses.
package tail

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/lassoColombo/agent-notify/command/internal/exit"
	"github.com/lassoColombo/agent-notify/command/internal/rows"
	"github.com/lassoColombo/agent-notify/session"
	"github.com/lassoColombo/agent-notify/subscribe"
)

func Command() *cobra.Command {
	var asJSON, wantEnded bool
	var wakeOn []string
	command := &cobra.Command{
		Use:   "tail",
		Short: "watch sessions change, live",
		Long: `Watches the store as a display would and prints every view it is handed,
which is the way to find out what a display is being told before blaming the
display. With --json it prints the whole view as one object per line.`,
		Args: cobra.NoArgs,
		Run: func(command *cobra.Command, arguments []string) {
			exit.TheProcessWith(tail(asJSON, wakeOn, wantEnded))
		},
	}
	command.Flags().BoolVar(&asJSON, "json", false,
		"print the whole view as one JSON object per line")
	command.Flags().StringSliceVar(&wakeOn, "wake-on", nil,
		"a record field worth waking for; repeat it, or separate them with commas")
	command.Flags().BoolVar(&wantEnded, "all", false, "include sessions that have ended")
	_ = command.RegisterFlagCompletionFunc("wake-on", completeTheRecordFieldsWorthWakingFor)
	return command
}

func tail(asJSON bool, wakeOn []string, wantEnded bool) int {
	err := subscribe.Run(context.Background(),
		subscribe.Integration{Name: "tail", WakeOn: wakeOn, WantEnded: wantEnded},
		func(view session.View) error { return print(view, asJSON) })
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify tail: %v\n", err)
		return 1
	}
	return 0
}

func print(view session.View, asJSON bool) error {
	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(view)
	}

	now := time.Now().UTC()
	if len(view.Changed) == 0 {
		// The world arriving rather than moving: the first view by contract.
		fmt.Printf("── %s: %d session(s) ──\n", now.Format("15:04:05"), len(view.Sessions))
		rows.OneLinePerSession(view.Sessions, now)
		return nil
	}
	for _, change := range view.Changed {
		record := change.Record
		fmt.Printf("%s  %-3d %-24s %-32s %s\n",
			now.Format("15:04:05"), record.Rank,
			rows.CutToWidthWithAnEllipsis(record.DisplayName(), 24),
			rows.CutToWidthWithAnEllipsis(record.State(), 32),
			rows.CutToWidthWithAnEllipsis(rows.OneLine(record.Message), 48))
	}
	return nil
}

// completeTheRecordFieldsWorthWakingFor is curated: `sequence` and
// `updated_at` move on every write, `key` and `created_at` never move, and
// `usage` is a stamp that has to be named to be woken for (§A7.4.3).
func completeTheRecordFieldsWorthWakingFor(
	command *cobra.Command, arguments []string, whatHasBeenTypedSoFar string,
) ([]string, cobra.ShellCompDirective) {
	return []string{
		"kernel", "detail", "rank", "name", "cwd", "branch", "model",
		"state_since", "message", "usage",
		"ended_at", "process", "captured_context", "derived_context",
		"annotations",
	}, cobra.ShellCompDirectiveNoFileComp
}
