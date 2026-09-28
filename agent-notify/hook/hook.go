// Package hook is what an agent-integration calls when its agent did something.
//
// It is the library half of record-agent-event, and the only way anything
// outside this module writes to the store — the store itself is internal, and
// its layout is ours to change without breaking a repository we do not control.
//
// An adapter is therefore a translation and one call:
//
//	hook.Record(session.Report{
//	    Key:   session.Key{Agent: "claude", SessionID: payload.SessionID},
//	    Event: session.TurnFinished,
//	    Message: &payload.LastAssistantMessage,
//	})
package hook

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/lassoColombo/agent-notify/capture"
	"github.com/lassoColombo/agent-notify/hook/internal/branch"
	"github.com/lassoColombo/agent-notify/internal/config"
	"github.com/lassoColombo/agent-notify/internal/core"
	"github.com/lassoColombo/agent-notify/internal/process"
	"github.com/lassoColombo/agent-notify/internal/sessionwatcher"
	"github.com/lassoColombo/agent-notify/internal/subcommand"
	"github.com/lassoColombo/agent-notify/session"
)

// captureTimeout bounds one integration's `capture-environment`, which runs as
// a child of the hook and is therefore on the path the agent waits on (R1). On
// expiry that integration contributes nothing, the hook writes and exits, and
// the next hook tries again.
//
// The real worst case is this plus a bounded flush — killing a child does not
// close the pipes a grandchild inherited, so there is a further wait of up to
// half a second before they are closed under it. Measured: a hook with one
// wedged container took 1.5s against a 1s timeout. The coded captures run
// concurrently, so that is the cost however many there are.
//
// A var rather than a const so that the test about where the answers LAND can
// give itself room. One second is the right bound for an agent waiting on a
// hook and the wrong one for three shell scripts racing a build on a loaded
// machine, and a test that fails for that reason teaches nobody anything.
var captureTimeout = time.Second

// Record is record-agent-event as a function: the whole hook path, for an
// agent-integration that has translated one of its agent's hooks into one of
// the eight events.
//
// It does everything: resolves where things live, reads the configuration,
// walks its own ancestry to find the agent's process, runs the
// `capture-environment` of each integration that has one, takes the session's
// lock, reduces, and writes.
//
// It returns no error, and that is the contract rather than an oversight.
// An agent interprets its hooks — Claude reads their stdout into the session
// and treats some exit codes as instructions — so there is nothing useful a
// caller could do with a failure except make things worse. Everything that
// goes wrong goes to the log (plan.md §A17 R2).
//
// The returned record is what was written, and false means nothing was. A
// caller may log it while developing and should ignore it in production.
//
// This is the only way an integration writes to the store. The store itself is
// internal, and its layout is ours to change.
func Record(report session.Report) (session.Record, bool) {
	opened, err := core.OpenEverythingACommandNeeds("record-agent-event")
	if err != nil {
		// Nowhere to log to and nowhere to complain: the one case where this
		// really can do nothing at all. It still must not fail the hook.
		return session.Record{}, false
	}
	defer opened.Close()

	if report.Key.Host == "" {
		host, err := os.Hostname()
		if err != nil {
			// A machine with no name is still a machine, and refusing to
			// record the session would be a strange way to say so.
			host = "localhost"
		}
		report.Key.Host = host
	}
	if err := report.Key.ReasonThisKeyCannotBeUsed(); err != nil {
		opened.Logger.Error("record-agent-event", "problem", err.Error())
		return session.Record{}, false
	}
	if !report.Event.Known() {
		// Not a refusal. It is a mistake in whoever called this, but the cost of
		// dropping a real event is a bar that is wrong until the next hook,
		// while the cost of recording an unknown one is a log line and a session
		// that exists — so the reducer treats it as metadata and this says so.
		opened.Logger.Warn("an event this build does not know", "event", string(report.Event))
	}
	if report.Cwd == "" {
		if here, err := os.Getwd(); err == nil {
			report.Cwd = here
		}
	}
	// Read rather than reported, and read here rather than in each adapter:
	// both agents believe they know which branch this is and at least one of
	// them is wrong about it — [verified 2026-09-20, 2.1.236] a Claude session
	// whose directory merely contains checkouts writes `gitBranch: "HEAD"` for
	// a repository sitting on `main`. It costs a couple of stats on a path that
	// already tolerates a second of capture (§A7.4.4).
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
	wake(opened, written, report.Event)
	return written, true
}

// wake tells a running session-watcher that something changed, and starts one
// if nobody answers.
//
// Both halves are best-effort by design. The poke carries a key and a sequence
// and nothing else, so losing it costs one sweep interval of latency and
// nothing more (R4). Starting one is a race several hooks may enter at once,
// and the singleton lock is what makes that harmless — so this neither waits
// for the result nor reports it (§A9.2).
func wake(opened *core.Core, written session.Record, event session.Event) {
	err := sessionwatcher.Send(opened.Layout, sessionwatcher.PokeFor(written, event))
	if err == nil {
		return
	}
	if !errors.Is(err, sessionwatcher.ErrNobodyListening) {
		opened.Logger.Warn("cannot poke the session-watcher", "problem", err.Error())
		return
	}
	if err := sessionwatcher.StartIfNobodyIs(opened.Layout, opened.Settings.AgentNotifyBinary); err != nil {
		opened.Logger.Warn("cannot start a session-watcher", "problem", err.Error())
		return
	}
	opened.Logger.Info("started a session-watcher, nobody was listening")
}

