// Package subscribe is what a tool-integration imports.
//
// It owns the whole lifecycle — connect, declare, receive the opening snapshot,
// coalesce, reconnect, handle resync, shut down — so that an author writes a
// render function and a main of about ten lines (plan.md §A10.4):
//
//	func main() {
//	    subscribe.Run(context.Background(), subscribe.Integration{
//	        Name:   "zellij",
//	        Roles:  []string{"display"},
//	        WakeOn: []string{"kernel"},
//	        OnChange: func(view subscribe.View) error {
//	            return render(view.Sessions)
//	        },
//	    })
//	}
//
// The callback is handed **the current state**, never a stream of transitions.
// That is not a convenience: it makes R22 structural, so a display physically
// cannot be written in a way that breaks after a dropped message, a restart or
// a closed laptop.
package subscribe

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"time"

	agentnotify "github.com/lassoColombo/agent-notify"
	"github.com/lassoColombo/agent-notify/internal/config"
	"github.com/lassoColombo/agent-notify/internal/core"
	"github.com/lassoColombo/agent-notify/internal/paths"
	"github.com/lassoColombo/agent-notify/internal/watcher"
)

// Integration is what an author fills in.
type Integration struct {
	// Name is what this calls itself, in the handshake and in the log.
	Name string
	// Roles are what it does: "display", "container", "enricher", or something
	// core has never heard of, which is carried and ignored.
	Roles []string
	// WakeOn names the record fields worth waking for. Empty means anything
	// but the stamps. "wake me only when kernel changes" is the common case.
	WakeOn []string
	// WantEnded asks to see ended sessions. A bar says no and they never
	// appear; a picker, whose job is offering you something to resume, says
	// yes; and so does a display that painted something into a UI it does not
	// own and must be told when to give it back, since the record is what
	// remembers which pane it was.
	WantEnded bool
	// OnChange is the render function. It is called with the current state,
	// never with a transition, and an error from it is logged and otherwise
	// ignored: a display's failure is its own (R13).
	OnChange func(View) error

	// Logger is optional; without one nothing is logged.
	Logger *slog.Logger
	// Root overrides where to look for the session-watcher, for tests and for
	// a fake (StartFake).
	Root string
}

// View is the world as it stands.
type View struct {
	// Sessions is everything, most urgent first, already ordered by the rule
	// every display would otherwise write for itself (R24).
	Sessions []agentnotify.Record
	// Changed is what moved since the last call, most urgent first. A renderer
	// ignores it; a notifier reads it and nothing else.
	//
	// "Since the last call" is meant literally, and it is the whole contract:
	// it survives a reconnection, an overflow and a resync, because what it is
	// compared against is what this subscriber was last SHOWN rather than what
	// arrived on this particular connection. On the very first call it is
	// empty — nothing has moved when there was no previous call — which is
	// what stops a display that started thirty seconds ago opening with a
	// banner for every agent that happens to be blocked.
	Changed []Change
	// Why is "snapshot" or "delta", for a log line.
	Why string
}

// Change is one session that moved, and what it moved from.
//
// It exists because a notifier is the one display that genuinely needs the
// transition rather than the state (§A12.1), and everything needed to give it
// one is already here: the session-watcher puts the previous kernel and the
// event in every delta, and a subscriber holding the last view it was shown
// knows the previous kernel even for a change it learns about from a snapshot.
// Before this, both were decoded and dropped, and the one notifier in the world
// rebuilt a weaker version of them from remembered timestamps.
//
// The record is a named field rather than an embedded one on purpose: Record
// has a MarshalJSON, and embedding it would mean a Change logged as JSON
// silently came out as the record alone, with the two fields that make it a
// change missing.
type Change struct {
	// Record is the session as it now stands.
	Record agentnotify.Record
	// PreviousKernel is the kernel this subscriber last saw it in, in the
	// vocabulary [agentnotify.Delta] uses for the same fact. It is empty for a
	// session this subscriber has never seen before, which is not a transition
	// at all — it is an arrival.
	//
	// `PreviousKernel == Record.Kernel` is the ordinary case and means
	// something other than the state moved: a new message, a rename, a fresh
	// token count. A notifier skips those; a preview pane wants them.
	PreviousKernel agentnotify.Kernel
	// Event is what the agent-integration reported, when this change arrived
	// as a delta and something reported it.
	//
	// It is empty in two honest cases, and neither means "nothing happened":
	// a change the session-watcher OBSERVED rather than was told about carries
	// no event (D-12 — death and supersession), and a change first seen in a
	// snapshot was never accompanied by one. PreviousKernel is the field to
	// reason from; this one is for saying why out loud.
	Event agentnotify.Event
}

