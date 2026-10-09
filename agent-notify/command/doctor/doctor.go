// Package doctor reports where everything lives and what is wrong with it. It
// opens each thing separately rather than through [internal/core], so that it
// can say which one failed.
package doctor

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/lassoColombo/agent-notify/command/internal/onpath"
	"github.com/lassoColombo/agent-notify/internal/config"
	"github.com/lassoColombo/agent-notify/internal/containers"
	"github.com/lassoColombo/agent-notify/internal/onewatcher"
	"github.com/lassoColombo/agent-notify/internal/paths"
	"github.com/lassoColombo/agent-notify/internal/process"
	"github.com/lassoColombo/agent-notify/internal/sessionstore"
	"github.com/lassoColombo/agent-notify/internal/sessionwatcher"
	"github.com/lassoColombo/agent-notify/logs"
	"github.com/lassoColombo/agent-notify/session"
)

// Command exits non-zero when something is wrong: doctor is run by a person
// who wants to know, unlike the hook path (R2).
func Command() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "where everything lives, and what is wrong with it",
		Long: `Names what it does NOT check as well as what it does, so that its output
never reads as a clean bill of health for something still being built.`,
		Args: cobra.NoArgs,
		Run: func(command *cobra.Command, arguments []string) {
			if code := doctor(); code != 0 {
				os.Exit(code)
			}
		},
	}
}

func doctor() int {
	healthy := true

	layout, err := paths.FromEnvironment()
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify doctor: %v\n", err)
		return 1
	}

	fmt.Printf("agent-notify %s   %s/%s\n\n", session.Version, runtime.GOOS, runtime.GOARCH)

	root := "(default — " + paths.TheVariableThatNamesTheRoot + " is not set)"
	if layout.Root != "" {
		root = layout.Root
	}
	fmt.Println("paths")
	for _, line := range [][2]string{
		{"root", root},
		{"state", layout.State},
		{"runtime", layout.Runtime},
		{"config", layout.ConfigFile},
		{"log", layout.LogFile()},
	} {
		fmt.Printf("  %-11s %s\n", line[0], line[1])
	}
	fmt.Println()

	if err := layout.Create(); err != nil {
		report("directories", false, err.Error())
		healthy = false
	} else {
		report("directories", true, fmt.Sprintf("present, mode %o", paths.DirMode))
	}

	// The log is opened before the configuration is read, so that complaints
	// about the configuration have somewhere to go.
	logger, closer := logs.OpenFile(layout.LogFile(), "doctor")
	defer closer.Close()

	settings, problems := config.Load(layout)
	for _, problem := range problems {
		logger.Warn("configuration", "problem", problem.Error())
	}

	summary := fmt.Sprintf("%d agent(s), %d integration(s), keep-ended-sessions %s",
		len(settings.Agent), len(settings.Integration), settings.KeepEndedSessions)

	switch {
	case len(problems) > 0:
		lines := make([]string, 0, len(problems)+1)
		for _, problem := range problems {
			lines = append(lines, problem.Error())
		}
		lines = append(lines, "running with: "+summary)
		report("config", false, strings.Join(lines, "\n"))
		healthy = false
	case fileMissing(layout.ConfigFile):
		report("config", true, "no file, using defaults — "+summary)
	default:
		report("config", true, summary)
	}

	reportCoreBinary(settings, &healthy)

	openedStore, err := sessionstore.Open(layout, settings)
	switch {
	case err != nil:
		report("store", false, err.Error())
		healthy = false
	default:
		live, liveErr := openedStore.List()
		ended, endedErr := openedStore.ListEnded()
		if problem := errors.Join(liveErr, endedErr); problem != nil {
			report("store", false, problem.Error())
			healthy = false
		} else {
			report("store", true, fmt.Sprintf("%d live session(s), %d ended and still "+
				"resumable, %d forgotten this run", len(live), len(ended), pruned(openedStore, logger)))
			reportLiveness(live, &healthy)
			reportAgents(append(live, ended...), settings, &healthy)
		}
	}

	reportWatcher(layout, &healthy)
	reportIntegrations(layout, settings, &healthy)
	reportContainers(layout, settings, &healthy)
	reportWhatIsInstalledAndNotAskedFor(settings)

	fmt.Println()
	fmt.Println("not checked here: whether each agent still runs its hooks. An")
	fmt.Println("agent-integration's `install` says so, and is safe to run again.")

	if healthy {
		return 0
	}
	return 1
}

