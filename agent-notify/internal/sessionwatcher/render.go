package sessionwatcher

import (
	"context"
	"encoding/json"
	"log/slog"
	"reflect"
	"slices"
	"sync"
	"time"

	"github.com/lassoColombo/agent-notify/internal/config"
	"github.com/lassoColombo/agent-notify/session"
	"github.com/lassoColombo/agent-notify/tool"
)

// renderTimeout bounds one `render`. Long, because a display shells out to its
// own tool; the bound that matters is that renders do not accumulate.
const renderTimeout = 5 * time.Second

// A renderer is one display core runs: `render` with the view on stdin, once
// per change it asked about. There is one per display name for as long as the
// display is configured, and one goroutine behind it, which is what makes two
// renders of one display in flight together impossible, a reload included
// (D-111).
type renderer struct {
	name   string
	binary string
	logger *slog.Logger
	// stop ends serve, for a display that left the configuration.
	stop context.CancelFunc

	// woken holds at most one: it coalesces, because a render paints the
	// whole world, and it serialises, because two `zellij action rename-pane`
	// in flight together is how a pane ends up wearing the wrong name.
	woken chan struct{}

	mu sync.Mutex
	// asked is what the display answered, under mu because a reload can
	// move it (D-111).
	asked session.Capabilities
	// handed is the world the last wake brought, the same one every other
	// display was handed (D-92). It is taken at the moment of rendering, so a
	// render that waited paints the newest it was given.
	handed []session.Record
	// shown is what this display was last handed. Kept here because a process
	// started fresh for one render remembers nothing.
	shown session.LastShown
}

func (r *renderer) wake(world []session.Record) {
	r.mu.Lock()
	r.handed = world
	r.mu.Unlock()
	select {
	case r.woken <- struct{}{}:
	default:
	}
}

// redeclare swaps in what the display answers now and forgets what it was
// shown, so that the next wake draws a first view (§A7.6). The goroutine
// stays: a render in flight finishes before that view is drawn. It answers
// whether anything moved.
func (r *renderer) redeclare(asked session.Capabilities) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if reflect.DeepEqual(r.asked, asked) {
		return false
	}
	r.asked, r.shown = asked, session.LastShown{}
	return true
}

func (r *renderer) serve(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.woken:
			r.renderOnce()
		}
	}
}

func (r *renderer) renderOnce() {
	// The decision is taken under the lock and the tool is run outside it,
	// so a reload is never held behind zellij.
	r.mu.Lock()
	world, asked := r.handed, r.asked
	if !asked.WantEnded {
		// The world is shared with every other display and not written.
		world = slices.DeleteFunc(slices.Clone(world), func(record session.Record) bool {
			return record.Kernel == session.Ended
		})
	}
	view, worth := r.shown.Replace(world, asked.WakeOn)
	r.mu.Unlock()
	if !worth {
		return
	}

	handed, err := json.Marshal(view)
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

// startRenderers is one renderer per display that answers `render`, kept
// across reloads. One that answers as it did keeps its picture of what it was
// last shown; a new one, or one whose answer moved — a rebuilt display with
// another `wake_on` — is drawn by the reconcile that follows, with the world
// it hands over (D-111).
func (w *Watcher) startRenderers(ctx context.Context) {
	if w.renderers == nil {
		w.renderers = map[string]*renderer{}
	}
	settings := w.opened.Settings
	w.mu.Lock()
	answers := w.answers
	w.mu.Unlock()

	wanted := map[string]bool{}
	for _, name := range settings.Enabled() {
		if !slices.Contains(answers[name].Methods, session.MethodRender) {
			continue
		}
		wanted[name] = true
		if have, found := w.renderers[name]; found {
			if have.redeclare(answers[name]) {
				w.logger.Info("answers differently since it was asked; drawn afresh",
					"display", name, "wake on", answers[name].WakeOn)
			}
			continue
		}

		serving, stop := context.WithCancel(ctx)
		drawing := &renderer{
			name: name, binary: config.Program(name), asked: answers[name],
			logger: w.logger, woken: make(chan struct{}, 1), stop: stop,
		}
		w.renderers[name] = drawing
		w.rendering.Add(1)
		go func() {
			defer w.rendering.Done()
			drawing.serve(serving)
		}()
		w.logger.Info("rendering on demand", "display", name,
			"binary", drawing.binary, "wake on", drawing.asked.WakeOn)
	}

	for name, drawing := range w.renderers {
		if !wanted[name] {
			drawing.stop()
			delete(w.renderers, name)
			w.logger.Info("no longer rendering", "display", name)
		}
	}
}

func (w *Watcher) drawEverythingAgain(world []session.Record) {
	for _, drawing := range w.renderers {
		drawing.wake(world)
	}
}
