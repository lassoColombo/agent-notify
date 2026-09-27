// Command agent-notify-macos-bar puts the semaphore in the macOS menu bar:
// one status item counting what your agents are doing, and a menu behind it
// listing them, most urgent first, with the one you choose brought to the
// front.
//
// It needs nothing installed. Where the sketchybar display needs sketchybar and
// the zellij display needs zellij, this one needs the menu bar every Mac
// already has — which is the reason it exists (M15a).
//
// It is a display and nothing else. It never talks to an agent, and nothing an
// agent does waits on it (R13).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	agentnotify "github.com/lassoColombo/agent-notify"
	"github.com/lassoColombo/agent-notify/logs"
	"github.com/lassoColombo/agent-notify/subscribe"
	"github.com/lassoColombo/agent-notify/tool"
)

// AppKit will only be driven from the thread the process started on, and it is
// not enough to be "a" thread: NSApplication checks. Locking it here, in the
// init of package main, is the earliest moment there is and the only one that
// is guaranteed to be before the runtime has moved the main goroutine anywhere.
func init() { runtime.LockOSThread() }

func main() {
	arguments := os.Args[1:]
	if len(arguments) > 0 {
		switch arguments[0] {
		case "install":
			os.Exit(install(arguments[1:]))
		case "dump":
			os.Exit(dump())
		case "check":
			os.Exit(check())
		case "-h", "--help", "help":
			usage(os.Stdout)
			os.Exit(0)
		default:
			fmt.Fprintf(os.Stderr, program+": %q is not a command\n\n", arguments[0])
			usage(os.Stderr)
			os.Exit(1)
		}
	}
	os.Exit(paint())
}

func usage(to *os.File) {
	fmt.Fprint(to, `agent-notify-macos-bar — the semaphore, in the macOS menu bar

  agent-notify-macos-bar            stay connected and paint
  agent-notify-macos-bar check      say what it can and cannot do on this machine
  agent-notify-macos-bar dump       print what it would put on the bar, and stop
  agent-notify-macos-bar install    build the .app bundle and print the [integration.macos-bar] table to add

This is one of two macOS displays and it draws the menu bar. The other posts a
notification when an agent wants you: agent-notify-macos-notifications, its own
program with its own table in the config, installed separately or not at all.
`)
}

// check says what this display can actually do on this machine.
//
// It exists because of how M15a failed: everything worked, every test passed,
// and nothing was on the screen. The things that decide whether this display is
// any use — is there a bundle, is there a menu bar, can the accessibility API
// see the item — are invisible when they are wrong, and most of them are
// invisible when they are right as well.
func check() int {
	fmt.Printf("%-16s %s\n", "binary", theValueOrAQuestionMark(os.Executable))
	bundle := RunningIn()
	if bundle == "" {
		where, err := DefaultBundle(theValueOrAQuestionMark(os.Executable))
		fmt.Printf("%-16s none — this is the bare binary, so it cannot remember where you\n", "bundle")
		fmt.Printf("%-16s put its item\n", "")
		if err == nil {
			fmt.Printf("%-16s run %s check\n", "", where.PathOfTheBinaryInside())
		}
	} else {
		fmt.Printf("%-16s %s\n", "bundle", bundle)
	}

	if !Available() {
		fmt.Printf("%-16s none — there is no window server in this session\n", "menu bar")
		return 1
	}
	fmt.Printf("%-16s available\n", "menu bar")

	if core, err := me.CoreBinary(); err != nil {
		fmt.Printf("%-16s %v\n", "focus-session", err)
	} else {
		fmt.Printf("%-16s %s\n", "focus-session", core)
	}

	// Put an item up and ask the accessibility API whether it can see one.
	// Nothing else can: an NSStatusItem's window is hosted out of process and
	// never appears in CGWindowListCopyWindowInfo.
	Start("")
	Pump(1500 * time.Millisecond)
	titles, err := OnTheBar()
	switch {
	case err != nil:
		fmt.Printf("%-16s cannot be asked: %v\n", "on the bar", err)
	case len(titles) == 0:
		fmt.Printf("%-16s nothing — the item did not go up\n", "on the bar")
	default:
		fmt.Printf("%-16s %s\n", "on the bar", strings.Join(titles, " | "))
	}
	fmt.Println()
	fmt.Println("Being ON the bar is not the same as being DRAWN on it. A full menu bar")
	fmt.Println("allocates the leftmost items positions and never renders them, and the")
	fmt.Println("accessibility API cannot tell the difference — so the last check is you,")
	fmt.Println("looking at the screen.")
	return 0
}

