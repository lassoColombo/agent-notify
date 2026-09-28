// Package doctor reports the half of the system that exists: where everything
// resolves to, whether the socket paths will bind, and whether the
// configuration parses.
//
// It opens each thing separately — the layout, the log, the configuration, the
// store — rather than going through [internal/core] as every other command
// does, and that is the point rather than an oversight. A health check that
// assembles everything and then says "it worked" cannot tell you which of the
// four failed.
//
// It is also the widest command in the module: it imports ten of our packages
// where `install` imports one. That is what a health check is, and the layering
// test records the depth rather than pretending otherwise.
package doctor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/lassoColombo/agent-notify/command/internal/exit"
	"github.com/lassoColombo/agent-notify/command/internal/onpath"
	"github.com/lassoColombo/agent-notify/internal/config"
	"github.com/lassoColombo/agent-notify/internal/paths"
	"github.com/lassoColombo/agent-notify/internal/process"
	"github.com/lassoColombo/agent-notify/internal/sessionstore"
	"github.com/lassoColombo/agent-notify/internal/sessionwatcher"
	"github.com/lassoColombo/agent-notify/internal/subscriber"
	"github.com/lassoColombo/agent-notify/logs"
	"github.com/lassoColombo/agent-notify/session"
)

// Command is `agent-notify doctor`.
//
// It exits non-zero when something is wrong, because a health check nothing can
// branch on is a health check nobody runs twice. That is the opposite of the
// hook path's rule (R2), and deliberately so: doctor is run by a person who
// wants to know, record-agent-event by an agent that must not be disturbed.
func Command() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "where everything lives, and what is wrong with it",
		Long: `Names what it does NOT check as well as what it does, so that its output
never reads as a clean bill of health for something still being built.`,
		Args: cobra.NoArgs,
		Run: func(command *cobra.Command, arguments []string) {
			exit.TheProcessWith(doctor())
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

	if err := layout.CheckSockets(); err != nil {
		report("sockets", false, err.Error())
		healthy = false
	} else {
		longest, length := layout.LongestSocket()
		report("sockets", true, fmt.Sprintf("longest path %d of %d bytes — %s",
			length, paths.MaxSocketPath(), longest))
	}

	// The log is opened before the configuration is read, so that the
	// complaints about the configuration have somewhere to go. This is the
	// same order record-agent-event will use, for the same reason.
	logger, closer := logs.OpenFile(layout.LogFile(), "doctor")
	defer closer.Close()

	settings, problems := config.Load(layout.ConfigFile)
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
		}
	}

	reportWatcher(layout, &healthy)
	reportIntegrations(layout, settings, &healthy)
	reportWhatIsInstalledAndNotAskedFor(settings)

	fmt.Println()
	fmt.Println("not checked here: which agents have their hooks installed. Each")
	fmt.Println("agent-integration answers that with its own `install`.")

	if healthy {
		return 0
	}
	return 1
}

// report prints one check. A multi-line detail is indented under its label so
// that a wall of complaints still reads as belonging to one check.
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

// pruned forgets whatever has outlived keep-ended-sessions. doctor is a
// reasonable place for it to happen: until the session-watcher exists, nothing
// else ever runs the sweep, and a store that only grows is a store that will
// surprise somebody.
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

// reportLiveness says what the machine makes of the sessions the store thinks
// are alive. It only looks; ending them is the session-watcher's job (M8).
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
			"session-watcher will file them (plan.md §B, M8)", len(ending))
	}
	report("liveness", true, detail)
}

// reportWatcher says whether the one long-lived process is there, and who it is.
//
// It never kills anything and never starts anything. A session-watcher that
// holds the lock and does not answer is reported, because recovery is the
// user's word and not ours: a notification tool that kills things unprompted is
// not one you would leave running (§A9.2).
func reportWatcher(layout paths.Layout, healthy *bool) {
	if !sessionwatcher.Running(layout) {
		report("watcher", true, "not running — the next hook will start one")
		return
	}
	held, err := sessionwatcher.WhoHolds(layout)
	if err != nil {
		report("watcher", false, "something holds the lock but did not say who: "+err.Error())
		*healthy = false
		return
	}

	detail := fmt.Sprintf("pid %d, version %s, since %s",
		held.PID, held.Version, held.Since.Format(time.RFC3339))
	detail += "\n" + askTheSocket(layout)
	if held.Version != session.Version {
		// Never a stand-down. With several core versions installed at once, a
		// session-watcher that yielded to any newer poke would flap between
		// integrations of different vintages; restarting is the user's word.
		detail += fmt.Sprintf("\nthis binary is %s — `agent-notify watcher restart` when you are ready",
			session.Version)
	}
	report("watcher", true, detail)
}

