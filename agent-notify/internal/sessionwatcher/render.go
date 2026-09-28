package sessionwatcher

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"time"

	"github.com/lassoColombo/agent-notify/session"
	"github.com/lassoColombo/agent-notify/tool"
)

// Drawing, which is what core does with a display now.
//
// It used to keep one running and talk to it down a socket, and the socket is
// still there for anything that connects on its own — but a display core knows
// about is a program it RUNS: `render`, with the view on stdin, and it exits.
// That is the same shape as everything else here (D-38), and it is what lets
// one interface serve every integration.
//
// What it costs is a process per change, and the two things that keep that
// honest are here: a display is only woken for a change it asked about, and a
// render that would say nothing is not run at all.

// renderTimeout bounds one `render`. It is long for what a paint is, and
// deliberately: a display shells out to its own tool — zellij's rename can sit
// behind a wedged server — and the bound that matters is "this does not
// accumulate", not "this is quick".
const renderTimeout = 5 * time.Second

// A renderer is one display, and the goroutine that runs it.
type renderer struct {
	name   string
	binary string

	wakeOn    []string
	wantEnded bool

	logger *slog.Logger
	// theWorldNow is the session-watcher's snapshot, asked for at the moment of
	// rendering rather than carried in: a render that has been sitting in the
	// queue should paint what is true now, not what was true when it was asked
	// for.
	theWorldNow func(wantEnded bool) []session.Record

	// wake holds at most one. That single slot is both halves of the problem:
	// it COALESCES, because a render always paints the whole world and a queued
	// one is therefore never worth keeping beside a newer one; and it
	// SERIALISES, because the loop reading it runs one render at a time, and
	// two `zellij action rename-pane` in flight together is how a pane ends up
	// wearing the wrong name.
	wake chan struct{}

	// shown is what this display was last handed. Keeping it HERE rather than
	// in the display is the whole difference a fork makes: a process started
	// fresh for one render remembers nothing, so "what changed since you last
	// saw it" has to be remembered by whoever does the handing.
	shown session.LastShown
}

// poke asks for a render, and never waits for one.
//
// Called from the sweep, so it must not block on anything: a display whose tool
// is wedged is a display that misses renders, never a session-watcher that
// stops reconciling (R13).
func (r *renderer) poke() {
	select {
	case r.wake <- struct{}{}:
	default:
		// One is already waiting, and it will read the same world this one
		// would have.
	}
}

// serve renders whenever it is poked, one at a time, until the context ends.
func (r *renderer) serve(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.wake:
			r.renderOnce()
		}
	}
}

func (r *renderer) renderOnce() {
	first := !r.shown.HasSeenAView()
	changed, departed := r.shown.Replace(r.theWorldNow(r.wantEnded), r.wakeOn)
	if !first && len(changed) == 0 && !departed {
		// Nothing this display asked about moved and nothing left. This is
		// where `wake_on` earns its keep now: it used to save a write to a
		// socket somebody was already listening on, and it saves a process.
		return
	}

	view := r.shown.ViewOf(changed)
	handed, err := json.Marshal(view)
	if err != nil {
		r.logger.Warn("a view could not be encoded", "display", r.name, "problem", err.Error())
		return
	}

	if _, err := tool.RunWithInput(r.binary, renderTimeout, handed, session.MethodRender); err != nil {
		// A display that failed is not a session that failed (R13). Nothing
		// is retried and nothing is remembered: the next change renders the
		// whole world again, which repairs whatever this one did not draw.
		r.logger.Warn("render", "display", r.name, "problem", err.Error())
	}
}

// theRenderers is one per display core can run, rebuilt whenever what they
// answer is asked again.
//
// A display that is already running keeps its goroutine and its picture of what
// it was last shown across a rebuild, because neither the program on disk nor
// the world changed just because the configuration was re-read.
func (w *Watcher) startRenderers(ctx context.Context) {
	if w.renderers == nil {
		w.renderers = map[string]*renderer{}
	}
	settings := w.opened.Settings
	methods := w.methodsByIntegration()

	wanted := map[string]bool{}
	for _, name := range everyIntegrationCoreCanRun(settings) {
		if !slices.Contains(methods[name], session.MethodRender) {
			continue
		}
		wanted[name] = true
		if already, have := w.renderers[name]; have && already.binary == settings.Integration[name].Binary {
			continue
		}

		answered := w.answers[name]
		drawing := &renderer{
			name: name, binary: settings.Integration[name].Binary,
			wakeOn: answered.WakeOn, wantEnded: answered.WantEnded,
			logger: w.logger, theWorldNow: w.snapshotFor,
			wake: make(chan struct{}, 1),
		}
		w.renderers[name] = drawing
		go drawing.serve(ctx)
		// Once, now, with nothing having changed: this is the cold path
		// (§A7.6). A display that has just been installed, or a
		// session-watcher that has just started, has a world to draw and no
		// change to be told about.
		drawing.poke()
		w.logger.Info("rendering on demand", "display", name,
			"binary", drawing.binary, "wake on", drawing.wakeOn)
	}

	for name := range w.renderers {
		if !wanted[name] {
			// Disabled, deleted, or it stopped answering `render`. Its
			// goroutine ends with the context; dropping it here is what stops
			// it being poked again.
			delete(w.renderers, name)
			w.logger.Info("no longer rendering", "display", name)
		}
	}
}

// drawEverythingAgain asks every display to draw, if anything it cares about
// moved. Nothing here waits.
//
// Both kinds, on one line, which is the point: a display core RUNS and a display
// that CONNECTED are woken by the same event and answer the same question about
// it. All that differs is how the world reaches them — an argument on stdin, or
// a line on a socket.
func (w *Watcher) drawEverythingAgain() {
	for _, drawing := range w.renderers {
		drawing.poke()
	}
	w.subs.PokeEveryone()
}
