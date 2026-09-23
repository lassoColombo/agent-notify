package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/lassoColombo/agent-notify/subscribe"
)

// tailCommand watches sessions change, in a terminal.
//
// It is also the proof that the SDK is what it claims to be: everything below
// the flag parsing is the ten lines an integration author writes, with a render
// function and nothing else (§A10.4).
func tailCommand() *cobra.Command {
	var asJSON, wantEnded bool
	var wakeOn []string
	command := &cobra.Command{
		Use:   "tail",
		Short: "watch sessions change, live",
		Long: `Connects as a display would and prints every view it is handed. This is the
same subscription an integration gets, which makes it the way to find out what
a display is being told before blaming the display.`,
		GroupID: groupForLookingAtWhatIsHappening,
		Args:    cobra.NoArgs,
		Run: func(command *cobra.Command, arguments []string) {
			endTheProcessWith(tail(asJSON, wakeOn, wantEnded))
		},
	}
	command.Flags().BoolVar(&asJSON, "json", false, "print each record as JSON")
	command.Flags().StringSliceVar(&wakeOn, "wake-on", nil,
		"a record field worth waking for; repeat it, or separate them with commas")
	command.Flags().BoolVar(&wantEnded, "all", false, "include sessions that have ended")
	_ = command.RegisterFlagCompletionFunc("wake-on", completeTheRecordFieldsWorthWakingFor)
	return command
}

func tail(asJSON bool, wakeOn []string, wantEnded bool) int {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	err := subscribe.Run(context.Background(), subscribe.Integration{
		Name:      "tail",
		Roles:     []string{"display"},
		WakeOn:    wakeOn,
		WantEnded: wantEnded,
		Logger:    logger,
		OnChange: func(view subscribe.View) error {
			return printTheViewJustHandedOver(view, asJSON)
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify tail: %v\n", err)
		return 1
	}
	return 0
}

func printTheViewJustHandedOver(view subscribe.View, asJSON bool) error {
	if asJSON {
		encoder := json.NewEncoder(os.Stdout)
		for _, record := range view.Changed {
			if err := encoder.Encode(record); err != nil {
				return err
			}
		}
		return nil
	}

	now := time.Now().UTC()
	if view.Why == "snapshot" {
		fmt.Printf("── %s: %d session(s) ──\n", now.Format("15:04:05"), len(view.Sessions))
		printOneLinePerSession(view.Sessions, now)
		return nil
	}
	for _, change := range view.Changed {
		record := change.Record
		fmt.Printf("%s  %-3d %-24s %-32s %s\n",
			now.Format("15:04:05"), record.Rank,
			cutToWidthWithAnEllipsis(record.DisplayName(), 24),
			cutToWidthWithAnEllipsis(record.State(), 32),
			cutToWidthWithAnEllipsis(oneLine(record.Message), 48))
	}
	return nil
}
