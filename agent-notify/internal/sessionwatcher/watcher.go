// Package sessionwatcher is the one long-lived process: it watches agents for
// death, sweeps up behind them, and runs the displays.
package sessionwatcher

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/signal"
	"slices"
	"sync"
	"syscall"
	"time"

	"github.com/lassoColombo/agent-notify/internal/config"
	"github.com/lassoColombo/agent-notify/internal/containers"
	"github.com/lassoColombo/agent-notify/internal/core"
	"github.com/lassoColombo/agent-notify/internal/onewatcher"
	"github.com/lassoColombo/agent-notify/internal/paths"
	"github.com/lassoColombo/agent-notify/internal/process"
	"github.com/lassoColombo/agent-notify/internal/storewatch"
	"github.com/lassoColombo/agent-notify/session"
)

// SweepInterval is the safety net behind the exit and store watches: the
// longest a session that died without saying so can sit in the store looking
// alive. A var so a test can shorten it.
var SweepInterval = 5 * time.Second

// interpretTimeout bounds one `interpret-environment`, which may talk to the
// tool it is asking about (§A11.1).
const interpretTimeout = 5 * time.Second

// Watcher is the one long-lived process: it watches agents for death, derives
// coordinates from what the hook captured, and runs displays when the store
// moves.
type Watcher struct {
	opened *core.Core
	held   *onewatcher.Lock
	exits  *Exits
	store  *storewatch.Watch
	logger *slog.Logger

	mu sync.Mutex
	// refused counts how often a container failed to interpret one session, so
	// that a broken one stops being run every sweep. Cleared on reload.
	refused map[string]int
	// complained remembers a configuration mistake already reported.
	complained map[string]bool

	// answers is what each integration said to `capabilities`, asked at
	// startup and on reload; unanswered is why one did not say.
	answers    map[string]session.Capabilities
	unanswered map[string]string
	// containers is [container] order resolved against those answers, which
	// is the one piece of configuration the deriving goroutine reads.
	containers []containers.Container

	// renderers is one per display core runs.
	renderers map[string]*renderer

	// derivations is how reconcile asks the deriving goroutine to look at the
	// store; one pending ask is enough, as with woken.
	derivations chan struct{}
}

// Start takes the lock and opens the watches. Losing the race for the lock is
// not a failure: anyone may start a session-watcher (§A9.2).
func Start(layout paths.Layout, toTerminal bool) (*Watcher, error) {
	// The directories first, because the lock lives in one of them: on a
	// fresh root `watcher run` otherwise fails to open a lock file in a
	// directory nothing has created yet.
	if err := layout.Create(); err != nil {
		return nil, err
	}
	held, err := onewatcher.Take(layout)
	if err != nil {
		return nil, err
	}

	opened, err := core.OpenAt(layout, "session-watcher")
	if err != nil {
		held.Release()
		return nil, err
	}
	logger := opened.Logger
	if toTerminal {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}

	exits, err := WatchExits()
	if err != nil {
		opened.Close()
		held.Release()
		return nil, err
	}
	store, err := storewatch.Directories(layout.Sessions(), layout.Ended())
	if err != nil {
		exits.Close()
		opened.Close()
		held.Release()
		return nil, err
	}

	return &Watcher{
		opened: opened, held: held, exits: exits, store: store, logger: logger,
	}, nil
}

// Run is the loop. It returns when the context is cancelled or a signal says so.
func (w *Watcher) Run(ctx context.Context) error {
	defer w.close()

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	reload := make(chan os.Signal, 1)
	signal.Notify(reload, syscall.SIGHUP)
	defer signal.Stop(reload)

	w.logger.Info("session-watcher started",
		"pid", os.Getpid(), "version", session.Version,
		"state", w.opened.Layout.State, "runtime", w.opened.Layout.Runtime)

	w.askWhatEachIntegrationAnswers()
	w.resolveContainers()
	w.startRenderers(ctx)
	// The answers are a property of the programs on disk, asked here and on
	// reload, so the report is written at those two moments and no other.
	w.writeReport(w.opened.Settings)

	// Interpretation talks to the tool under its own timeout (D-38), so it
	// runs beside the loop rather than on it: an exit or a store wake is never
	// queued behind zellij. What it derives it writes through the store, and
	// that write wakes the loop, which draws (D-91).
	w.derivations = make(chan struct{}, 1)
	var deriving sync.WaitGroup
	deriving.Add(1)
	go func() {
		defer deriving.Done()
		w.deriveWhenAsked(ctx)
	}()
	defer deriving.Wait()

	// Reconciling from the store at startup is what makes every missed wake-up
	// harmless (§A9.1, R4).
	w.reconcile("startup")

	// One pending wake is enough: reconcile reads the whole store whatever
	// woke it, so wakes that arrive while it runs collapse into one more.
	woken := make(chan string, 1)
	go w.listenForStoreChanges(ctx, woken)
	go w.listenForExits(ctx, woken)

	tick := time.NewTicker(SweepInterval)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("session-watcher stopping")
			return nil
		case <-reload:
			w.reloadConfiguration(ctx)
		case why := <-woken:
			w.reconcile(why)
		case <-tick.C:
			if w.worldIsGone() {
				w.logger.Info("the state directory is gone; nothing left to watch",
					"state", w.opened.Layout.State)
				return nil
			}
			w.prune()
			w.reconcile("sweep")
		}
	}
}