// look fills in what only this process can see: which process the agent is, and
// what its environment says about where it lives.
//
// The walk must happen here. By the time anything else reads this event the
// hook has exited and there is no chain left to walk from (§A8.4).
func look(opened *core.Core, report *session.Report) {
	machine := process.ProcessesOnThisMachine{}
	binary := opened.Settings.Agent[report.Key.Agent].Binary
	agentProcess, chain, found := process.FindAgent(binary, process.Self(), machine)

	if !found {
		// Recording the chain anyway is the point: a wrapper that re-execs or
		// a renamed binary leaves the session unjudgeable rather than wrong,
		// and the chain is what a person needs in order to fix the config.
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
	// Decided BEFORE anything is spawned, which is the whole point. This used
	// to run every capture and then ask whether the answer was wanted — and it
	// almost never is, because an agent's environment does not change while it
	// runs. [measured 2026-09-19, macOS 26] With three capturing integrations a
	// hook costs 9.3ms when it captures and 4.9ms when it does not, so the
	// spawns were most of what a hook cost — and every one after the first was
	// thrown away.
	asked := sessionwatcher.TheIntegrationsToAsk(opened.Layout, opened.Settings)
	if worthCapturing(previous, asked, report.Process) {
		captured := askEveryoneWhoCaptures(opened.Settings, asked, opened.Logger, chain)
		report.CapturedContext = &captured
	}
}

// askEveryoneWhoCaptures runs the `capture-environment` of every integration
// core can run, as a child of this process, which is the only place an agent's
// environment can be read at all (D-27).
//
// Every one of them, and not the ones a key in the config named. That key was
// only ever there because the hook has no socket and cannot ask (D-39), and it
// was a fact about the program living in the user's file — yes for an
// integration that reads nothing, no for one that reads something, and no error
// either way. An integration with nothing to say answers an empty object, which
// is recorded as nothing at all.
//
// There is one form and there used to be two. The other was a list of variable
// names in the config file, read by core — cheaper, because it spawned nothing,
// and wrong, because it asked the user to write down something the integration
// already knew and could not be told when the two disagreed (D-57).
//
// They run CONCURRENTLY, and that is the difference between one timeout and N
// of them. `capture-timeout` bounds each, so the worst case a hook can cost is
// one timeout however many integrations are enabled — which is what makes R1
// survivable when somebody installs five containers.
//
// Nothing here can fail upwards. An integration that hangs, crashes or is not
// installed contributes nothing, the hook writes and exits, and the next hook
// tries again (R13).
func askEveryoneWhoCaptures(
	settings config.Config, asked []string, logger *slog.Logger, chain []session.Ancestor,
) session.CapturedContext {
	captured := session.CapturedContext{Ancestry: chain}

	// Who to run is handed in rather than worked out again, and it is the same
	// list worthCapturing was shown: look() has already decided whether to be
	// here at all on the strength of it, and a loop that derived its own could
	// send the two different answers.
	//
	// Knowing the list before the loop is also why there is no channel here.
	// Each capture writes its own slot and nothing is shared, so waiting is the
	// only coordination there is. This used to range over every integration and
	// skip most of them, which left the number of writers unknown until the loop
	// had run — and a buffered channel sized to the whole table was the way to
	// collect an unknown number of answers.
	answered := make([]json.RawMessage, len(asked))
	var running sync.WaitGroup

	for i, tool := range asked {
		running.Add(1)
		go func() {
			defer running.Done()
			blob, err := subcommand.Ask(settings.Integration[tool].Binary,
				capture.Command, nil, captureTimeout)
			if err != nil {
				logger.Warn("capture-environment", "integration", tool, "problem", err.Error())
				return
			}
			answered[i] = blob
		}()
	}
	running.Wait()

	for i, tool := range asked {
		if len(answered[i]) == 0 {
			// It hung, crashed, or is not installed.
			continue
		}
		var anything map[string]json.RawMessage
		if err := json.Unmarshal(answered[i], &anything); err == nil && len(anything) == 0 {
			// It ran and had nothing to say. That is the ordinary answer from
			// an integration that reads no environment, now that every one of
			// them is asked rather than the ones a config key named.
			continue
		}
		// Neither is recorded, and for the same reason: an entry that says
		// nothing is not evidence of anything. worthCapturing would read one as
		// an answer, and this session would never be captured again.
		if captured.By == nil {
			captured.By = map[string]json.RawMessage{}
		}
		captured.By[tool] = answered[i]
	}
	return captured
}

// worthCapturing decides whether this event should replace the stored context.
//
// An agent's environment does not change while it runs, so capturing on every
// hook would be pure cost — and it would void everything derived from the
// previous capture each time (§A7.4.1), throwing away coordinates that were
// perfectly good. It is worth doing when there is nothing stored, when the
// process is a different one, or when an integration has been enabled or
// disabled since (D-27).
//
// It takes who WOULD be asked rather than who answered, and that is the
// difference between deciding before the spawns and deciding after them. The
// third condition used to compare against `captured.By`, which cannot be known
// without running everything — so everything ran, on every hook, to answer a
// question whose answer was almost always no.
//
// One consequence, which is a deliberate trade rather than an oversight: an
// integration that is asked and FAILS never appears in `stored.By`, so the sets
// differ and every hook captures again until it is fixed. That is what today's
// code does for everyone unconditionally; here it happens only when something is
// actually broken, and the failure is logged each time it is attempted.
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