// Refused is what a version mismatch returns, and it is permanent.
//
// There is no negotiation and no retry: across majors nothing meets at all, so
// the right response is to stop and say what to upgrade (§A10.5, R12).
type Refused struct {
	Reason string
}

func (r *Refused) Error() string { return r.Reason }

// Run connects and stays connected until the context is cancelled.
//
// It starts a session-watcher if there is none, reconnects when one goes away,
// and asks for a snapshot every time it arrives, so a subscriber that was
// started before anything else is correct the moment the rest appears.
func Run(ctx context.Context, integration Integration) error {
	if integration.Name == "" {
		return fmt.Errorf("an integration must say what it is called")
	}
	if integration.OnChange == nil {
		return fmt.Errorf("%s has nothing to do: OnChange is nil", integration.Name)
	}
	// Before the socket, before starting a session-watcher, and here rather
	// than in the watcher: this SDK was compiled from the same source as the
	// record, so it knows exactly which names its author can use, where a
	// watcher one version behind cannot tell a typo from a field the record
	// has since gained (D-70).
	if err := agentnotify.ReasonTheseFieldsCannotBeWokenOn(integration.WakeOn); err != nil {
		return fmt.Errorf("%s: %w", integration.Name, err)
	}
	logger := integration.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	layout, err := integration.layout()
	if err != nil {
		return err
	}

	holding := &held{sessions: map[string]agentnotify.Record{}}

	wait := 50 * time.Millisecond
	for ctx.Err() == nil {
		err := attach(ctx, layout, integration, logger, holding)
		var refused *Refused
		if errors.As(err, &refused) {
			// Permanent. Retrying would only produce the same refusal for as
			// long as both remain installed.
			return err
		}
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			logger.Info("not connected", "problem", err.Error(), "retrying in", wait.String())
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
		if wait < 2*time.Second {
			wait *= 2
		}
	}
	return nil
}

// layout is where this integration's things are.
//
// Both branches end in the same code, and that is the point of it looking this
// dull: naming a root outright used to reach a second, wronger implementation
// of what a root contains (D-69).
func (i Integration) layout() (paths.Layout, error) {
	if i.Root == "" {
		return paths.FromEnvironment()
	}
	return paths.Under(i.Root)
}

// held is what a subscriber has been shown, and it deliberately outlives any
// one connection.
//
// It lived inside attach until it was found to be the reason View.Changed could
// not be used for the one job it exists for. A session-watcher restarting —
// `watcher reload`, a crash, a new version installed, a laptop opening — hands
// every subscriber a fresh snapshot, and a picture of the world that began
// empty at the same moment reports every session in that snapshot as changed.
// A notifier written to the documented contract would therefore post a banner
// per live agent every time the daemon came back.
type held struct {
	sessions map[string]agentnotify.Record
	// shown is false until a view has been handed over. The first one carries
	// no changes: "since the last call" is nothing when there was no last call.
	shown bool
}

