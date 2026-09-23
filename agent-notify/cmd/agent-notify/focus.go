package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	agentnotify "github.com/lassoColombo/agent-notify"
	"github.com/lassoColombo/agent-notify/container"
	"github.com/lassoColombo/agent-notify/internal/containers"
	"github.com/lassoColombo/agent-notify/internal/core"
)

// focusSession brings one session to the front.
//
// It runs the containers itself rather than asking the session-watcher to,
// which is not a shortcut: focus must work on a machine where nothing is
// running, and routing it through a daemon would add a way for it to fail that
// has nothing to do with whether the pane is there (D-38).
func focusSessionCommand() *cobra.Command {
	var quiet bool
	command := &cobra.Command{
		Use:     "focus-session <session>",
		Aliases: []string{"focus"},
		Short:   "bring one session to the front",
		Long: `Goes through every container in order — the window manager, then the
multiplexer — because where a session lives is the containers' business and
nobody else's. <session> is a key, a session id or a name, as ` + "`list`" + ` prints
them; press TAB for the ones that are running.`,
		GroupID:           groupForDoingSomethingAboutIt,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeTheSessionsThatAreRunning,
		Run: func(command *cobra.Command, arguments []string) {
			endTheProcessWith(focusSession(arguments[0], quiet))
		},
	}
	command.Flags().BoolVar(&quiet, "quiet", false, "say nothing; the exit code is the answer")
	return command
}

func focusSession(wanted string, quiet bool) int {
	openedCore, err := core.OpenEverythingACommandNeeds("focus-session")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer openedCore.Close()

	record, err := findSession(openedCore, wanted)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	steps, outcome := containers.FocusSession(openedCore.Settings, record)
	if !quiet {
		for _, step := range steps {
			switch {
			case step.Skipped:
				fmt.Printf("  %-22s skipped — %s\n", step.Container, step.Outcome.Detail)
			case step.Outcome.OK:
				fmt.Printf("  %-22s focused\n", step.Container)
			default:
				fmt.Printf("  %-22s %s — %s\n", step.Container, step.Outcome.Problem, step.Outcome.Detail)
			}
		}
	}
	if outcome.OK {
		if !quiet {
			fmt.Printf("%s is in front.\n", record.DisplayName())
		}
		return 0
	}

	// The failure is named rather than described, because each one deserves a
	// different next move and a sentence cannot be switched on (§A11.3).
	fmt.Fprintf(os.Stderr, "cannot focus %s: %s — %s\n",
		record.DisplayName(), outcome.Problem, outcome.Detail)
	switch outcome.Problem {
	case container.NoContainer:
		fmt.Fprintln(os.Stderr,
			"Nothing is configured to place a session. Everything else works without one:\n"+
				"states, counts, liveness and history need no container at all.")
	case container.PlaceIsGone:
		fmt.Fprintln(os.Stderr,
			"The place this session was in is gone. The session may still be alive elsewhere;\n"+
				"`agent-notify list` says whether it is.")
	case container.Ambiguous:
		fmt.Fprintln(os.Stderr,
			"Several places match and nothing chooses between them, so nothing was moved:\n"+
				"going to one at random looks exactly like working and is wrong half the time.")
	}
	return 1
}

// focusedCommand answers whether you are already looking at a session.
//
// Three answers, and the third is not a failure: it is the state of every fresh
// install, and a notifier's rule for it is to notify anyway (R27, §A11.7).
func focusedCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "focused <session>",
		Short: "are you already looking at it?",
		Long: `Yes (exit 0), no (exit 1), or nobody can tell (exit 2). The third answer is
not a failure: it is the state of a machine with no container installed, and
anything that treats it as "no" will interrupt somebody who is already looking
at the thing it wants to tell them about.`,
		GroupID:           groupForLookingAtWhatIsHappening,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeTheSessionsThatAreRunning,
		Run: func(command *cobra.Command, arguments []string) {
			endTheProcessWith(focused(arguments[0]))
		},
	}
}

func focused(wanted string) int {
	openedCore, err := core.OpenEverythingACommandNeeds("focused")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer openedCore.Close()

	record, err := findSession(openedCore, wanted)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	verdict := containers.IsFocused(openedCore.Settings, record)
	fmt.Println(verdict.Answer)
	if verdict.Detail != "" {
		fmt.Fprintln(os.Stderr, verdict.Detail)
	}
	switch verdict.Answer {
	case container.Yes:
		return 0
	case container.No:
		return 1
	}
	return 2
}

// annotate writes one owner's section of a record.
//
// Annotations are the third section and the one with no lifetime rule attached:
// they are not derived from a captured context and nothing core does invalidates
// them (§A7.4.1). A git branch, a ticket id, a colour somebody wants remembered.
// Core stores the JSON and never looks inside it (R7).
func annotateCommand() *cobra.Command {
	var remove bool
	command := &cobra.Command{
		Use:   "annotate <session> <owner> [json]",
		Short: "write one owner's section of a record",
		Long: `Core stores the JSON and never looks inside it. Annotations are the one part
of a record with no lifetime rule attached — a branch, a ticket, a colour
somebody wants remembered — and they belong to whoever wrote them.

<json> of - reads stdin. With --remove, leave it out.`,
		GroupID:           groupForDoingSomethingAboutIt,
		Args:              cobra.RangeArgs(2, 3),
		ValidArgsFunction: completeTheSessionsThatAreRunning,
		RunE: func(command *cobra.Command, arguments []string) error {
			if len(arguments) < 3 && !remove {
				return fmt.Errorf("annotate needs the JSON to write, or --remove")
			}
			endTheProcessWith(annotate(arguments, remove))
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

	record, err := findSession(openedCore, arguments[0])
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

	written, err := openedCore.Store.Update(record.Key, time.Now().UTC(), func(previous agentnotify.Record) agentnotify.Record {
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

// findSession turns whatever a person typed into one record.
//
// Three spellings, because three different callers exist: a picker passes the
// key it was given, a person types the name they see on their bar, and a script
// pastes a session id out of a log. Ambiguity is an error that lists the
// candidates rather than a guess, because the whole point of focus is going
// somewhere on purpose.
func findSession(openedCore *core.Core, wanted string) (agentnotify.Record, error) {
	sessions, err := openedCore.Store.List()
	if err != nil {
		return agentnotify.Record{}, err
	}
	ended, err := openedCore.Store.ListEnded()
	if err == nil {
		sessions = append(sessions, ended...)
	}

	var matched []agentnotify.Record
	for _, record := range sessions {
		switch {
		case record.Key.String() == wanted,
			record.Key.SessionID == wanted,
			strings.EqualFold(record.DisplayName(), wanted),
			len(wanted) >= 4 && strings.HasPrefix(record.Key.SessionID, wanted):
			matched = append(matched, record)
		}
	}

	switch len(matched) {
	case 1:
		return matched[0], nil
	case 0:
		return agentnotify.Record{}, fmt.Errorf("no session matches %q — `agent-notify list --all` says what there is", wanted)
	}

	agentnotify.ByUrgency(matched)
	var names strings.Builder
	for _, record := range matched {
		fmt.Fprintf(&names, "\n  %s  %s  %s", record.Key.String(), record.DisplayName(), record.State())
	}
	return agentnotify.Record{}, fmt.Errorf("%q matches %d sessions:%s", wanted, len(matched), names.String())
}
