// Command agent-notify-macos-bar puts the semaphore in the macOS menu bar: one
// status item counting what your agents are doing, and a menu behind it
// listing them, most urgent first, with the one you choose brought to the
// front. It needs nothing installed.
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

	"github.com/lassoColombo/agent-notify/logs"
	"github.com/lassoColombo/agent-notify/session"
	"github.com/lassoColombo/agent-notify/subscribe"
)

// AppKit will only be driven from the thread the process started on, and
// NSApplication checks.
func init() { runtime.LockOSThread() }

func main() {
	help := func([]string) int { usage(os.Stdout); return 0 }
	os.Exit(subscribe.Main(me, subscribe.Commands{
		Named: map[string]func([]string) int{
			"install":   install,
			"uninstall": uninstall,
			"dump":      func([]string) int { return dump() },
			"check":     func([]string) int { return check() },
			"help":      help, "-h": help, "--help": help,
		},
		Default: func(arguments []string) int {
			if len(arguments) > 0 {
				fmt.Fprintf(os.Stderr, program+": %q is not a command\n\n", arguments[0])
				usage(os.Stderr)
				return 1
			}
			return paint()
		},
	}, os.Args[1:]))
}

func usage(to *os.File) {
	fmt.Fprint(to, `agent-notify-macos-bar — the semaphore, in the macOS menu bar

  agent-notify-macos-bar            watch the store and paint
  agent-notify-macos-bar check      say what it can and cannot do on this machine
  agent-notify-macos-bar dump       print what it would put on the bar, and stop
  agent-notify-macos-bar install    build the .app bundle, file the [integration.macos-bar] table, load the launch agent
  agent-notify-macos-bar uninstall  stop the launch agent and remove it, the bundle and the table

This is one of two macOS displays and it draws the menu bar. The other posts a
notification when an agent wants you: agent-notify-macos-notifications, its own
program with its own table in the config, installed separately or not at all.
`)
}

// check says what this display can actually do on this machine, because the
// things that decide it are invisible when they are wrong.
func check() int {
	binary, _ := os.Executable()
	fmt.Printf("%-16s %s\n", "binary", binary)
	bundle := RunningIn()
	if bundle == "" {
		fmt.Printf("%-16s none — this is the bare binary, so it cannot remember where you\n", "bundle")
		fmt.Printf("%-16s put its item\n", "")
		if where, err := subscribe.DefaultBundle(program, Identifier, binary); err == nil {
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

	// An NSStatusItem's window is hosted out of process, so only the
	// accessibility API can say whether one went up.
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

// dump prints the document this display would apply, read straight from the
// store: a menu exists only while somebody holds it open, so this is where to
// read what it thinks.
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
	if RunningIn() == "" {
		log.Warn("this is running as a bare binary rather than from its .app bundle, "+
			"so where you put its item will not be remembered and it will go back to the "+
			"far left of the menu bar on every restart",
			"fix", program+" install")
	}
	whatToDoWhenAMenuRowIsChosen = display.focus
	Start(settings.Font)
	log.Info("on the menu bar", "refresh", Refresh.String())

	// Elapsed time is rendering, not state (R26): the ages repaint on a clock.
	go display.repaintOnAClockAndWhenAnAnnouncementEnds(ctx, Refresh)

	outcome := make(chan error, 1)
	go func() {
		outcome <- subscribe.Run(ctx, me, display.Render)
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

// WhatToWakeFor is what this display asks to be woken for. A test moves one
// record field at a time and fails when the menu draws differently for
// something not named here (D-73). `message` is not here: the menu never
// draws it.
func WhatToWakeFor() []string {
	return []string{"kernel", "detail", "rank", "name", "cwd", "state_since"}
}

// Display is the render function and what it needs.
type Display struct {
	Bar    Bar
	Logger *slog.Logger
	mu     sync.Mutex
	last   []session.Record
	// said is the announcement the last paint made, so the clock knows when
	// it must next wake.
	said session.Arrival
	// wake re-arms that clock when an announcement arrives (D-47).
	wake chan struct{}
}

func (d *Display) Render(view session.View) error {
	d.mu.Lock()
	d.last = view.Sessions
	d.mu.Unlock()
	return d.paint(view.Sessions)
}

func (d *Display) paint(sessions []session.Record) error {
	bar := d.Bar
	bar.Now = time.Now()

	document, now := Render(bar, sessions)

	d.mu.Lock()
	fresh := !now.Same(d.said)
	d.said = now
	d.mu.Unlock()

	if fresh && now.Announced() {
		select {
		case d.wake <- struct{}{}:
		default:
		}
	}

	if err := Draw(document); err != nil {
		d.Logger.Warn("painting", "problem", err.Error())
	}
	return nil
}

// repaintOnAClockAndWhenAnAnnouncementEnds keeps the ages true between
// changes, and takes an announcement down when its deadline passes.
func (d *Display) repaintOnAClockAndWhenAnAnnouncementEnds(
	ctx context.Context, every time.Duration,
) {
	for {
		d.mu.Lock()
		said := d.said
		d.mu.Unlock()

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

// howFarPastADeadlineToRepaint keeps the paint that ends an announcement from
// racing the deadline it waits for.
const howFarPastADeadlineToRepaint = 100 * time.Millisecond

func (d *Display) focus(key string) {
	if err := me.Focus(key); err != nil {
		d.Logger.Warn("focusing", "session", key, "problem", err.Error())
	}
}