func report(label string, ok bool, detail string) {
	mark := "ok  "
	if !ok {
		mark = "FAIL"
	}
	lines := strings.Split(detail, "\n")
	fmt.Printf("%-12s %s  %s\n", label, mark, lines[0])
	for _, line := range lines[1:] {
		fmt.Printf("%-12s      %s\n", "", strings.TrimRight(line, " "))
	}
}

func fileMissing(path string) bool {
	_, err := os.Stat(path)
	return os.IsNotExist(err)
}

// pruned forgets whatever has outlived keep-ended-sessions, so a store on a
// machine where no session-watcher runs does not only grow.
func pruned(openedStore *sessionstore.SessionStore, logger *slog.Logger) int {
	removed, err := openedStore.ForgetWhatIsTooOld(time.Now().UTC())
	if err != nil {
		logger.Warn("pruning", "problem", err.Error())
	}
	for _, key := range removed {
		logger.Info("forgot an ended session past its welcome", "session", key.String())
	}
	return len(removed)
}

// reportCoreBinary: without it no hook can start a session-watcher and no
// launchd child can focus anything, and the only sign is a line in the log.
func reportCoreBinary(settings config.Config, healthy *bool) {
	found, err := onewatcher.CoreBinary(settings.AgentNotifyBinary)
	if err != nil {
		report("binary", false, err.Error()+"\n`agent-notify install` files it")
		*healthy = false
		return
	}
	report("binary", true, found)
}

// reportAgents: a session from an agent with no [agent.<name>] table has no
// process to be judged by, so it can never be found dead.
func reportAgents(records []session.Record, settings config.Config, healthy *bool) {
	var unknown []string
	for _, record := range records {
		if _, known := settings.Agent[record.Key.Agent]; !known && !slices.Contains(unknown, record.Key.Agent) {
			unknown = append(unknown, record.Key.Agent)
		}
	}
	slices.Sort(unknown)
	if len(unknown) == 0 {
		report("agents", true, fmt.Sprintf("%d configured", len(settings.Agent)))
		return
	}
	report("agents", false, fmt.Sprintf("sessions from %s and no [agent.<name>] table for them, so "+
		"their liveness cannot be judged;\n`agent-notify install %s` files it",
		strings.Join(unknown, ", "), strings.Join(unknown, " ")))
	*healthy = false
}

// reportContainers says what the session-watcher would complain about in its
// log, here where a person is looking.
func reportContainers(layout paths.Layout, settings config.Config, healthy *bool) {
	if len(settings.Container.Order) == 0 {
		report("containers", true, "none in [container] order; nothing can be focused, everything else works")
		return
	}
	configured, problems := containers.Configured(settings, sessionwatcher.CapabilitiesByIntegration(layout, settings))
	if len(problems) == 0 {
		names := make([]string, 0, len(configured))
		for _, one := range configured {
			names = append(names, one.Name)
		}
		report("containers", true, "outermost first: "+strings.Join(names, ", "))
		return
	}
	lines := make([]string, 0, len(problems))
	for _, problem := range problems {
		lines = append(lines, problem.Error())
	}
	report("containers", false, strings.Join(lines, "\n"))
	*healthy = false
}