// attach is one connection, from dial to disconnect. What it has been shown is
// passed in rather than started here, which is what makes Changed mean "since
// the last call" rather than "since this socket opened".
func attach(
	ctx context.Context, layout paths.Layout,
	integration Integration, logger *slog.Logger, holding *held,
) error {
	connection, err := net.Dial("unix", layout.SubscribersSocket())
	if err != nil {
		// Anyone may start a session-watcher, and a subscriber that finds none
		// is exactly the case the design names (§A9.2). The lock makes the race
		// with every other starter harmless.
		settings, _ := config.Load(layout.ConfigFile)
		if starting := watcher.StartIfNobodyIs(layout, settings.AgentNotifyBinary); starting != nil {
			return fmt.Errorf("no session-watcher, and cannot start one: %w", starting)
		}
		return err
	}
	defer connection.Close()

	go func() {
		<-ctx.Done()
		_ = connection.Close()
	}()

	hello, err := json.Marshal(agentnotify.Hello{
		Kind: agentnotify.KindHello, Name: integration.Name, Version: agentnotify.Version,
		Roles: integration.Roles, WakeOn: integration.WakeOn,
		WantEnded: integration.WantEnded,
	})
	if err != nil {
		return err
	}
	if _, err := connection.Write(append(hello, '\n')); err != nil {
		return err
	}

	reader := bufio.NewReader(connection)
	welcomed := false

	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if errors.Is(err, io.EOF) {
				return fmt.Errorf("the session-watcher went away")
			}
			return err
		}

		kind, err := agentnotify.KindOf(line)
		if err != nil {
			logger.Warn("unreadable message", "problem", err.Error())
			continue
		}

		switch kind {
		case agentnotify.KindRefused:
			var refused agentnotify.Refused
			_ = json.Unmarshal(line, &refused)
			return &Refused{Reason: refused.Reason}

		case agentnotify.KindWelcome:
			welcomed = true
			logger.Info("connected", "as", integration.Name)

		case agentnotify.KindSnapshot:
			var snapshot agentnotify.Snapshot
			if err := json.Unmarshal(line, &snapshot); err != nil {
				logger.Warn("unreadable snapshot", "problem", err.Error())
				continue
			}
			// A snapshot replaces everything. That is what makes overflow,
			// reconnection and a cold start the same code path (§A12.2).
			changed := replaceAll(holding, snapshot.Sessions, integration.WakeOn)
			deliver(integration, logger, holding, changed, "snapshot")

		case agentnotify.KindDelta:
			var delta agentnotify.Delta
			if err := json.Unmarshal(line, &delta); err != nil {
				logger.Warn("unreadable delta", "problem", err.Error())
				continue
			}
			key := delta.Session.Key.String()
			if !integration.WantEnded && delta.Session.Kernel == agentnotify.Ended {
				// It ended, and this subscriber said it did not want ended
				// sessions. It leaves the view now rather than sitting in it
				// until the store prunes it days later, so that WantEnded
				// means the same thing in a delta as it already meant in a
				// snapshot. Without this every display writes the same filter
				// for itself and D-26 — ended sessions are not displayed,
				// period — is a convention rather than a mechanism.
				if _, have := holding.sessions[key]; !have {
					continue
				}
				delete(holding.sessions, key)
				deliver(integration, logger, holding, nil, "delta")
				continue
			}
			if was, have := holding.sessions[key]; have && was.Sequence > delta.Session.Sequence {
				// Older than what we hold. A subscriber applies a record only
				// if it is newer, which is what stops a late delta undoing a
				// snapshot (R16).
				continue
			}
			// The previous kernel comes off the wire rather than out of what
			// is held, because the session-watcher knows it exactly and a
			// subscriber that missed a delta does not.
			holding.sessions[key] = delta.Session
			deliver(integration, logger, holding, []Change{{
				Record:         delta.Session,
				PreviousKernel: delta.PreviousKernel,
				Event:          delta.Event,
			}}, "delta")

		case agentnotify.KindGone:
			var gone agentnotify.Gone
			if err := json.Unmarshal(line, &gone); err != nil {
				continue
			}
			delete(holding.sessions, gone.Key.String())
			deliver(integration, logger, holding, nil, "delta")

		default:
			logger.Debug("a message this build ignores", "kind", kind)
		}

		if !welcomed {
			return fmt.Errorf("the session-watcher said %q before welcoming us", kind)
		}
	}
}

