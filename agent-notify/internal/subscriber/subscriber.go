// Package subscriber is core's own client of the subscribers socket.
//
// It was the SDK every display imported, and it is internal now because no
// display imports it any more. One that core runs is handed a view on stdin and
// exits; one that owns its process reads `agent-notify tail --json`. Either way
// the socket has a single consumer shape, and it is this — reached through
// `tail`, `replay` and `doctor`, which are all core talking to itself.
//
// The callback is handed **the current state**, never a stream of transitions.
// That is not a convenience: it makes R22 structural, so a display physically
// cannot be written in a way that breaks after a dropped message, a restart or
// a closed laptop.
//
// It is also all the socket carries. The session-watcher sends whole worlds and
// nothing else, and what changed in one is worked out HERE, against what was
// last handed over — which is the only end that can, because this memory
// outlives both the connection and the session-watcher behind it.
package subscriber

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

	"github.com/lassoColombo/agent-notify/internal/config"
	"github.com/lassoColombo/agent-notify/internal/paths"
	"github.com/lassoColombo/agent-notify/internal/sessionwatcher"
	"github.com/lassoColombo/agent-notify/session"
)

// A Subscription is what core asks the socket for.
//
// It is not [subscribe.Integration], and the difference is the point: that one
// is what an author fills in, and this is what core's own client needs. They
// carry the same handful of fields today and they answer to different people,
// so importing the SDK here to save six lines would put the published door
// underneath core's use of it — which the layering test would have caught, and
// did.
type Subscription struct {
	// Name is what this calls itself, in the handshake and in the log.
	Name string
	// WakeOn names the record fields worth waking for. Empty means anything
	// but the stamps.
	WakeOn []string
	// WantEnded asks to see ended sessions.
	WantEnded bool
	// OnChange is handed the current state, never a transition (R22).
	OnChange func(session.View) error

	// Logger is optional; without one nothing is logged.
	Logger *slog.Logger
	// Root overrides where to look for the session-watcher, for tests and for
	// a fake (StartFake).
	Root string
}

// Refused is the session-watcher declining the handshake, and it is permanent.
//
// It is NOT about versions. A version is announced and logged and never
// negotiated on, because everything here is released together and a mismatch is
// a half-finished deployment rather than a protocol to arbitrate (D-77). What
// gets refused is a hello that cannot be acted on: unreadable, or nameless.
//
// There is no retry either. Reconnecting produces the same answer, so the right
// response is to stop and repeat the sentence the session-watcher sent.
type Refused struct {
	Reason string
}

func (r *Refused) Error() string { return r.Reason }

// Run connects and stays connected until the context is cancelled.
//
// It starts a session-watcher if there is none, reconnects when one goes away,
// and asks for a snapshot every time it arrives, so a subscriber that was
// started before anything else is correct the moment the rest appears.
func Run(ctx context.Context, watching Subscription) error {
	if watching.Name == "" {
		return fmt.Errorf("an integration must say what it is called")
	}
	if watching.OnChange == nil {
		return fmt.Errorf("%s has nothing to do: OnChange is nil", watching.Name)
	}
	// Before the socket, before starting a session-watcher, and here rather
	// than in the watcher: this SDK was compiled from the same source as the
	// record, so it knows exactly which names its author can use, where a
	// watcher one version behind cannot tell a typo from a field the record
	// has since gained (D-70).
	if err := session.ReasonTheseFieldsCannotBeWokenOn(watching.WakeOn); err != nil {
		return fmt.Errorf("%s: %w", watching.Name, err)
	}
	logger := watching.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	layout, err := paths.FromEnvironmentOrUnder(watching.Root)
	if err != nil {
		return err
	}

	holding := &session.LastShown{}

	wait := 50 * time.Millisecond
	for ctx.Err() == nil {
		err := attach(ctx, layout, watching, logger, holding)
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

// attach is one connection, from dial to disconnect. What it has been shown is
// passed in rather than started here, which is what makes Changed mean "since
// the last call" rather than "since this socket opened".
func attach(
	ctx context.Context, layout paths.Layout,
	watching Subscription, logger *slog.Logger, holding *session.LastShown,
) error {
	connection, err := net.Dial("unix", layout.SubscribersSocket())
	if err != nil {
		// Anyone may start a session-watcher, and a subscriber that finds none
		// is exactly the case the design names (§A9.2). The lock makes the race
		// with every other starter harmless.
		settings, _ := config.Load(layout.ConfigFile)
		if starting := sessionwatcher.StartIfNobodyIs(layout, settings.AgentNotifyBinary); starting != nil {
			return fmt.Errorf("no session-watcher, and cannot start one: %w", starting)
		}
		return err
	}
	defer connection.Close()

	go func() {
		<-ctx.Done()
		_ = connection.Close()
	}()

	hello, err := json.Marshal(session.Hello{
		Kind: session.KindHello, Name: watching.Name, Version: session.Version,
		WakeOn:    watching.WakeOn,
		WantEnded: watching.WantEnded,
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

		kind, err := session.KindOf(line)
		if err != nil {
			logger.Warn("unreadable message", "problem", err.Error())
			continue
		}

		switch kind {
		case session.KindRefused:
			var refused session.Refused
			_ = json.Unmarshal(line, &refused)
			return &Refused{Reason: refused.Reason}

		case session.KindWelcome:
			welcomed = true
			logger.Info("connected", "as", watching.Name)

		case session.KindSnapshot:
			var snapshot session.Snapshot
			if err := json.Unmarshal(line, &snapshot); err != nil {
				logger.Warn("unreadable snapshot", "problem", err.Error())
				continue
			}
			// A snapshot replaces everything, which is what makes a cold
			// start, a reconnection and an ordinary change one code path
			// (§A12.2) — and what deleted the three messages that used to be
			// the other cases of this switch.
			//
			// An ended session a subscriber did not ask for simply is not in
			// the world it is sent, so D-26 needs no filter here: it is
			// enforced where the world is built.
			// Every world that arrives is handed on, departures included: the
			// session-watcher has already decided this one was worth sending.
			changed, _ := holding.Replace(snapshot.Sessions, watching.WakeOn)
			deliver(watching, logger, holding, changed)

		default:
			logger.Debug("a message this build ignores", "kind", kind)
		}

		if !welcomed {
			return fmt.Errorf("the session-watcher said %q before welcoming us", kind)
		}
	}
}

func deliver(
	watching Subscription, logger *slog.Logger,
	holding *session.LastShown, changed []session.Change,
) {
	if err := watching.OnChange(holding.ViewOf(changed)); err != nil {
		// Its failure is its own. Nothing waits for a display and nothing
		// stops because one could not draw (R13).
		logger.Warn("rendering", "problem", err.Error())
	}
}