func theValueOrTheFallback(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func theValueOrAQuestionMark(get func() (string, error)) string {
	value, err := get()
	if err != nil {
		return "?"
	}
	return value
}

// dump prints the document this display would apply, read straight from the
// store and with no menu bar involved.
//
// Every other display in this system can be looked at: a bar item stays where
// it is, a pane title is on the screen. A menu exists for the second somebody
// is holding it open, which makes "what does it think is going on" a question
// with nowhere to read the answer — so here is the answer.
func dump() int {
	settings, err := Read(me)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	sessions, err := me.Read()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	document, _ := Render(Bar{
		Rows: settings.Rows, Glyphs: settings.Glyphs, Symbols: settings.Symbols,
		Colours:  settings.Colours,
		Announce: settings.Announce, Resting: settings.Resting,
		Now: time.Now(),
	}, sessions)

	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println(string(encoded))
	return 0
}

func paint() int {
	log, closeLog := logs.Open(Name)
	defer closeLog.Close()
	settings, err := Read(me)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	display := &Display{
		Bar: Bar{
			Rows:     settings.Rows,
			Glyphs:   settings.Glyphs,
			Symbols:  settings.Symbols,
			Colours:  settings.Colours,
			Announce: settings.Announce,
			Resting:  settings.Resting,
		},
		Core:   settings.Core,
		Logger: log,
		wake:   make(chan struct{}, 1),
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if !Available() {
		fmt.Fprintln(os.Stderr, "there is no window server here, so there is no menu bar to "+
			"draw on: this display has to run in your own logged-in session")
		return 1
	}
	// Said once, at startup, because the symptom is otherwise inexplicable: the
	// display runs, says it is painting, the accessibility API agrees the item
	// exists — and nothing is on the menu bar, because the item is in the
	// leftmost slot and the leftmost slot is where a full menu bar stops
	// drawing (see bundle.go).
	if RunningIn() == "" {
		log.Warn("this is running as a bare binary rather than from its .app bundle, "+
			"so where you put its item will not be remembered and it will go back to the "+
			"far left of the menu bar on every restart",
			"fix", program+" install")
	}
	whatToDoWhenAMenuRowIsChosen = display.focus
	Start(settings.Font)
	log.Info("on the menu bar", "refresh", Refresh.String())

	// Ages go stale on their own. Nothing will ever tell this display that four
	// minutes have become five, because nothing changed — elapsed time is
	// rendering, not state (R26) — so it repaints on its own clock as well as
	// on every change.
	go display.repaintOnAClockAndWhenAnAnnouncementEnds(ctx, Refresh)

	outcome := make(chan error, 1)
	go func() {
		outcome <- subscribe.Run(ctx, subscribe.Integration{
			Name:  Name,
			Roles: []string{"display"},
			// The message is on this list where the sketchybar display leaves
			// it off, and the difference is what a paint costs. There, a paint
			// is a process; here it is a JSON encode and a dispatch onto a
			// queue, so a message that changes fifty times in a turn is
			// affordable — and it is what a row's tooltip is made of.
			WakeOn:   WhatToWakeFor(),
			OnChange: display.Render,
			Logger:   log,
		})
		// Ends the run loop below, which is what lets this process exit.
		Stop()
	}()

	// AppKit owns the main thread from here until Stop.
	Run()

	if err := <-outcome; err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// WhatToWakeFor is what this display asks the session-watcher to wake it for.
//
// It is a function so a test can read it, and the test is the point: it moves
// one record field at a time, renders twice, and fails when the menu draws
// differently for something not named here (D-73). A list kept by hand drifts
// away from the render the first time somebody adds a line to a row — which is
// exactly how sketchybar came to draw the agent's message without asking to be
// told when it changed (D-72).
//
// `message` is NOT here, and was until D-73 found it the other way round: this
// menu has never drawn what an agent said — it shows the state, the name and
// how long — so asking for it bought a repaint every time somebody typed and
// nothing to show for it (R23).
func WhatToWakeFor() []string {
	return []string{"kernel", "detail", "rank", "name", "cwd", "state_since"}
}

// Display is the render function and what it needs.
type Display struct {
	Bar    Bar
	Core   string
	Logger *slog.Logger
	mu     sync.Mutex
	last   []agentnotify.Record
	// said is the announcement the last paint made. Unlike M14's, it is not
	// needed to DRAW anything — there are no edges here, because a menu that
	// nobody has open has no state to disturb — only to know when the clock
	// must next be woken.
	said agentnotify.Arrival
	// wake re-arms that clock. Without it the paint that ENDS an announcement
	// waits for the refresh interval that was already running when the
	// announcement arrived (D-47).
	wake chan struct{}
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

	document, now := Render(bar, sessions)

	d.mu.Lock()
	fresh := !now.Same(d.said)
	d.said = now
	d.mu.Unlock()

	if fresh && now.Announced() {
		// A deadline the clock does not know about is a deadline nothing will
		// ever reach. Never blocks: a wake already waiting is the same wake.
		select {
		case d.wake <- struct{}{}:
		default:
		}
	}

	if err := Draw(document); err != nil {
		// A bar that will not draw is this display's problem and nobody
		// else's (R13).
		d.Logger.Warn("painting", "problem", err.Error())
	}
	return nil
}

// repaintOnAClockAndWhenAnAnnouncementEnds keeps the ages in the menu true
// between changes, and takes an announcement down when its deadline passes.
func (d *Display) repaintOnAClockAndWhenAnAnnouncementEnds(
	ctx context.Context, every time.Duration,
) {
	for {
		d.mu.Lock()
		said := d.said
		d.mu.Unlock()

		// The wait is not fixed. While an announcement is up, the paint that
		// matters is the one that takes it away, and it has to land when it is
		// over rather than up to a refresh interval later.
		wait := every
		if said.Announced() {
			left := time.Until(said.Until) + howFarPastADeadlineToRepaint
			if left > 0 && left < wait {
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

// howFarPastADeadlineToRepaint is how late the paint that ends an announcement
// lands. Without it the paint races the deadline it is waiting for, finds the
// announcement still up by a microsecond, and schedules itself again.
const howFarPastADeadlineToRepaint = 100 * time.Millisecond

// focusTimeout bounds one `agent-notify focus-session`, and it is not a
// performance bound — it is there so that this goroutine cannot be lost.
//
// Generous on purpose. Core walks the container order and gives each step five
// seconds of its own, so a focus through a window manager and a multiplexer can
// legitimately take three times that. A bound shorter than what it is waiting
// on would kill focuses that were about to succeed, which is worse than the
// unbounded wait it replaces.
const focusTimeout = 30 * time.Second

// focus is what choosing a row does.
//
// It runs the same `focus-session` every other display runs, for the reason
// D-37 gives: where a session lives is the containers' business, and a display
// that went and looked would be a second implementation of it, wrong in its own
// way (R24).
func (d *Display) focus(key string) {
	if _, err := tool.Run(d.Core, focusTimeout, "focus-session", "--quiet", key); err != nil {
		d.Logger.Warn("focusing", "session", key, "problem", err.Error())
	}
}
