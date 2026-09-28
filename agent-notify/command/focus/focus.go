// Package focus brings a session to the front, and answers whether one is
// already there.
//
// Both commands run the container-integrations themselves rather than asking
// the session-watcher to, which is not a shortcut: focus must work on a machine
// where nothing is running, and routing it through a daemon would add a way for
// it to fail that has nothing to do with whether the pane is there (D-38).
//
// Which programs the containers are is read from the report the session-watcher
// wrote, because asking every one of them costs a process each and this runs on
// a keypress. That is a shortcut and not a dependency: with no report it asks,
// which is slower and still works on a machine where nothing is running.
package focus

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/lassoColombo/agent-notify/command/internal/exit"
	"github.com/lassoColombo/agent-notify/command/internal/find"
	"github.com/lassoColombo/agent-notify/container"
	"github.com/lassoColombo/agent-notify/internal/containers"
	"github.com/lassoColombo/agent-notify/internal/core"
	"github.com/lassoColombo/agent-notify/internal/sessionwatcher"
)

// FocusSession brings one session to the front.
func FocusSession() *cobra.Command {
	var quiet bool
	command := &cobra.Command{
		Use:     "focus-session <session>",
		Aliases: []string{"focus"},
		Short:   "bring one session to the front",
		Long: `Goes through every container in order — the window manager, then the
multiplexer — because where a session lives is the containers' business and
nobody else's. <session> is a key, a session id or a name, as ` + "`list`" + ` prints
them; press TAB for the ones that are running.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: find.TheSessionsThatAreRunning,
		Run: func(command *cobra.Command, arguments []string) {
			exit.TheProcessWith(focusSession(arguments[0], quiet))
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

	record, err := find.Session(openedCore, wanted)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	steps, outcome := containers.FocusSession(openedCore.Settings,
		sessionwatcher.MethodsByIntegration(openedCore.Layout, openedCore.Settings), record)
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

// Focused answers whether you are already looking at a session.
//
// Three answers, and the third is not a failure: it is the state of every fresh
// install, and a notifier's rule for it is to notify anyway (R27, §A11.7).
func Focused() *cobra.Command {
	return &cobra.Command{
		Use:   "focused <session>",
		Short: "are you already looking at it?",
		Long: `Yes (exit 0), no (exit 1), or nobody can tell (exit 2). The third answer is
not a failure: it is the state of a machine with no container installed, and
anything that treats it as "no" will interrupt somebody who is already looking
at the thing it wants to tell them about.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: find.TheSessionsThatAreRunning,
		Run: func(command *cobra.Command, arguments []string) {
			exit.TheProcessWith(focused(arguments[0]))
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

	record, err := find.Session(openedCore, wanted)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	verdict := containers.IsFocused(openedCore.Settings,
		sessionwatcher.MethodsByIntegration(openedCore.Layout, openedCore.Settings), record)
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