// reportLiveness only looks; ending sessions is the session-watcher's job.
func reportLiveness(live []session.Record, healthy *bool) {
	boot, err := process.BootIdentity()
	if err != nil {
		report("liveness", false, err.Error())
		*healthy = false
		return
	}

	machine := process.ProcessesOnThisMachine{}
	counts := map[process.Verdict]int{}
	for _, record := range live {
		counts[process.LivenessOf(record, boot, machine)]++
	}

	detail := fmt.Sprintf("boot %s", boot)
	if len(live) > 0 {
		detail += fmt.Sprintf(" — %d running, %d gone but not yet ended, %d cannot tell",
			counts[process.Running], counts[process.Gone], counts[process.CannotTell])
	}
	if ending := process.Ended(live, boot, process.Self(), machine); len(ending) > 0 {
		detail += fmt.Sprintf("\n%d session(s) have ended without saying so; the "+
			"session-watcher will file them", len(ending))
	}
	report("liveness", true, detail)
}

// reportWatcher never kills and never starts anything (§A9.2).
func reportWatcher(layout paths.Layout, healthy *bool) {
	if !onewatcher.Running(layout) {
		report("watcher", true, "not running — the next hook will start one")
		return
	}
	held, err := onewatcher.WhoHolds(layout)
	if err != nil {
		report("watcher", false, "something holds the lock but did not say who: "+err.Error())
		*healthy = false
		return
	}

	detail := fmt.Sprintf("pid %d, version %s, since %s",
		held.PID, held.Version, held.Since.Format(time.RFC3339))
	if held.Version != session.Version {
		detail += fmt.Sprintf("\nthis binary is %s — `agent-notify watcher restart` when you are ready",
			session.Version)
	}
	report("watcher", true, detail)
}

// reportIntegrations reads the file the session-watcher writes, which works
// even when the session-watcher does not answer.
func reportIntegrations(
	layout paths.Layout, settings config.Config, healthy *bool,
) {
	report, found := sessionwatcher.ReadReport(layout)
	if !found {
		if len(settings.Integration) == 0 {
			fmt.Printf("%-12s %-5s %s\n", "integrations", "ok", "none configured")
			return
		}
		fmt.Printf("%-12s %-5s %s\n", "integrations", "--",
			"no report yet — the session-watcher writes one when it starts")
		return
	}

	if len(report.Integrations) == 0 {
		fmt.Printf("%-12s %-5s %s\n", "integrations", "ok", "none configured")
		return
	}

	worst := "ok"
	for _, one := range report.Integrations {
		if one.Problem != "" {
			worst = "fail"
			*healthy = false
		}
	}
	fmt.Printf("%-12s %-5s %s\n", "integrations", worst,
		fmt.Sprintf("reported by pid %d at %s", report.PID, report.Written))

	for _, one := range report.Integrations {
		fmt.Printf("             %-22s %s\n", one.Name, one.State)
		if one.Binary != "" {
			fmt.Printf("             %-22s %s\n", "", whatItAnswers(one))
		}
		if one.Problem != "" {
			fmt.Printf("             %-22s %s\n", "",
				"rebuild and reinstall it, then run `agent-notify watcher reload`")
		}
	}
}

func whatItAnswers(one sessionwatcher.Integration) string {
	switch {
	case one.Problem != "":
		return "did not say what it answers: " + one.Problem
	case len(one.Answers.Methods) == 0:
		return "answers nothing, so core will never run it"
	default:
		return fmt.Sprintf("answers %s — built against %s",
			strings.Join(one.Answers.Methods, ", "), one.Answers.Version)
	}
}

// reportWhatIsInstalledAndNotAskedFor names the programs on your PATH whose
// install has not been run. Never a failure.
func reportWhatIsInstalledAndNotAskedFor(settings config.Config) {
	var unasked []string
	for _, name := range onpath.Integrations() {
		if _, wanted := settings.Integration[name]; wanted {
			continue
		}
		if _, wanted := settings.Agent[name]; wanted {
			continue
		}
		unasked = append(unasked, name)
	}
	if len(unasked) == 0 {
		return
	}

	fmt.Printf("%-12s %-5s %s\n", "installed", "--",
		fmt.Sprintf("%d on your PATH and not mentioned here: %s",
			len(unasked), strings.Join(unasked, ", ")))
	fmt.Printf("%-12s %-5s %s\n", "", "",
		"`agent-notify install` sets each of them up.")
}
