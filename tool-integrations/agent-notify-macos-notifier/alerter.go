package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

)

// This file is the whole of what this program knows about macOS, and it is an
// exec rather than a framework call.
//
// What it replaces was four hundred lines of Objective-C against
// UNUserNotificationCenter, and the reason it was that much is that macOS will
// not take a notification from a process without a bundle identity: the
// program had to be wrapped in a .app, the .app had to be signed with a
// keychain identity made by hand with openssl, the bundle held a COPY of the
// binary so every rebuild needed a reinstall, and a decision macOS once made
// about the identifier could never be unmade (D-53). alerter is that bundle,
// maintained by somebody else, signed and notarised by Apple, and on PATH.
//
// Three things were measured before this was written [2026-10-07, macOS 26.6.2,
// alerter 26.5]:
//
//   - A SECOND BANNER IN THE SAME GROUP REAPS THE FIRST. The superseded
//     alerter exits by itself, printing @CLOSED. So nothing here tracks
//     children: one banner per session, replaced rather than stacked (D-54),
//     is what --group already means.
//   - AN ALERTER OUTLIVES WHATEVER STARTED IT. Launched detached it is
//     reparented to pid 1 and goes on holding its banner. That is what makes
//     this a render rather than a resident process.
//   - A TAP IS @CONTENTCLICKED ON STDOUT, and the close button, a timeout and
//     being replaced are three other words. Only the first one focuses
//     anything: the rest are somebody declining to be interrupted, and acting
//     on those would take you to a session you dismissed.
const alerterProgram = "alerter"

// tapped is what alerter prints when somebody tapped the banner itself. It is
// the ONLY outcome that focuses a session.
const tapped = "@CONTENTCLICKED"

// hold is the subcommand the detached half runs. Not for a person: it is here
// because a banner has to be held by something, and this program is the
// something.
const holdCommand = "hold"

// Alerter is where alerter is: what install wrote into the table, or PATH, or
// the reason there will be no banners.
//
// The written one wins because nothing core starts has your shell's PATH
// (D-67), and it is checked rather than trusted: a brew upgrade that moves it
// would otherwise be a display that silently stops interrupting anybody.
func Alerter(configured string) (string, error) {
	if configured != "" {
		if _, err := os.Stat(configured); err != nil {
			return "", fmt.Errorf("[integration.%s.settings] alerter = %q is not there: %w",
				Name, configured, err)
		}
		return configured, nil
	}
	path, err := exec.LookPath(alerterProgram)
	if err != nil {
		return "", fmt.Errorf("%s is what posts a notification on this machine, and it is "+
			"neither written in [integration.%s.settings] alerter nor on PATH: "+
			"brew install vjeantet/tap/alerter", alerterProgram, Name)
	}
	return path, nil
}

// theArgumentsFor is one banner, in alerter's spelling.
func theArgumentsFor(notice Notice, mark string, sound Sound) []string {
	arguments := []string{
		// The session, which is also the identity of the banner: a second
		// notice about one session replaces the first rather than stacking
		// under it, and reaps the alerter that was holding it.
		"--group", notice.Key,
		"--title", notice.Title,
		"--subtitle", notice.Subtitle,
		"--message", notice.Body,
	}
	if mark != "" {
		// Both, and the same file: --content-image is the picture beside the
		// text, --app-icon is the badge on it. The invader in the colour of
		// what just happened is the one thing that makes a banner recognisable
		// as this system's before any of it has been read.
		arguments = append(arguments, "--content-image", mark, "--app-icon", mark)
	}
	switch {
	case !sound.Plays:
	case sound.Name == "":
		arguments = append(arguments, "--sound", "default")
	default:
		arguments = append(arguments, "--sound", sound.Name)
	}
	return arguments
}

// Post puts one banner up and lets go of it.
//
// What holds it is a detached copy of this program, because a tap arrives when
// it arrives — a minute later, an hour later — and a render has five seconds
// to return before core kills it. Nothing is waited for and nothing is
// remembered: the child is reparented to launchd and answers for its own
// banner.
func Post(alerter string, notice Notice, mark string, sound Sound) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	held := exec.Command(self, append([]string{holdCommand, notice.Key, alerter},
		theArgumentsFor(notice, mark, sound)...)...)
	// Its own session, so that it survives this process and whatever started
	// it, and no inherited pipes, so that core's render is not waiting on a
	// file descriptor a banner is holding open.
	held.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return held.Start()
}

// hold is the detached half: it runs alerter, which does not return until
// somebody has done something about the banner or a later banner for the same
// session has replaced it, and then does what they asked for.
func hold(arguments []string) int {
	if len(arguments) < 3 {
		fmt.Fprintf(os.Stderr, "%s %s: the session, alerter, and then alerter's own arguments\n",
			program, holdCommand)
		return 2
	}
	log := me.Logger()

	key, alerter := arguments[0], arguments[1]
	said, err := exec.Command(alerter, arguments[2:]...).Output()
	if err != nil {
		log.Warn("holding a banner", "session", key, "problem", err.Error())
		return 1
	}
	if strings.TrimSpace(string(said)) != tapped {
		return 0
	}
	// What tapping a banner does: `agent-notify focus-session`, and not a
	// second implementation of where a session lives (R24).
	if err := me.Focus(key); err != nil {
		log.Warn("focusing", "session", key, "problem", err.Error())
		return 1
	}
	return 0
}