func (w *Watcher) listenForStoreChanges(ctx context.Context, woken chan<- string) {
	for ctx.Err() == nil {
		if w.store.Changed(time.Second) {
			select {
			case woken <- "store":
			default:
			}
		}
	}
}

func (w *Watcher) listenForExits(ctx context.Context, woken chan<- string) {
	for ctx.Err() == nil {
		gone, err := w.exits.WhichPidsExited(time.Second)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			w.logger.Warn("the exit queue", "problem", err.Error())
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if len(gone) == 0 {
			continue
		}
		w.logger.Debug("a watched process exited", "pids", gone)
		select {
		case woken <- "exit":
		default:
		}
	}
}

// reconcile is the whole of the watcher's work: read the store, watch what is
// alive, end what is provably gone, draw. It is the same function whatever
// woke it, because the store is the truth (R4). A record it writes itself
// wakes it once more, and that pass finds nothing to do.
func (w *Watcher) reconcile(why string) {
	w.logger.Debug("reconciling", "why", why)

	// One read and one judgement per wake, however many displays there are:
	// the same read `list` and the SDK make, so every display is handed the
	// world they would have read (D-92). Ended sessions come too when one
	// display asked for them; the others drop them as they render.
	wantEnded := false
	for _, drawing := range w.renderers {
		wantEnded = wantEnded || drawing.asked.WantEnded
	}
	world, ended := w.opened.WhatIsRunningAndWhatEnded(wantEnded)

	// Derivation is asked for, not waited for; what it finds comes back as a
	// store wake.
	select {
	case w.derivations <- struct{}{}:
	default:
	}

	for _, record := range world {
		if record.Kernel != session.Ended && record.Process.PID > 0 {
			// A pid that has already gone fails to register, and the sweep
			// judges it anyway (R5).
			_ = w.exits.Watch(record.Process.PID)
		}
	}

	for _, end := range ended {
		w.end(end)
	}

	// Once, at the end: every display is handed the whole world and decides
	// for itself whether anything it watches moved. The world already says
	// `ended` where the store is about to, so the write above costs one
	// more pass that finds nothing to draw.
	w.drawEverythingAgain(world)
}

// resolveContainers reads [container] order against what each integration
// answered, once per ask, so the deriving goroutine never touches Settings.
func (w *Watcher) resolveContainers() {
	w.mu.Lock()
	answers := w.answers
	w.mu.Unlock()
	configured, problems := containers.Configured(w.opened.Settings, answers)
	for _, problem := range problems {
		w.complainOnce(problem.Error())
	}
	w.mu.Lock()
	w.containers = configured
	w.mu.Unlock()
}

// deriveWhenAsked is the deriving goroutine: each ask is one read of the
// store and one derive over it. A newer ask that arrives while it works
// waits in the channel, so a capture written mid-derive is seen next time.
func (w *Watcher) deriveWhenAsked(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.derivations:
			live, err := w.opened.Store.List()
			if err != nil {
				w.logger.Warn("reading the store to derive", "problem", err.Error())
				continue
			}
			w.derive(live)
		}
	}
}

