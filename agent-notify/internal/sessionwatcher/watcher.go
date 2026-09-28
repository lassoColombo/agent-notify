//go:build unix

package sessionwatcher

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/lassoColombo/agent-notify/internal/paths"
	"github.com/lassoColombo/agent-notify/internal/process"
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
	held   *TheOnlyRunningWatcher
	exits  *Exits
	store  *DirWatch
	boot   string
	prober process.ReadsProcessFacts
	logger *slog.Logger

	mu    sync.Mutex
	known map[string]session.Record
	// refused counts how often a container failed to interpret one session, so
	// that a broken one stops being run every sweep. Cleared on reload.
	refused map[string]int
	// complained remembers a configuration mistake already reported.
	complained map[string]bool

	// answers is what each integration said to `capabilities`, asked at
	// startup and on reload.
	answers map[string]WhatAnIntegrationAnswers

	// renderers is one per display core runs.
	renderers map[string]*renderer
}

// Start takes the lock and opens the watches. Losing the race for the lock is
// not a failure: anyone may start a session-watcher (§A9.2).
func Start(layout paths.Layout, toTerminal bool) (*Watcher, error) {
	held, err := TakeIfNobodyElseHasIt(layout)
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
	store, err := WatchDirectories(layout.Sessions(), layout.Ended())
	if err != nil {
		exits.Close()
		opened.Close()
		held.Release()
		return nil, err
	}

	boot, err := process.BootIdentity()
	if err != nil {
		// Without it the reboot rule cannot apply and every session is judged
		// by probing instead, which is slower and still correct.
		logger.Warn("cannot read this boot's identity", "problem", err.Error())
	}

	return &Watcher{
		opened: opened, held: held, exits: exits, store: store,
		boot: boot, prober: process.ProcessesOnThisMachine{}, logger: logger,
		known: map[string]session.Record{},
	}, nil
}

// snapshotFor is the world a display is handed, from memory. Ended sessions
// only for a display that asked (D-26).
func (w *Watcher) snapshotFor(wantEnded bool) []session.Record {
	w.mu.Lock()
	sessions := make([]session.Record, 0, len(w.known))
	for _, record := range w.known {
		sessions = append(sessions, record)
	}
	w.mu.Unlock()

	if wantEnded {
		ended, err := w.opened.Store.ListEnded()
		if err != nil {
			w.logger.Warn("reading ended sessions for a snapshot", "problem", err.Error())
		}
		sessions = append(sessions, ended...)
	}
	session.ByUrgency(sessions)
	return sessions
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
	w.startRenderers(ctx)
	// Reconciling from the store at startup is what makes every missed wake-up
	// harmless (§A9.1, R4).
	w.reconcile("startup")

	woken := make(chan string, 64)
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
			w.writeReport(w.opened.Settings)
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
// woke it, because the store is the truth (R4).
func (w *Watcher) reconcile(why string) {
	live, err := w.opened.Store.List()
	if err != nil {
		w.logger.Warn("reading the store", "why", why, "problem", err.Error())
	}

	live = w.derive(live)

	seen := make(map[string]bool, len(live))
	for _, record := range live {
		if record.Process.PID > 0 {
			// A pid that has already gone fails to register, and the sweep
			// judges it anyway (R5).
			_ = w.exits.Watch(record.Process.PID)
		}
		seen[record.Key.String()] = true
		w.remember(record)
	}
	w.forgetWhatLeft(seen)

	for _, end := range process.Ended(live, w.boot, process.Self(), w.prober) {
		w.end(end)
	}

	// Once, at the end: every display is handed the whole world and decides
	// for itself whether anything it watches moved.
	w.drawEverythingAgain()
}

// derive runs each container's `interpret-environment` on what the hook
// captured, here rather than in the hook because this half talks to the tool
// (D-27). A container is asked once per session per capture: replacing a
// capture voids what was derived from it (§A7.4.1).
func (w *Watcher) derive(live []session.Record) []session.Record {
	configured, problems := containers.Configured(w.opened.Settings, w.methodsByIntegration())
	for _, problem := range problems {
		w.complainOnce(problem.Error())
	}
	if len(configured) == 0 {
		return live
	}

	updated := make([]session.Record, 0, len(live))
	for _, record := range live {
		for _, one := range configured {
			captured, present := record.CapturedContext.By[one.Name]
			if !present {
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

			written, err := w.opened.Store.Update(record.Key, time.Now().UTC(),
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
			record = written
		}
		updated = append(updated, record)
	}
	return updated
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

func (w *Watcher) remember(record session.Record) {
	w.mu.Lock()
	w.known[record.Key.String()] = record
	w.mu.Unlock()
}

func (w *Watcher) forgetWhatLeft(seen map[string]bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for key := range w.known {
		if !seen[key] {
			delete(w.known, key)
		}
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

	w.mu.Lock()
	delete(w.known, ending.Key.String())
	w.mu.Unlock()
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
	settings, problems := config.Load(w.opened.Layout.ConfigFile)
	for _, problem := range problems {
		w.logger.Warn("configuration", "problem", problem.Error())
	}
	w.opened.Settings = settings

	w.askWhatEachIntegrationAnswers()
	w.startRenderers(ctx)

	w.mu.Lock()
	w.refused, w.complained = nil, nil
	w.mu.Unlock()

	w.writeReport(w.opened.Settings)
	w.logger.Info("configuration re-read")
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

// StartIfNobodyIs starts a detached session-watcher unless one holds the lock.
// Several callers may race; the lock means exactly one survives.
func StartIfNobodyIs(layout paths.Layout, configured string) error {
	if Running(layout) {
		return nil
	}
	return Spawn(layout, configured)
}

// Stop asks a running session-watcher to shut down: SIGTERM to the pid the
// lock file names, sent because a person asked. Nothing here ever kills.
func Stop(layout paths.Layout, within time.Duration) error {
	if !Running(layout) {
		return fmt.Errorf("no session-watcher is running")
	}
	held, err := WhoHolds(layout)
	if err != nil {
		return fmt.Errorf("something holds the lock but did not say who: %w", err)
	}
	if err := syscall.Kill(held.PID, syscall.SIGTERM); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return fmt.Errorf("cannot signal pid %d: %w", held.PID, err)
	}

	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if !Running(layout) {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("pid %d still holds the lock %s after being asked to stop", held.PID, within)
}

// worldIsGone reports that the state directory has been removed under us, the
// one condition that ends this process without being asked.
func (w *Watcher) worldIsGone() bool {
	_, err := os.Stat(w.opened.Layout.Sessions())
	return os.IsNotExist(err)
}
