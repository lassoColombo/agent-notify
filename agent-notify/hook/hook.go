// Package hook is what an agent-integration calls when its agent did something:
// the library half of record-agent-event, and the only way anything outside
// this module writes to the store.
//
//	hook.Record(session.Report{
//	    Key:   session.Key{Agent: "claude", SessionID: payload.SessionID},
//	    Event: session.TurnFinished,
//	    Message: &payload.LastAssistantMessage,
//	})
package hook

import (
	"encoding/json"
	"log/slog"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/lassoColombo/agent-notify/hook/internal/branch"
	"github.com/lassoColombo/agent-notify/internal/config"
	"github.com/lassoColombo/agent-notify/internal/core"
	"github.com/lassoColombo/agent-notify/internal/onewatcher"
	"github.com/lassoColombo/agent-notify/internal/process"
	"github.com/lassoColombo/agent-notify/session"
	"github.com/lassoColombo/agent-notify/tool"
)

// captureTimeout bounds every integration's `capture-environment` at once: they
// run concurrently, as children of the hook, on the path the agent waits on
// (R1). On expiry that integration contributes nothing and the next hook tries
// again. Measured: a hook with one wedged container took 1.5s against a 1s
// timeout, the rest being the bounded flush of pipes a grandchild inherited.
//
// A var so a test can give itself room.
var captureTimeout = time.Second

// Record is the whole hook path: resolve where things live, read the config,
// find the agent's process, capture, reduce under the session's lock, write,
// and make sure a session-watcher exists.
//
// It returns no error, by contract: an agent interprets its hooks, so
// everything that goes wrong goes to the log (R2). The record returned is what
// was written; false means nothing was.
func Record(report session.Report) (session.Record, bool) {
	opened, err := core.OpenEverythingACommandNeeds("record-agent-event")
	if err != nil {
		return session.Record{}, false
	}
	defer opened.Close()

	if report.Key.Host == "" {
		host, err := os.Hostname()
		if err != nil {
			host = "localhost"
		}
		report.Key.Host = host
	}
	if err := report.Key.ReasonThisKeyCannotBeUsed(); err != nil {
		opened.Logger.Error("record-agent-event", "problem", err.Error())
		return session.Record{}, false
	}
	if !report.Event.Known() {
		// Recorded as metadata rather than dropped: a bar that is wrong until
		// the next hook costs more than a log line.
		opened.Logger.Warn("an event this build does not know", "event", string(report.Event))
	}
	if report.Cwd == "" {
		if here, err := os.Getwd(); err == nil {
			report.Cwd = here
		}
	}
	// Read here rather than reported: [verified 2026-09-20, 2.1.236] a Claude
	// session whose directory merely contains checkouts writes
	// `gitBranch: "HEAD"` for a repository sitting on `main` (§A7.4.4).
	report.Branch = branch.At(report.Cwd)

	look(opened, &report)

	written, err := opened.Store.Apply(report, time.Now().UTC())
	if err != nil {
		opened.Logger.Error("record-agent-event",
			"session", report.Key.String(), "problem", err.Error())
		return session.Record{}, false
	}
	opened.Logger.Info("recorded",
		"session", written.Key.String(), "event", string(report.Event),
		"kernel", string(written.Kernel), "sequence", written.Sequence)

	// The session-watcher wakes on the store itself; all that is left is to
	// make sure there is one. A race between hooks is harmless: the lock
	// means exactly one survives (§A9.2).
	if err := onewatcher.StartIfNobodyIs(opened.Layout, opened.Settings.AgentNotifyBinary); err != nil {
		opened.Logger.Warn("cannot start a session-watcher", "problem", err.Error())
	}
	return written, true
}

// look fills in what only this process can see: which process the agent is,
// and what its environment says about where it lives. By the time anything
// else reads this event the hook has exited and there is no chain to walk
// (§A8.4).
func look(opened *core.Core, report *session.Report) {
	machine := process.ProcessesOnThisMachine{}
	binary := opened.Settings.Agent[report.Key.Agent].Binary
	agentProcess, chain, found := process.FindAgent(binary, process.Self(), machine)

	if !found {
		// The chain is recorded anyway: it is what a person needs to fix the
		// config.
		opened.Logger.Warn("no ancestor matched the configured binary",
			"agent", report.Key.Agent, "binary", binary, "walked", len(chain))
	} else {
		if boot, err := process.BootIdentity(); err == nil {
			agentProcess.BootID = boot
		} else {
			opened.Logger.Warn("cannot read this boot's identity", "problem", err.Error())
		}
		report.Process = agentProcess
	}

	previous, _, err := opened.Store.Read(report.Key)
	if err != nil {
		previous = session.Record{}
	}
	// Decided before anything is spawned. [measured 2026-09-19, macOS 26]
	// three capturing integrations cost a hook 9.3ms when it captures and
	// 4.9ms when it does not.
	asked := opened.Settings.Runnable()
	if worthCapturing(previous, asked, report.Process) {
		captured := askEveryoneWhoCaptures(opened.Settings, asked, opened.Logger, chain)
		report.CapturedContext = &captured
	}
}

// askEveryoneWhoCaptures runs every runnable integration's `capture-environment`
// concurrently, as children of this process (D-27). One that reads nothing
// answers an empty object, kept as it is; one that hangs, crashes or is
// missing contributes nothing (R13).
func askEveryoneWhoCaptures(
	settings config.Config, asked []string, logger *slog.Logger, chain []session.Ancestor,
) session.CapturedContext {
	captured := session.CapturedContext{Ancestry: chain}

	answered := make([]json.RawMessage, len(asked))
	var running sync.WaitGroup
	for i, name := range asked {
		running.Add(1)
		go func() {
			defer running.Done()
			blob, err := tool.Ask(settings.Integration[name].Binary,
				session.CaptureCommand, nil, captureTimeout)
			if err != nil {
				logger.Warn("capture-environment", "integration", name, "problem", err.Error())
				return
			}
			answered[i] = blob
		}()
	}
	running.Wait()

	for i, name := range asked {
		if len(answered[i]) == 0 {
			continue
		}
		if captured.By == nil {
			captured.By = map[string]json.RawMessage{}
		}
		captured.By[name] = answered[i]
	}
	return captured
}

// worthCapturing: an agent's environment does not change while it runs, and a
// capture voids everything derived from the previous one (§A7.4.1). So only
// when nothing is stored, the process is a different one, or the set of
// integrations that answered is not the set that would be asked. One that
// fails never appears in `stored.By`, so every hook captures again until it
// is fixed.
func worthCapturing(
	previous session.Record, wouldAsk []string, agentProcess session.Process,
) bool {
	stored := previous.CapturedContext
	if stored.CapturedAt.IsZero() {
		return true
	}
	if previous.Process.PID != agentProcess.PID && agentProcess.PID != 0 {
		return true
	}
	return !slices.Equal(wouldAsk, owners(stored.By))
}

func owners(by map[string]json.RawMessage) []string {
	names := make([]string, 0, len(by))
	for name := range by {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