// derive runs each container's `interpret-environment` on what the hook
// captured, here rather than in the hook because this half talks to the tool
// (D-27). A container is asked once per session per capture: replacing a
// capture voids what was derived from it (§A7.4.1).
func (w *Watcher) derive(live []session.Record) {
	w.mu.Lock()
	configured := w.containers
	w.mu.Unlock()

	for _, record := range live {
		for _, one := range configured {
			captured, present := record.CapturedContext.By[one.Name]
			if !present || isEmptyObject(captured) {
				continue
			}
			if _, already := record.DerivedContext[one.Name]; already {
				continue
			}
			if w.gaveUpOn(record.Key.String(), one.Name) {
				continue
			}
			if !slices.Contains(one.Methods, session.MethodInterpret) {
				continue
			}

			coordinates, err := containers.Interpret(one, captured, interpretTimeout)
			if err != nil {
				failures := w.giveUpOn(record.Key.String(), one.Name)
				level := w.logger.Warn
				if failures >= attemptsBeforeGivingUp {
					level = w.logger.Error
				}
				level("interpret-environment", "container", one.Name,
					"session", record.Key.String(), "attempt", failures,
					"giving up after", attemptsBeforeGivingUp, "problem", err.Error())
				continue
			}

			_, err = w.opened.Store.Update(record.Key, time.Now().UTC(),
				func(previous session.Record) session.Record {
					next := previous.Clone()
					// A capture replaced while this ran must not get the old
					// one's coordinates.
					if !bytes.Equal(next.CapturedContext.By[one.Name], captured) {
						return previous
					}
					if next.DerivedContext == nil {
						next.DerivedContext = map[string]json.RawMessage{}
					}
					next.DerivedContext[one.Name] = coordinates
					return next
				})
			if err != nil {
				w.logger.Warn("recording coordinates",
					"container", one.Name, "session", record.Key.String(), "problem", err.Error())
				continue
			}
			w.logger.Info("placed", "container", one.Name, "session", record.Key.String())
		}
	}
}

// isEmptyObject is what an integration with nothing to read captured.
func isEmptyObject(captured json.RawMessage) bool {
	var fields map[string]json.RawMessage
	return json.Unmarshal(captured, &fields) == nil && len(fields) == 0
}

// attemptsBeforeGivingUp is small and not one: the first failure is usually
// the session-watcher asking zellij about a pane a moment before it exists.
const attemptsBeforeGivingUp = 3

func (w *Watcher) gaveUpOn(session, container string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.refused[session+"\x00"+container] >= attemptsBeforeGivingUp
}

func (w *Watcher) giveUpOn(session, container string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.refused == nil {
		w.refused = map[string]int{}
	}
	key := session + "\x00" + container
	w.refused[key]++
	return w.refused[key]
}

func (w *Watcher) complainOnce(message string) {
	w.mu.Lock()
	if w.complained == nil {
		w.complained = map[string]bool{}
	}
	said := w.complained[message]
	w.complained[message] = true
	w.mu.Unlock()
	if !said {
		w.logger.Warn("configuration", "problem", message)
	}
}

// end files a session that stopped without saying so, through the same
// reducer as everything else (D-8).
func (w *Watcher) end(ending process.Ending) {
	written, err := w.opened.Store.Apply(session.Report{
		Key:    ending.Key,
		Event:  session.SessionEnded,
		Detail: ending.Detail,
	}, time.Now().UTC())
	if err != nil {
		w.logger.Warn("cannot end a session that has gone",
			"session", ending.Key.String(), "problem", err.Error())
		return
	}
	w.logger.Info("ended a session nobody announced",
		"session", ending.Key.String(), "why", ending.Detail, "sequence", written.Sequence)
}

// prune forgets what has outlived keep-ended-sessions. It runs before the
// sweep's reconcile, whose draw then shows the departures.
func (w *Watcher) prune() {
	removed, err := w.opened.Store.ForgetWhatIsTooOld(time.Now().UTC())
	if err != nil {
		w.logger.Warn("pruning", "problem", err.Error())
	}
	for _, key := range removed {
		w.logger.Info("forgot an ended session past its welcome", "session", key.String())
	}
}

// reloadConfiguration re-reads the file on SIGHUP, asks every integration
// again, and retries anything that had failed.
func (w *Watcher) reloadConfiguration(ctx context.Context) {
	settings, problems := config.Load(w.opened.Layout)
	for _, problem := range problems {
		w.logger.Warn("configuration", "problem", problem.Error())
	}
	w.opened.Settings = settings

	w.askWhatEachIntegrationAnswers()
	w.mu.Lock()
	w.refused, w.complained = nil, nil
	w.mu.Unlock()
	w.resolveContainers()
	w.startRenderers(ctx)

	w.writeReport(w.opened.Settings)
	w.logger.Info("configuration re-read")
	// A display started just now has a world to draw and no change to be
	// told about: the cold path (§A7.6).
	w.reconcile("reload")
}

func (w *Watcher) close() {
	if w.store != nil {
		_ = w.store.Close()
	}
	if w.exits != nil {
		_ = w.exits.Close()
	}
	if w.opened != nil {
		w.opened.Close()
	}
	if w.held != nil {
		_ = w.held.Release()
	}
}

// worldIsGone reports that the state directory has been removed under us, the
// one condition that ends this process without being asked.
func (w *Watcher) worldIsGone() bool {
	_, err := os.Stat(w.opened.Layout.Sessions())
	return os.IsNotExist(err)
}
