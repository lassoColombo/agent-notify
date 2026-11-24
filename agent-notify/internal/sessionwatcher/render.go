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
	// whole world, and it serialises, because two renders in flight together
	// can land out of order and leave a pane wearing the wrong mark.
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
	// epoch moves whenever shown is forgotten by a reload, so that a render
	// that fails across one does not put back a picture of before it.
	epoch int
	// failing is how the last render failed, empty once one succeeds: a
	// display that keeps failing the same way is logged once, not once a
	// sweep.
	failing string
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
	r.epoch++
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
	// so a reload is never held behind the tool.
	r.mu.Lock()
	world, asked, epoch := r.handed, r.asked, r.epoch
	if !asked.WantEnded {
		// The world is shared with every other display and not written.
		world = slices.DeleteFunc(slices.Clone(world), func(record session.Record) bool {
			return record.Kernel == session.Ended
		})
	}
	before := r.shown.Clone()
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
	_, err = tool.RunWithInput(r.binary, renderTimeout, handed, session.MethodRender)

	r.mu.Lock()
	defer r.mu.Unlock()
	if err == nil {
		if r.failing != "" {
			r.logger.Info("drawn again", "display", r.name)
			r.failing = ""
		}
		return
	}
	// What failed to be drawn is still to be drawn: the picture goes back to
	// what the display had before, and the next wake — a sweep at the
	// latest — hands the same change over again, against the world as it is
	// by then (D-130). A display that cannot draw costs one process a sweep,
	// and nothing else (R13).
	if r.epoch == epoch {
		r.shown = before
	}
	if problem := err.Error(); problem != r.failing {
		r.logger.Warn("render", "display", r.name, "problem", problem,
			"then", "drawn again at the next sweep, and not logged again while it fails like this")
		r.failing = problem
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
