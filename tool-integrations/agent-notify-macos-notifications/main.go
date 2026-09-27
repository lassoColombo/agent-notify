// Command agent-notify-macos-notifications posts a real macOS notification when
// one of your agents wants you: a banner, in Notification Centre, that takes you
// to the session when you tap it.
//
// It is a display in the sense that matters — it only ever reads (R13) — and it
// is the one integration in this system that asks a person for permission, which
// is why it is its own program and its own bundle rather than a setting on the
// menu bar display (D-37).
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	agentnotify "github.com/lassoColombo/agent-notify"
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
		case "check":
			os.Exit(check(arguments[1:]))
		case "-h", "--help", "help":
			usage(os.Stdout)
			os.Exit(0)
		default:
			fmt.Fprintf(os.Stderr, "%s: %q is not a command\n\n", program, arguments[0])
			usage(os.Stderr)
			os.Exit(1)
		}
	}
	os.Exit(notify())
}

func usage(to *os.File) {
	fmt.Fprintf(to, `%s — a macOS notification when an agent wants you

  %s            stay connected and notify
  %s check      say whether macOS will deliver them, and post a test banner
  %s check --sound NAME   post it with that sound, to hear one before
                          writing it into the config
  %s install    build the .app bundle and print the [integration.%s] table to add

This is one of two macOS displays and it does the interrupting. The other puts
the semaphore on your menu bar: agent-notify-macos-bar, its own program with its
own table in the config, installed separately or not at all.
`, program, program, program, program, program, Name)
}

