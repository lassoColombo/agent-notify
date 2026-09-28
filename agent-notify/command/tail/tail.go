// Package tail watches sessions change, in a terminal.
//
// It is also the proof that the SDK is what it claims to be: everything below
// the flag parsing is the ten lines an integration author writes, with a render
// function and nothing else (§A10.4). If `tail` needs something the SDK does
// not offer, so does every display.
package tail

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/lassoColombo/agent-notify/command/internal/exit"
	"github.com/lassoColombo/agent-notify/command/internal/rows"
	"github.com/lassoColombo/agent-notify/internal/subscriber"
	"github.com/lassoColombo/agent-notify/session"
)

// Command is `agent-notify tail`.
func Command() *cobra.Command {
	var asJSON, wantEnded bool
	var wakeOn []string
	command := &cobra.Command{
		Use:   "tail",
		Short: "watch sessions change, live",
		Long: `Connects as a display would and prints every view it is handed. This is the
same subscription an integration gets, which makes it the way to find out what
a display is being told before blaming the display.

With --json it is also the way to BE one: a display that has to own its own
process reads these lines instead of linking the library.`,
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
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	err := subscriber.Run(context.Background(), subscriber.Subscription{
		Name:      "tail",
		WakeOn:    wakeOn,
		WantEnded: wantEnded,
		Logger:    logger,
		OnChange: func(view session.View) error {
			return printTheViewJustHandedOver(view, asJSON)
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify tail: %v\n", err)
		return 1
	}
	return 0
}

func printTheViewJustHandedOver(view session.View, asJSON bool) error {
	if asJSON {
		// The whole view, one object per line, and not just what changed.
		// This is the transport a display that runs as its own process reads,
		// so it has to carry what a display needs: a bar draws every row and
		// the ordering is a property of the whole set, while `changed` is
		// empty on the opening snapshot by contract. Printing only the changes
		// left such a client blank until something moved and unable to order
		// what it did get.
		return json.NewEncoder(os.Stdout).Encode(view)
	}

	now := time.Now().UTC()
	if len(view.Changed) == 0 {
		// The world arriving rather than the world moving, which is exactly
		// what an empty Changed means: the first view a display is handed
		// carries no changes by contract, and no later one is handed over at
		// all unless something moved.
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

// completeTheRecordFieldsWorthWakingFor is a curated list and not a derived
// one: a record has more fields than these, and `sequence` and `updated_at`
// move on every write, so completing to one of those would offer somebody a way
// to be woken always. A test keeps it honest about the ones it does offer.
//
// `key` and `created_at` are left out for the opposite reason — they are
// settled when the session is created and never move again, so waking on one is
// waking never.
//
// `usage` is a stamp and is offered anyway, which is not a contradiction: it is
// a stamp so that tokens moving on every response do not wake a bar that never
// asked about them, and naming it here is the only way to ask (§A7.4.3). It is
// the one field where the default and the named answer are meant to differ.
func completeTheRecordFieldsWorthWakingFor(
	command *cobra.Command, arguments []string, whatHasBeenTypedSoFar string,
) ([]string, cobra.ShellCompDirective) {
	return []string{
		"kernel", "detail", "rank", "name", "cwd", "branch", "model",
		"state_since", "message", "usage",
		"ended_at", "process", "captured_context", "derived_context",
		"annotations",
		// No NoSpace: the flag is repeatable, so the useful next keystroke
		// after one field is a space and another --wake-on, not a comma.
	}, cobra.ShellCompDirectiveNoFileComp
}
