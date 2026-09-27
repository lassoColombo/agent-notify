// Command agent-notify-sketchybar puts the semaphore on your menu bar: one
// counter per state, a chip behind each one listing the sessions in it, and a
// click that takes you to the session.
//
// It is a display and nothing else. It never talks to an agent, and nothing an
// agent does waits on it (R13).
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	agentnotify "github.com/lassoColombo/agent-notify"
	"github.com/lassoColombo/agent-notify/subscribe"
)

func main() {
	arguments := os.Args[1:]
	if len(arguments) > 0 {
		switch arguments[0] {
		case "install":
			os.Exit(install(arguments[1:]))
		case "-h", "--help", "help":
			usage(os.Stdout)
			os.Exit(0)
		default:
			fmt.Fprintf(os.Stderr, "agent-notify-sketchybar: %q is not a command\n\n", arguments[0])
			usage(os.Stderr)
			os.Exit(1)
		}
	}
	os.Exit(paint())
}

func usage(to *os.File) {
	fmt.Fprint(to, `agent-notify-sketchybar — the semaphore, on your menu bar

  agent-notify-sketchybar            stay connected and paint
  agent-notify-sketchybar install    print the [integration.sketchybar] table to add; writes nothing
`)
}

func paint() int {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	settings, err := Read(me)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	display := &Display{
		Sketchybar: Sketchybar{Binary: settings.Binary, Timeout: Timeout},
		Bar: Bar{
			Prefix: Name, Position: settings.Position, Rows: settings.Rows,
			Before: settings.Before, After: settings.After,
			Popup:      settings.Popup,
			Announce:   settings.Announce,
			Preview:    settings.Preview,
			Sketchybar: settings.Binary, Core: settings.Core,
			Glyphs: settings.Glyphs, Colours: settings.Colours,
		},
		Logger:    log,
		structure: true,
		wake:      make(chan struct{}, 1),
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Ages go stale on their own. Nothing will ever tell this display that four
	// minutes have become five, because nothing changed — elapsed time is
	// rendering, not state (R26) — so it repaints on its own clock as well as
	// on every change.
	go display.tick(ctx, Refresh)

	log.Info("painting", "sketchybar", settings.Binary, "every", Refresh.String())
	if err := subscribe.Run(ctx, subscribe.Integration{
		Name:     Name,
		Roles:    []string{"display"},
		WakeOn:   WhatToWakeFor(settings),
		OnChange: display.Render,
		Logger:   log,
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	display.clear()
	return 0
}

// WhatToWakeFor is what this display asks the session-watcher to wake it for.
//
// A bar shows the state, the name and how long it has been like that, and none
// of that is in the record's stamps — so the list is short and a session
// writing every few seconds does not repaint anything (R23).
//
// **The message is conditional, and that is the whole of D-72.** The line here
// used to read "the message changes fifty times in a turn and is not on the
// bar", which was true when it was written and stopped being true when the
// preview popup was added: Preview.Of renders record.Message, and nothing
// asked to be woken when the message changed. So a second prompt queued at an
// agent that is already working — same kernel, same state_since, no detail —
// moved nothing this display watched, and the popup went on showing the
// previous prompt.
//
// It is conditional rather than simply added because the preview is optional
// and every path that draws one is behind Preview.on(). A display woken for
// something it does not render is a repaint per prompt for nothing; one that
// renders something it is not woken for shows it stale for ever. Asking
// exactly when it will draw is the only answer that is true either way.
func WhatToWakeFor(settings Resolved) []string {
	wakeOn := []string{"kernel", "detail", "rank", "name", "cwd", "state_since"}
	if settings.Preview.on() {
		wakeOn = append(wakeOn, "message")
	}
	return wakeOn
}

// Display is the render function and what it needs.
type Display struct {
	Sketchybar Sketchybar
	Bar        Bar
	Logger     *slog.Logger

	mu   sync.Mutex
	last []agentnotify.Record
	// wake re-arms the clock. Without it the paint that ENDS an announcement
	// waits for the refresh interval that was already running when the
	// announcement arrived — measured, a chip that stayed lit and open for the
	// better part of a minute after it stopped meaning anything.
	wake chan struct{}
	// structure says the next paint must declare every item it owns before it
	// sets anything. True to begin with, and true again whenever the bar says
	// it has lost something.
	structure bool
	// said is the announcement the previous paint left on the bar. It is the
	// only thing this display remembers between paints, and it exists because
	// opening and closing a chip are edges — see Bar.announcing.
	said agentnotify.Arrival
}

// Render is called with the current state, never with a transition, so there is
// nothing to remember and nothing to get out of step with (R22).
func (d *Display) Render(view subscribe.View) error {
	d.mu.Lock()
	d.last = view.Sessions
	d.mu.Unlock()
	return d.paint(view.Sessions)
}

func (d *Display) paint(sessions []agentnotify.Record) error {
	bar := d.Bar
	bar.Now = time.Now()

	d.mu.Lock()
	was := d.said
	d.mu.Unlock()

	// Asked only when it could matter — an announcement that is ending — so
	// the ordinary paint costs nothing extra. A pointer that has arrived owns
	// the chip, and this is how the bar tells us it arrived.
	seen := false
	if was.Announced() {
		seen = d.Sketchybar.Seen(d.Bar.seen(), was.Record.Key.String())
	}

	arguments, now := Render(bar, sessions, was, seen)
	if now.Announced() && !now.Same(was) {
		// A deadline the clock does not know about is a deadline nothing will
		// ever reach. Never blocks: a wake already waiting is the same wake.
		select {
		case d.wake <- struct{}{}:
		default:
		}
	}
	d.mu.Lock()
	d.said = now
	structure := d.structure
	d.structure = false
	d.mu.Unlock()

	// The structure goes in front of the values, in the same invocation, so
	// that a bar which has just been rebuilt is never asked to set something it
	// does not have yet.
	if structure {
		arguments = append(Scaffold(bar), arguments...)
	}

	complaints, err := d.Sketchybar.Do(arguments)
	if err != nil {
		// A bar that will not draw is this display's problem and nobody
		// else's (R13).
		d.Logger.Warn("painting", "problem", err.Error())
		return nil
	}
	for _, complaint := range complaints {
		d.Logger.Warn("sketchybar", "said", complaint)
		// The bar lost something — it was restarted, or somebody reloaded a
		// config that clears items. Saying so is the only notification there
		// is, so the next paint rebuilds everything this display owns and the
		// structure repairs itself (measured: sketchybar reports a missing
		// item as `[!] Set: Item not found`).
		if strings.Contains(complaint, "not found") {
			d.mu.Lock()
			d.structure = true
			d.mu.Unlock()
		}
	}
	return nil
}

// tick repaints what is already on the bar, so that the ages on the chips stay
// true between changes.
func (d *Display) tick(ctx context.Context, every time.Duration) {
	for {
		d.mu.Lock()
		said := d.said
		d.mu.Unlock()

		// The wait is not fixed. While an announcement is lit, the paint that
		// matters is the one that takes it away, and it has to land when it is
		// over rather than up to a refresh interval later — which for the
		// default of thirty seconds would be a bar that stays lit for half a
		// minute after it stopped meaning anything.
		wait := every
		if said.Announced() {
			if left := time.Until(said.Until) + afterwards; left > 0 && left < wait {
				wait = left
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-d.wake:
			// Something was announced while this was asleep. Work the wait out
			// again rather than paint now — the paint that raised it has just
			// happened.
			continue
		case <-time.After(wait):
		}

		d.mu.Lock()
		sessions := d.last
		d.mu.Unlock()
		if len(sessions) > 0 {
			_ = d.paint(sessions)
		}
	}
}

// afterwards is how far past a deadline the paint that ends an announcement
// lands. Without it the paint races the deadline it is waiting for, finds the
// announcement still live by a microsecond, and schedules itself again.
const afterwards = 100 * time.Millisecond

// clear takes the counters off the bar on the way out.
//
// A display that is no longer running must not leave a number behind saying two
// agents are working: a stale truth on a bar is worse than an empty bar,
// because there is no way to tell it from a current one.
func (d *Display) clear() {
	_, _ = d.Sketchybar.Do(Remove(d.Bar))
}