// replaceAll swaps the whole world and reports what actually moved, so that a
// notifier is not made to re-announce everything on every reconnection.
//
// It compares on the fields the subscriber asked to be woken for, which is the
// same rule the session-watcher applies before it sends a delta at all
// (internal/watcher, Subscribers.Publish). Comparing on everything here instead
// meant the two ends of one field disagreed about what "changed" means: a
// display that declared `wake_on = ["kernel"]` is never woken by a new message,
// and used to find one in Changed anyway if it arrived while disconnected.
//
// The previous kernel is carried out with each one. A snapshot says nothing
// about how a session got where it is, but a subscriber that has been shown a
// view before knows what it last saw, and that is the same fact.
func replaceAll(holding *held, fresh []agentnotify.Record, wakeOn []string) []Change {
	was := make(map[string]agentnotify.Kernel, len(fresh))
	var moved []agentnotify.Record
	arrived := make(map[string]bool, len(fresh))

	for _, record := range fresh {
		key := record.Key.String()
		arrived[key] = true
		if previous, have := holding.sessions[key]; !have || agentnotify.Differs(previous, record, wakeOn) {
			was[key] = previous.Kernel
			moved = append(moved, record)
		}
		holding.sessions[key] = record
	}
	for key := range holding.sessions {
		if !arrived[key] {
			delete(holding.sessions, key)
		}
	}

	// Ordered here, while these are still records, so that the one urgency
	// rule does the work rather than a second spelling of it (R24).
	agentnotify.ByUrgency(moved)
	changed := make([]Change, 0, len(moved))
	for _, record := range moved {
		changed = append(changed, Change{Record: record, PreviousKernel: was[record.Key.String()]})
	}
	return changed
}

func deliver(
	integration Integration, logger *slog.Logger,
	holding *held, changed []Change, why string,
) {
	all := make([]agentnotify.Record, 0, len(holding.sessions))
	for _, record := range holding.sessions {
		all = append(all, record)
	}
	agentnotify.ByUrgency(all)

	if !holding.shown {
		// The first view is the world arriving, not the world moving. Every
		// session in it would otherwise read as a change, which is a restart
		// telling you about the past.
		changed = nil
		holding.shown = true
	}

	if err := integration.OnChange(View{Sessions: all, Changed: changed, Why: why}); err != nil {
		// Its failure is its own. Nothing waits for a display and nothing
		// stops because one could not draw (R13).
		logger.Warn("rendering", "problem", err.Error())
	}
}

// Read is the cold read path: what is running, straight from the store, with no
// session-watcher and no socket anywhere.
//
// It is here so that no integration ever walks our directory layout itself —
// the layout is ours to change (§A10.4). An agent's own statusline, polled
// several times a second to show your *other* agents, is the case this exists
// for (§A7.6).
//
// It applies the liveness decision as it reads and writes nothing, exactly as
// `agent-notify list` does and through the same function: a session whose
// process was killed is handed over as ended even if nothing has got round to
// filing it (D-30). An integration that had to do that for itself would be the
// second implementation of it, and the two would disagree about somebody's
// screen (R24).
func (i Integration) Read() ([]agentnotify.Record, error) {
	return i.read(false)
}

// ReadIncludingEnded is Read plus the sessions that are over and still
// resumable — the set bounded by `keep-ended-sessions`.
//
// It is a separate method rather than a flag because the two callers are
// different things: a bar asks the first question and a picker asks the
// second, whose entire job is offering you something to resume (§A7.5).
func (i Integration) ReadIncludingEnded() ([]agentnotify.Record, error) {
	return i.read(true)
}

func (i Integration) read(includeEnded bool) ([]agentnotify.Record, error) {
	layout, err := i.layout()
	if err != nil {
		return nil, err
	}
	opened, err := core.OpenAt(layout, "subscribe")
	if err != nil {
		return nil, err
	}
	defer opened.Close()

	return opened.WhatIsRunning(includeEnded), nil
}

// History is one session's history: the last few things it said and the last
// few times its state moved (§A7.7).
//
// It is read on demand, for one session, and never arrives with a delta — that
// asymmetry is the whole reason it is a separate file, because a delta carrying
// it would grow from a kilobyte to twenty. The caller is a display with room to
// show more than a row: a preview pane, a chip somebody is hovering over.
//
// A session with nothing recorded yet is an empty History and no error, which
// is the ordinary case for one that has only just started.
func (i Integration) History(key agentnotify.Key) (agentnotify.History, error) {
	layout, err := i.layout()
	if err != nil {
		return agentnotify.History{}, err
	}
	opened, err := core.OpenAt(layout, "subscribe")
	if err != nil {
		return agentnotify.History{}, err
	}
	defer opened.Close()

	return opened.Store.History(key)
}
