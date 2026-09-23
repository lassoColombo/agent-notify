package main

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	agentnotify "github.com/lassoColombo/agent-notify"
	"github.com/lassoColombo/agent-notify/hook"
	"github.com/lassoColombo/agent-notify/internal/core"
)

// reportEvent is the command line spelling of record-agent-event, for a shell
// script, a test, or an agent whose adapter is not written in Go.
//
// It parses arguments and calls hook.Record: there is no second implementation
// of the hook path here and there never will be (R14). Why it exits 0 whatever
// happens and says nothing is R2, and is in the Long below rather than here as
// well.
func reportEventCommand() *cobra.Command {
	var reported reportedEvent
	command := &cobra.Command{
		Use:   "report-event",
		Short: "what an agent-integration calls when its agent did something",
		Long: `Never fails and never speaks. It exits 0 whatever happened and writes nothing
to stdout, because an agent interprets its hook's exit code — Claude treats some
of them as instructions — so a hook that failed would reach into the very
session it is describing. Everything it has to say goes to the log.

There is no second implementation of the hook path here: the Go adapters call
exactly the same function.`,
		GroupID: groupForWhatAnAgentCalls,
		Args:    cobra.ArbitraryArgs,
		// An unknown flag is a mistake in somebody's hook script, and the
		// answer to it is a line in the log — not a non-zero exit into an agent
		// that is listening.
		FParseErrWhitelist: cobra.FParseErrWhitelist{UnknownFlags: true},
		SilenceUsage:       true,
		SilenceErrors:      true,
		Run: func(command *cobra.Command, arguments []string) {
			endTheProcessWith(reportEvent(reported))
		},
	}
	flags := command.Flags()
	flags.StringVar(&reported.agent, "agent", "", "the agent this session belongs to, as configured")
	flags.StringVar(&reported.session, "session", "", "the agent's own id for the session")
	flags.StringVar(&reported.event, "event", "", "one of the nine events (§A5.7)")
	flags.StringVar(&reported.detail, "detail", "", "the agent's refinement of the state")
	flags.StringVar(&reported.message, "message", "", "what the agent said; - reads stdin")
	flags.StringVar(&reported.name, "name", "", "what the agent calls this session")
	flags.StringVar(&reported.cwd, "cwd", "", "the session's working directory")
	flags.BoolVar(&reported.noMessage, "no-message", false, "leave the stored message alone")
	return command
}

// reportedEvent is one invocation's worth of flags, together because there are
// now enough of them that a positional list would be a bug waiting to be typed
// in the wrong order.
type reportedEvent struct {
	agent     string
	session   string
	event     string
	detail    string
	message   string
	name      string
	cwd       string
	noMessage bool
}

func reportEvent(reported reportedEvent) int {
	report, err := buildReport(reported)
	if err != nil {
		logTheProblemBecauseTheHookMayNotSpeak(err.Error())
		return 0
	}

	hook.Record(report)
	return 0
}

// logTheProblemBecauseTheHookMayNotSpeak puts a usage problem where R2 says
// diagnostics go, which is the log and nowhere else. Opening the log for this
// is the whole cost of never being able to say anything on the way out.
func logTheProblemBecauseTheHookMayNotSpeak(problem string) {
	openedCore, err := core.OpenEverythingACommandNeeds("record-agent-event")
	if err != nil {
		return
	}
	defer openedCore.Close()
	openedCore.Logger.Error("report-event", "problem", problem)
}

func buildReport(reported reportedEvent) (agentnotify.Report, error) {
	if reported.agent == "" || reported.session == "" || reported.event == "" {
		return agentnotify.Report{}, fmt.Errorf(
			"--agent, --session and --event are all required; got %q, %q, %q",
			reported.agent, reported.session, reported.event)
	}

	report := agentnotify.Report{
		Key:    agentnotify.Key{Agent: reported.agent, SessionID: reported.session},
		Event:  agentnotify.Event(reported.event),
		Detail: reported.detail,
		Name:   reported.name,
		Cwd:    reported.cwd,
	}
	if !reported.noMessage {
		text := reported.message
		if reported.message == "-" {
			read, err := io.ReadAll(os.Stdin)
			if err != nil {
				return agentnotify.Report{}, fmt.Errorf("cannot read the message from stdin: %w", err)
			}
			text = string(read)
		}
		report.Message = &text
	}
	return report, nil
}
