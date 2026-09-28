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

// renderTimeout bounds one `render`. Long, because a display shells out to its
// own tool; the bound that matters is that renders do not accumulate.
const renderTimeout = 5 * time.Second

// A renderer is one display core runs: `render` with the view on stdin, once
// per change it asked about.
type renderer struct {
	name      string
	binary    string
	wakeOn    []string
	wantEnded bool
	logger    *slog.Logger
	// theWorldNow is asked at the moment of rendering, so a render that waited
	// paints what is true now.
	theWorldNow func(wantEnded bool) []session.Record

	// wake holds at most one: it coalesces, because a render paints the whole
	// world, and it serialises, because two `zellij action rename-pane` in
	// flight together is how a pane ends up wearing the wrong name.
	wake chan struct{}

	// shown is what this display was last handed. Kept here because a process
	// started fresh for one render remembers nothing.
	shown session.LastShown
}

func (r *renderer) poke() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

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
		return
	}

	handed, err := json.Marshal(r.shown.ViewOf(changed))
	if err != nil {
		r.logger.Warn("a view could not be encoded", "display", r.name, "problem", err.Error())
		return
	}
	if _, err := tool.RunWithInput(r.binary, renderTimeout, handed, session.MethodRender); err != nil {
		// Nothing is retried: the next change renders the whole world again
		// (R13).
		r.logger.Warn("render", "display", r.name, "problem", err.Error())
	}
}

// startRenderers is one renderer per display that answers `render`, rebuilt on
// reload. One already running keeps its picture of what it was last shown.
func (w *Watcher) startRenderers(ctx context.Context) {
	if w.renderers == nil {
		w.renderers = map[string]*renderer{}
	}
	settings := w.opened.Settings
	methods := w.methodsByIntegration()

	wanted := map[string]bool{}
	for _, name := range settings.Runnable() {
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
		// The cold path: a world to draw and no change to be told about.
		drawing.poke()
		w.logger.Info("rendering on demand", "display", name,
			"binary", drawing.binary, "wake on", drawing.wakeOn)
	}

	for name := range w.renderers {
		if !wanted[name] {
			delete(w.renderers, name)
			w.logger.Info("no longer rendering", "display", name)
		}
	}
}

func (w *Watcher) drawEverythingAgain() {
	for _, drawing := range w.renderers {
		drawing.poke()
	}
}