// askTheSocket connects as an ordinary subscriber and reports what it was told.
//
// This is the difference between "something holds the lock" and "something
// holds the lock and answers" — the wedged session-watcher of §A9.2, which
// doctor names and never kills.
func askTheSocket(layout paths.Layout) string {
	ctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()

	answered := make(chan int, 1)
	go subscriber.Run(ctx, subscriber.Subscription{
		Name: "doctor", Root: layout.Root, WantEnded: true,
		OnChange: func(view session.View) error {
			select {
			case answered <- len(view.Sessions):
			default:
			}
			stop()
			return nil
		},
	})

	select {
	case count := <-answered:
		return fmt.Sprintf("the socket answers: %d session(s) in its snapshot", count)
	case <-ctx.Done():
		return "it holds the lock but did not answer on the socket — " +
			"`agent-notify watcher restart` when you are ready; nothing here kills it"
	}
}

// reportIntegrations is what the session-watcher knows about its integrations,
// read from the file it writes rather than asked for over the socket.
//
// That is deliberate. doctor's hardest job is a session-watcher that holds the
// lock and does not answer (§A9.2), and a report you have to ask for over the
// socket is precisely the report you cannot get in that case. This one is also
// readable with `cat`, which is worth something at three in the morning.
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
			"no report yet — the session-watcher writes one on its first sweep")
		return
	}

	if len(report.Integrations) == 0 {
		fmt.Printf("%-12s %-5s %s\n", "integrations", "ok", "none configured, none connected")
		return
	}

	worst := "ok"
	for _, one := range report.Integrations {
		if one.Answers.Problem != "" {
			worst = "fail"
			*healthy = false
		}
	}
	fmt.Printf("%-12s %-5s %s\n", "integrations", worst,
		fmt.Sprintf("reported by pid %d at %s", report.PID, report.Written))

	for _, one := range report.Integrations {
		fmt.Printf("             %-22s %s\n", one.Name, one.State)
		if one.Binary != "" {
			// Only for something core runs. A client's table was never asked
			// anything, so "answers nothing" would be a report on a
			// conversation that did not happen.
			fmt.Printf("             %-22s %s\n", "", whatItAnswers(one.Answers))
		}
		if one.Answers.Problem != "" {
			fmt.Printf("             %-22s %s\n", "",
				"rebuild and reinstall it, then run `agent-notify watcher reload`")
		}
	}
}

// whatItAnswers is one integration's handshake, in a line.
//
// It is printed for every integration and not only the broken ones, because
// what core will and will not run is now entirely decided by this, and a line
// that only appears when something is wrong is a line nobody learns to read.
func whatItAnswers(answers sessionwatcher.WhatAnIntegrationAnswers) string {
	switch {
	case answers.Problem != "":
		return "did not say what it answers: " + answers.Problem
	case len(answers.Methods) == 0:
		return "answers nothing, so core will never run it"
	default:
		return fmt.Sprintf("answers %s — built against %s",
			strings.Join(answers.Methods, ", "), answers.Version)
	}
}

// reportWhatIsInstalledAndNotAskedFor names the programs that are on your PATH
// and nowhere in your configuration.
//
// It exists because of what D-66 took away. An integration used to write its
// own table, so installing the program and enabling it were one act and there
// was nothing to notice. Now they are two, deliberately — the table is yours to
// write, because the table being there is what says yes — and the gap that
// opens is that nothing tells you the second half never happened. A display
// that is installed, correct, and simply never mentioned looks exactly like one
// that is broken.
//
// It is not a failure and never sets `healthy` false. A program on your PATH
// that you have not asked for is not a fault; it is a decision you have not
// made, or have made the other way.
func reportWhatIsInstalledAndNotAskedFor(settings config.Config) {
	var unasked []string
	for _, name := range onpath.Integrations() {
		// Either table counts as having been asked for. Core does not know
		// which kind of integration it is looking at and must not (R10): a
		// tool-integration is named under [integration], an agent-integration
		// reports for an agent named under [agent], and this can tell neither
		// apart nor needs to.
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
	// What to run is the same either way; what it then does is not, and this
	// deliberately does not claim to know which it is about to be.
	fmt.Printf("%-12s %-5s %s\n", "", "",
		"`agent-notify install <name>` hands over to each: a tool-integration prints")
	fmt.Printf("%-12s %-5s %s\n", "", "",
		"the table for you to add, an agent-integration writes its agent's hooks.")
}
