// Command agent-notify-macos-notifier posts a real macOS notification when
// one of your agents wants you: a banner, in Notification Centre, that takes you
// to the session when you tap it.
//
// It is a display in the sense that matters — it only ever reads (R13), and the
// only thing it ever runs is `agent-notify focus-session`. Since D-87 it is the
// only macOS display there is.
package main

import (
	"fmt"
	"os"

	"log/slog"

	"github.com/lassoColombo/agent-notify/session"
	"github.com/lassoColombo/agent-notify/subscribe"
)

func main() {
	help := func([]string) int { usage(os.Stdout); return 0 }
	os.Exit(subscribe.Main(me, subscribe.Commands{
		Named: map[string]func([]string) int{
			"install":   install,
			"uninstall": uninstall,
			"check":     check,
			holdCommand: hold,
			"help":      help, "-h": help, "--help": help,
		},
		Render: paint,
	}, os.Args[1:]))
}

func usage(to *os.File) {
	fmt.Fprintf(to, `%s — a macOS notification when an agent wants you

  %s check      post a test banner and say what would carry it
  %s install    file the [integration.%s] table
  %s uninstall  remove it

Core runs this; you do not. The banner is posted by alerter, which has to be on
PATH: brew install vjeantet/tap/alerter.

A banner is the whole of what macOS will tell you: the menu bar display that used
to sit beside this one was deleted (D-87).
`, program, program, program, Name, program)
}

// paint is one render: core hands over the world and what moved to reach it,
// and whatever is worth interrupting somebody about becomes a banner.
//
// A render is a process that posts and exits. Nothing here waits for anybody to
// tap anything — see alerter.go, where a detached child does the waiting.
func paint(view session.View) error {
	settings, err := Read(me)
	if err != nil {
		return err
	}
	return (&Banners{
		Notifier:    NewNotifier(settings.Preview, settings.Colours),
		InvaderPNGs: &InvaderPNGs{},
		Alerter:     settings.Alerter,
		Sound:       settings.Sound,
		Logger:      me.Logger(),
	}).Post(view)
}

// check says what would carry a banner, and posts one.
//
// Every way this fails is quiet: alerter missing, a sound macOS will not find,
// a notification macOS files somewhere nobody looks. What it cannot answer is
// whether a banner is on the screen — macOS decides that, in System Settings,
// and the last check is you looking at it.
func check(arguments []string) int {
	if len(arguments) > 0 {
		fmt.Fprintf(os.Stderr, "%s check takes no options.\n", program)
		return 2
	}
	settings, err := Read(me)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("%-16s %s\n", "alerter", settings.Alerter)
	core, err := me.CoreBinary()
	if err != nil {
		fmt.Printf("%-16s %v\n", "focus-session", err)
		return 1
	}
	fmt.Printf("%-16s %s\n", "focus-session", core)
	fmt.Printf("%-16s %s\n", "sound", settings.Sound.Describe())

	notice := Notice{
		Key:      "agent-notify-check",
		Title:    "agent-notify",
		Subtitle: "notifications are working",
		Body:     "If you can read this, agent-notify can interrupt you when an agent needs you.",
		Colour:   DefaultColours[session.FinishedATurn],
	}
	invaderPNGs := &InvaderPNGs{}
	if err := Post(settings.Alerter, notice,
		invaderPNGs.PathOfTheInvaderDrawnIn(notice.Colour), settings.Sound); err != nil {
		fmt.Printf("%-16s %v\n", "notifications", err)
		return 1
	}
	fmt.Printf("%-16s a test banner has been posted\n", "notifications")
	fmt.Println()
	fmt.Println("Tapping it runs focus-session on a session that does not exist, which is")
	fmt.Println("the point: it proves the way back is alive, and says so in the log.")
	fmt.Println("If nothing appeared, macOS is deciding that, not this program — alerter")
	fmt.Println("posts under its own identity, so System Settings -> Notifications is")
	fmt.Println("where banners from it are turned on.")
	return 0
}

// WhatToWakeFor is what this display asks to be woken for. The message is
// here because it is the body of the banner; `state_since` is not, because a
// notification is an edge and nothing about time passing is one. A test moves
// one record field at a time and fails when a banner comes out different for
// something not named here (D-73).
func WhatToWakeFor() []string {
	return []string{"kernel", "detail", "rank", "name", "cwd", "message"}
}

// Banners is the notifier and what it needs.
type Banners struct {
	Notifier    *Notifier
	InvaderPNGs *InvaderPNGs
	// Alerter is the program that puts a banner on the screen.
	Alerter string
	// Sound is what a banner plays. Held here rather than on the Notifier
	// because it is not part of deciding what is worth saying — notify.go's
	// answer is the same either way — only of how it is said.
	Sound  Sound
	Logger *slog.Logger
}

// Post is called with the current state, and with what moved to get to it.
//
// A renderer reads view.Sessions and ignores the rest, which is R22 working as
// intended. This is the one display in the system that does the opposite: a
// banner is an edge, so it reads view.Changed and nothing else. It used to read
// the sessions and reconstruct the edges itself, which is the weaker version of
// this that D-63 was built to replace (D-71).
func (b *Banners) Post(view session.View) error {
	for _, notice := range b.Notifier.Fresh(view.Changed) {
		if err := Post(b.Alerter, notice,
			b.InvaderPNGs.PathOfTheInvaderDrawnIn(notice.Colour), b.Sound); err != nil {
			b.Logger.Warn("posting", "session", notice.Key, "problem", err.Error())
		}
	}
	return nil
}
