// Package annotate writes one owner's section of a record.
//
// Annotations are the third section and the one with no lifetime rule attached:
// they are not derived from a captured context and nothing core does invalidates
// them (§A7.4.1). A git branch, a ticket id, a colour somebody wants remembered.
// Core stores the JSON and never looks inside it (R7).
//
// It is not part of [command/focus] even though it takes the same first
// argument and offers the same completion. What the two share is finding the
// session somebody named, and that is [command/internal/find]; annotating one
// is not focusing it.
package annotate

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/lassoColombo/agent-notify/command/internal/find"
	"github.com/lassoColombo/agent-notify/internal/core"
	"github.com/lassoColombo/agent-notify/session"
)

// Command is `agent-notify annotate`.
func Command() *cobra.Command {
	var remove bool
	command := &cobra.Command{
		Use:   "annotate <session> <owner> [json]",
		Short: "write one owner's section of a record",
		Long: `Core stores the JSON and never looks inside it. Annotations are the one part
of a record with no lifetime rule attached — a branch, a ticket, a colour
somebody wants remembered — and they belong to whoever wrote them.

<json> of - reads stdin. With --remove, leave it out.`,
		Args:              cobra.RangeArgs(2, 3),
		ValidArgsFunction: find.TheSessionsThatAreRunning,
		RunE: func(command *cobra.Command, arguments []string) error {
			if len(arguments) < 3 && !remove {
				return fmt.Errorf("annotate needs the JSON to write, or --remove")
			}
			if code := annotate(arguments, remove); code != 0 {
				os.Exit(code)
			}
			return nil
		},
	}
	command.Flags().BoolVar(&remove, "remove", false,
		"delete this owner's section instead of writing it")
	return command
}

func annotate(arguments []string, remove bool) int {
	openedCore, err := core.OpenEverythingACommandNeeds("annotate")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer openedCore.Close()

	record, err := find.Session(openedCore, arguments[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	owner := arguments[1]

	var value json.RawMessage
	if !remove {
		raw := arguments[2]
		if raw == "-" {
			read, err := io.ReadAll(os.Stdin)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			raw = string(read)
		}
		if !json.Valid([]byte(raw)) {
			fmt.Fprintf(os.Stderr, "annotate: that is not JSON: %s\n", raw)
			return 1
		}
		value = json.RawMessage(raw)
	}

	written, err := openedCore.Store.Update(record.Key, time.Now().UTC(), func(previous session.Record) session.Record {
		next := previous.Clone()
		if remove {
			delete(next.Annotations, owner)
			return next
		}
		if next.Annotations == nil {
			next.Annotations = map[string]json.RawMessage{}
		}
		next.Annotations[owner] = value
		return next
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("%s: annotations.%s %s (sequence %d)\n",
		written.DisplayName(), owner, map[bool]string{true: "removed", false: "written"}[remove],
		written.Sequence)
	return 0
}