// check says whether this can actually interrupt anybody on this machine.
//
// It exists because every way this fails is silent. An ad-hoc signature is
// refused without a prompt; a bare binary aborts the process; a provisional
// grant delivers to Notification Centre and shows nothing at all. A program
// that posts notifications nobody ever sees looks exactly like one that is
// broken, and there is nowhere to look it up.
func check(arguments []string) int {
	flags := flag.NewFlagSet("check", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	// Auditioning a sound is the one thing the config cannot do for you.
	// macOS never reports that it could not find a name — it plays the default
	// chime instead — so a name that IS on this machine and a name the
	// notification daemon will not take sound identical from here. The
	// difference is audible and nothing else, which is what this is for.
	named := flags.String("sound", "", "post the test banner with this sound")
	if err := flags.Parse(arguments); err != nil {
		fmt.Fprintf(os.Stderr, program+" check: %v\n", err)
		return 2
	}
	sound := Sound{Plays: true}
	if *named != "" {
		file, found := TheSoundFileCalled(*named, SoundDirectories())
		if !found {
			fmt.Fprintf(os.Stderr, program+" check: %q is not a sound on this machine.\n"+
				"There is: %s.\nA sound of your own goes in ~/Library/Sounds.\n",
				*named, strings.Join(TheSoundsOnThisMachine(SoundDirectories()), ", "))
			return 1
		}
		sound.Name = file
	}

	fmt.Printf("%-16s %s\n", "binary", theValueOrAQuestionMark(os.Executable))
	bundle := RunningIn()
	if bundle == "" {
		fmt.Printf("%-16s none — this is the bare binary, and macOS will not take a\n", "bundle")
		fmt.Printf("%-16s notification from one at all\n", "")
		if where, err := DefaultBundle(theValueOrAQuestionMark(os.Executable)); err == nil {
			fmt.Printf("%-16s run %s install, then %s check\n",
				"", program, where.PathOfTheBinaryInside())
		}
		return 1
	}
	fmt.Printf("%-16s %s\n", "bundle", bundle)

	if !Available() {
		fmt.Printf("%-16s none — there is no logged-in session here to notify into\n", "session")
		return 1
	}
	if core, err := me.CoreBinary(); err != nil {
		fmt.Printf("%-16s %v\n", "focus-session", err)
	} else {
		fmt.Printf("%-16s %s\n", "focus-session", core)
	}

	if !Start() {
		fmt.Printf("%-16s not possible without the bundle\n", "permission")
		return 1
	}
	fmt.Printf("%-16s %s\n", "permission",
		theValueOrTheFallback(Status(), "could not be asked"))
	invaderPNGs := &InvaderPNGs{}
	Post(Notice{
		Key:      "agent-notify-check",
		Title:    "agent-notify",
		Subtitle: "notifications are working",
		Body:     "If you can read this, agent-notify can interrupt you when an agent needs you.",
	}, invaderPNGs.PathOfTheInvaderDrawnIn(DefaultColours[agentnotify.FinishedATurn]), sound)
	// Long enough for macOS to have asked, been answered, and delivered.
	Pump(3 * time.Second)
	fmt.Printf("%-16s %s (after asking)\n", "permission",
		theValueOrTheFallback(Status(), "could not be asked"))
	fmt.Printf("%-16s %s\n", "delivery", HowBannersWillArrive(sound))
	// Asked because it is the one thing that was broken while everything else
	// looked perfect: a process that posts and does not answer.
	//
	// The question is asked from a goroutine while THIS thread runs the loop,
	// which is the arrangement the program itself runs in — a main thread in
	// AppKit's hands and everything else beside it. Asking from the thread that
	// is supposed to be answering would answer "no" however healthy it was.
	answered := make(chan bool, 1)
	go func() { answered <- Answers(2 * time.Second) }()
	Pump(2500 * time.Millisecond)
	if <-answered {
		fmt.Printf("%-16s yes — a tap on a banner reaches this program\n", "answers")
	} else {
		fmt.Printf("%-16s NO — banners will arrive and clicking one will be answered by\n", "answers")
		fmt.Printf("%-16s macOS with \"the application is not responding properly\"\n", "")
	}
	fmt.Printf("%-16s a test banner has been posted\n", "notifications")
	fmt.Println()
	switch Status() {
	case "provisional":
		fmt.Println("Provisional means DELIVERED QUIETLY: the notification is in Notification")
		fmt.Println("Centre, with no banner and no sound. To get banners, open Notification")
		fmt.Println("Centre, press \"Keep…\" on one of them and choose \"Deliver Prominently\".")
		fmt.Println("Until you answer that question the app is not in System Settings either.")
	case "authorized":
		fmt.Println("Authorized: banners and sound, tuned in System Settings → Notifications.")
	default:
		fmt.Println("macOS is refusing them. An identifier it has already decided about cannot")
		fmt.Println("be undecided — see the README on signing and bundle identifiers.")
	}
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

func notify() int {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	settings, err := Read(me)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	if !Available() {
		fmt.Fprintln(os.Stderr, "there is no logged-in session here to notify into: this has "+
			"to run in your own session")
		return 1
	}
	if RunningIn() == "" {
		// Not a warning but a refusal, and the difference from the menu bar
		// display is the whole reason these are two programs: there, a bare
		// binary works and merely forgets where its item was; here, asking
		// UNUserNotificationCenter anything without a bundle identifier
		// terminates the process.
		fmt.Fprintf(os.Stderr, "this is the bare binary, and macOS will not take a "+
			"notification from one: run `%s install` and let the session-watcher start "+
			"the copy inside the bundle\n", program)
		return 1
	}

	banners := &Banners{
		Notifier:    NewNotifier(settings.Preview, settings.Colours),
		InvaderPNGs: &InvaderPNGs{},
		Core:        settings.Core,
		Sound:       settings.Sound,
		Logger:      log,
	}
	whatToDoWhenABannerIsTapped = banners.focus

	if !Start() {
		fmt.Fprintln(os.Stderr, "macOS would not take the permission request")
		return 1
	}
	switch Status() {
	case "authorized":
	case "provisional":
		// Working, and quietly: provisional notifications go to Notification
		// Centre with no banner and no sound until somebody answers macOS's own
		// "Keep receiving these?". Said at INFO because it is not a fault — but
		// it is the reason somebody will say they see nothing.
		log.Info("notifications are delivered QUIETLY, to Notification Centre only",
			"promote", "open Notification Centre, press Keep… on one, "+
				"then Deliver Prominently")
	case "":
		log.Warn("macOS would not say whether it will deliver notifications")
	default:
		// Silent otherwise: posted, dropped, and this program reporting that
		// everything is fine.
		log.Warn("macOS will not deliver notifications from this app",
			"status", Status(), "check", program+" check")
	}
	log.Info("notifying", "delivery", HowBannersWillArrive(settings.Sound))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	outcome := make(chan error, 1)
	go func() {
		outcome <- subscribe.Run(ctx, subscribe.Integration{
			Name:     Name,
			Roles:    []string{"display"},
			WakeOn:   WhatToWakeFor(),
			OnChange: banners.Post,
			Logger:   log,
		})
		// Ends the run loop below, which is what lets this process exit.
		Stop()
	}()

	// The loop is the only way back in: a tap on a banner, and everything else
	// macOS ever hands this process, arrives on the main queue. It is asked
	// about rather than assumed, because the failure is otherwise invisible —
	// posting is XPC and goes on working perfectly while nothing answers.
	go func() {
		if !Answers(5 * time.Second) {
			log.Warn("this process is not answering on its main thread: notifications will "+
				"arrive and clicking one will be answered by macOS with "+
				"\"the application is not responding properly\"",
				"check", program+" check")
		}
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
// The message is on this list because it is the BODY of the banner. There is
// no clock in this program and no ages to keep true: a notification is an
// edge, and nothing about time passing is one.
//
// `state_since` came off it in D-73. It was there when this program worked out
// for itself what had moved, by comparing the state_since it remembered per
// session against the one in hand — and D-71 deleted all of that in favour of
// the previous kernel the SDK already carries. Nothing here has read the field
// since, so asking for it was a wake for something nobody would look at (R23).
//
// It is a function so a test can read it, and the test is the point: it moves
// one record field at a time, calls Fresh twice, and fails when a banner comes
// out different for something not named here (D-73).
func WhatToWakeFor() []string {
	return []string{"kernel", "detail", "rank", "name", "cwd", "message"}
}

// Banners is the notifier and what it needs.
type Banners struct {
	Notifier    *Notifier
	InvaderPNGs *InvaderPNGs
	Core        string
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
func (b *Banners) Post(view subscribe.View) error {
	for _, notice := range b.Notifier.Fresh(view.Changed) {
		Post(notice, b.InvaderPNGs.PathOfTheInvaderDrawnIn(notice.Colour), b.Sound)
	}
	return nil
}

// focusTimeout bounds one `agent-notify focus-session`, and it is not a
// performance bound — it is there so that this goroutine cannot be lost.
//
// Generous on purpose. Core walks the container order and gives each step five
// seconds of its own, so a focus through a window manager and a multiplexer can
// legitimately take three times that. A bound shorter than what it is waiting
// on would kill focuses that were about to succeed, which is worse than the
// unbounded wait it replaces.
const focusTimeout = 30 * time.Second

// focus is what tapping a banner does.
//
// It runs the same `focus-session` every other integration runs, for the reason
// D-37 gives: where a session lives is the containers' business, and anything
// that went and looked would be a second implementation of it, wrong in its own
// way (R24).
func (b *Banners) focus(key string) {
	if _, err := tool.Run(b.Core, focusTimeout, "focus-session", "--quiet", key); err != nil {
		b.Logger.Warn("focusing", "session", key, "problem", err.Error())
	}
}
