package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	agentnotify "github.com/lassoColombo/agent-notify"
	"github.com/lassoColombo/agent-notify/internal/core"
)

// The command line, as a tree.
//
// It is cobra rather than a `switch` over `os.Args[1]`, and the trade is worth
// stating: three dependencies where there were two, against a great deal of
// code that is no longer written here. Gone with the switch are a hand-kept
// usage string that had to be edited whenever a flag changed, a `partition`
// helper that existed only so a flag after a positional argument would still
// be a flag, and the per-command `FlagSet` boilerplate. What arrives with it is
// `--help` on every command, "did you mean" on a typo, and completion.
//
// **Completion is the point, not a side effect.** `agent-notify focus-session
// <TAB>` offers the sessions that are actually running, most urgent first, each
// annotated with what it is doing — which is the thing this program is FOR, and
// it would be a hundred lines of shell per shell to write by hand.

const (
	groupForLookingAtWhatIsHappening = "looking"
	groupForDoingSomethingAboutIt    = "doing"
	groupForSettingItUp              = "setting-up"
	groupForWhatAnAgentCalls         = "agents"
)

func theWholeCommandTree() *cobra.Command {
	command := &cobra.Command{
		Use:   "agent-notify",
		Short: "the semaphore over your coding agents",
		Long: `agent-notify keeps one answer up to date — which of your agents wants you —
and hands it to whatever displays it: a menu bar, a pane title, a notification.

This command is the client half of the same library an integration links, and
never a second implementation of anything.`,
		Version:       agentnotify.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		// A bare `agent-notify` says what there is rather than failing, because
		// the first thing anybody types is the name of the program.
		RunE: func(command *cobra.Command, arguments []string) error {
			return command.Help()
		},
	}
	command.SetVersionTemplate("{{.Version}}\n")

	command.AddGroup(
		&cobra.Group{ID: groupForLookingAtWhatIsHappening, Title: "Looking at what is happening:"},
		&cobra.Group{ID: groupForDoingSomethingAboutIt, Title: "Doing something about it:"},
		&cobra.Group{ID: groupForSettingItUp, Title: "Setting it up, and keeping it running:"},
		&cobra.Group{ID: groupForWhatAnAgentCalls, Title: "What an agent-integration calls:"},
	)

	command.AddCommand(
		// `version` as a command as well as a flag, because it was a command
		// before this file existed and something out there types it.
		&cobra.Command{
			Use: "version", Short: "what every integration handshakes against",
			GroupID: groupForLookingAtWhatIsHappening, Args: cobra.NoArgs,
			Run: func(*cobra.Command, []string) {
				fmt.Println(agentnotify.Version)
			},
		},
		listCommand(), tailCommand(), focusedCommand(), doctorCommand(),
		focusSessionCommand(), annotateCommand(),
		installCommand(), watcherCommand(), recordCommand(), replayCommand(),
		reportEventCommand(),
	)
	return command
}

// completeTheSessionsThatAreRunning offers them most urgent first, with what
// each one is doing as the description beside it.
//
// It is the reason this file exists. Everything else cobra brings is comfort;
// this is the program answering its own question at the moment somebody is
// asking it.
func completeTheSessionsThatAreRunning(
	command *cobra.Command, arguments []string, whatHasBeenTypedSoFar string,
) ([]string, cobra.ShellCompDirective) {
	if len(arguments) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	openedCore, err := core.OpenEverythingACommandNeeds("complete")
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	defer openedCore.Close()

	records, err := openedCore.Store.List()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	agentnotify.ByUrgency(records)

	offered := make([]string, 0, len(records))
	for _, record := range records {
		// `name<TAB>description` is cobra's spelling for a completion with a
		// gloss beside it, which zsh and fish both show.
		offered = append(offered, record.DisplayName()+"\t"+string(record.Kernel))
	}
	return offered, cobra.ShellCompDirectiveNoFileComp
}

// completeTheIntegrationsOnPath offers the programs that are actually there,
// which is the same question `install` asks a moment later — so the completion
// cannot offer something that will then be refused.
func completeTheIntegrationsOnPath(
	command *cobra.Command, arguments []string, whatHasBeenTypedSoFar string,
) ([]string, cobra.ShellCompDirective) {
	if len(arguments) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return IntegrationsOnPath(), cobra.ShellCompDirectiveNoFileComp
}

// IntegrationsOnPath is every agent-notify-<name> a person could run, by the
// name you would type after `agent-notify install`.
//
// The naming convention is the only thing core knows about an integration, and
// deliberately so (R10) — this cannot tell a display from an adapter, and does
// not need to. Two callers: the completion above, and the doctor check that
// notices a program you installed and never mentioned in your config.
func IntegrationsOnPath() []string {
	const prefix = "agent-notify-"
	seen := map[string]bool{}
	var found []string
	for _, directory := range filepath.SplitList(os.Getenv("PATH")) {
		entries, err := os.ReadDir(directory)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			name, isOne := strings.CutPrefix(entry.Name(), prefix)
			if !isOne || seen[name] {
				continue
			}
			if _, err := exec.LookPath(entry.Name()); err != nil {
				continue
			}
			seen[name] = true
			found = append(found, name)
		}
	}
	slices.Sort(found)
	return found
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
