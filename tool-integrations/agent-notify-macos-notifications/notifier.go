package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework UserNotifications
#include <stdlib.h>
#include "notifier.h"
*/
import "C"

import (
	"fmt"
	"time"
	"unsafe"
)

// This file is the only place in this program that knows AppKit exists. It is
// thin on purpose: everything above it decides whether there is anything to
// say, and everything below it in notifier.m says it.

// Available reports whether there is a logged-in window session.
//
// Asked before Start, because AppKit's answer to being started without one is
// to abort the process — and the session-watcher is perfectly capable of
// starting this somewhere there is no screen.
func Available() bool { return C.NotifierAvailable() == 1 }

// Start asks for permission and reports whether notifications are possible at
// all. False means this is a bare binary, where asking would terminate the
// process rather than fail. It must be called from the main goroutine, which
// must be locked to the main thread — see main.go, where it is.
func Start() bool { return C.NotifierStart() == 1 }

// Post posts one notification about one session, with a picture beside it or
// "" for none, and with whatever sound has been settled on. Safe from any
// goroutine.
func Post(notice Notice, mark string, sound Sound) {
	session := C.CString(notice.Key)
	heading := C.CString(notice.Title)
	under := C.CString(notice.Subtitle)
	text := C.CString(notice.Body)
	picture := C.CString(mark)
	named := C.CString(sound.Name)
	defer C.free(unsafe.Pointer(named))
	defer C.free(unsafe.Pointer(session))
	defer C.free(unsafe.Pointer(heading))
	defer C.free(unsafe.Pointer(under))
	defer C.free(unsafe.Pointer(text))
	defer C.free(unsafe.Pointer(picture))
	plays := C.int(0)
	if sound.Plays {
		plays = 1
	}
	C.NotifierPost(session, heading, under, text, picture, plays, named)
}

// DrawInvaderPNG writes the sprite at one size, in one 0xRRGGBB colour, to a PNG.
func DrawInvaderPNG(path string, colour uint32, size int) bool {
	where := C.CString(path)
	defer C.free(unsafe.Pointer(where))
	return C.NotifierDrawInvaderPNG(where, C.uint(colour), C.double(size)) == 1
}

// Status is what macOS has decided about notifications from this bundle. ""
// means nobody could be asked — a bare binary, or a system that did not answer.
func Status() string {
	answer := C.NotifierStatus()
	if answer == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(answer))
	return C.GoString(answer)
}

// RunningIn is the bundle identifier this process has, or "" for a bare binary.
func RunningIn() string {
	identifier := C.NotifierBundleIdentifier()
	if identifier == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(identifier))
	return C.GoString(identifier)
}

// Run is the AppKit loop and does not return until Stop.
func Run() { C.NotifierRun() }

// Stop ends the loop.
func Stop() { C.NotifierStop() }

// Answers reports whether this process is draining its main queue, which is
// where every callback macOS has arrives — a tap on a banner above all. A
// program that posts and does not answer is the failure this exists to catch,
// and it is invisible from outside: the banners still arrive.
func Answers(long time.Duration) bool { return C.NotifierAnswers(C.double(long.Seconds())) == 1 }

// Pump gives AppKit the calling thread for a while. macOS answers questions
// about permission on another queue, and a program that asked and exited would
// never hear.
func Pump(long time.Duration) { C.NotifierPump(C.double(long.Seconds())) }

// WriteIconset draws the app icon into a directory as the ten PNGs an .iconset
// is made of.
func WriteIconset(directory string) bool {
	where := C.CString(directory)
	defer C.free(unsafe.Pointer(where))
	return C.NotifierWriteIconset(where) == 1
}

// whatToDoWhenABannerIsTapped is a package-level variable because
// a notification response carries no context of ours, and it is set once before
// anything is posted.
var whatToDoWhenABannerIsTapped func(key string)

//export goBannerWasTapped
func goBannerWasTapped(key *C.char) { aBannerWasTapped(C.GoString(key)) }

// aBannerWasTapped is goBannerWasTapped with the C taken off, which is where a
// test can reach it: cgo is not allowed in the test file of a package that
// exports anything, so the alternative to this line is not testing the callback
// at all.
func aBannerWasTapped(key string) {
	if whatToDoWhenABannerIsTapped == nil {
		return
	}
	// On a goroutine of its own: this is called from AppKit's thread, and what
	// it goes on to do is run a program.
	go whatToDoWhenABannerIsTapped(key)
}

// HowBannersWillArrive is a one-line account of what macOS will do with what
// this posts,
// for `check` and for the log line at startup.
//
// `sound` is this program's own setting and not macOS's: a line that said
// "banners and sound" while every notification went out silent would be the
// first thing somebody read and the last thing they believed. Provisional is
// the other way round — quiet because macOS says so — and the two are worth
// telling apart when somebody is working out why they heard nothing.
func HowBannersWillArrive(sound Sound) string {
	switch status := Status(); status {
	case "authorized":
		return "banners, " + sound.Describe()
	case "provisional":
		return "quietly, to Notification Centre only"
	case "":
		return "macOS would not say"
	default:
		return fmt.Sprintf("not at all (%s)", status)
	}
}
