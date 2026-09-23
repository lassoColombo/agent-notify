package main

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework ApplicationServices
#include <stdlib.h>
#include "menubar.h"
*/
import "C"

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unsafe"
)

// This file is the only place in this program that knows AppKit exists. It is
// thin on purpose: everything above it deals in a WhatTheBarShows, and below
// it in menubar.m deals in a document it was handed.

// RunningIn is the bundle identifier this process has, or "" for a bare binary.
func RunningIn() string {
	identifier := C.MenuBarBundleIdentifier()
	if identifier == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(identifier))
	return C.GoString(identifier)
}

// Available reports whether there is a window server to draw on.
//
// Asked before Start, because AppKit's answer to being started without one is
// to abort the process — and the session-watcher is perfectly capable of
// starting this display somewhere there is no screen.
func Available() bool { return C.MenuBarAvailable() == 1 }

// Start puts the item on the bar. It must be called from the main goroutine,
// which must be locked to the main thread — see main.go, where it is.
func Start(font string) {
	var name *C.char
	if font != "" {
		name = C.CString(font)
		defer C.free(unsafe.Pointer(name))
	}
	C.MenuBarStart(name)
}

// Draw puts one of these on the bar. Safe from any goroutine.
func Draw(whatToShow WhatTheBarShows) error {
	encoded, err := json.Marshal(whatToShow)
	if err != nil {
		return err
	}
	document := C.CString(string(encoded))
	defer C.free(unsafe.Pointer(document))
	C.MenuBarApply(document)
	return nil
}

// Run is the AppKit loop and does not return until Stop.
func Run() { C.MenuBarRun() }

// Stop takes the item off the bar and ends the loop.
func Stop() { C.MenuBarStop() }

// Pump gives AppKit the calling thread for a while. It exists for tests: a test
// needs the bar to have serviced what it asked for, and it cannot hand over its
// thread for ever to find out.
func Pump(long time.Duration) { C.MenuBarPump(C.double(long.Seconds())) }

// OnTheBar is what the accessibility API says this process is showing, which is
// the only way to find out. An error means nobody can tell — accessibility has
// not been granted to whatever is responsible for this process — and that is
// not the same as the item being absent (R27).
func OnTheBar() ([]string, error) {
	answer := C.MenuBarOnTheBar()
	if answer == nil {
		return nil, errors.New("the accessibility API will not say what is on the menu bar: " +
			"nothing has granted it to whatever started this process")
	}
	defer C.free(unsafe.Pointer(answer))
	text := C.GoString(answer)
	if text == "" {
		return nil, nil
	}
	return strings.Split(text, "\n"), nil
}

// ColourKnown reports whether AppKit can draw a colour by that name, so that a
// name nobody can draw is refused when the config is read.
func ColourKnown(name string) bool {
	spelling := C.CString(name)
	defer C.free(unsafe.Pointer(spelling))
	return C.MenuBarColourKnown(spelling) == 1
}

// FontKnown reports whether a font of that name can be had. An empty name is
// the menu bar's own and is always fine.
func FontKnown(name string) bool {
	spelling := C.CString(name)
	defer C.free(unsafe.Pointer(spelling))
	return C.MenuBarFontKnown(spelling) == 1
}

// WriteIconset draws the app icon into a directory as the ten PNGs an .iconset
// is made of.
func WriteIconset(directory string) bool {
	into := C.CString(directory)
	defer C.free(unsafe.Pointer(into))
	return C.MenuBarWriteIconset(into) == 1
}

// SymbolKnown reports whether this macOS has an SF Symbol of that name. An
// empty name is "no symbol, use the glyph" and is always fine.
func SymbolKnown(name string) bool {
	spelling := C.CString(name)
	defer C.free(unsafe.Pointer(spelling))
	return C.MenuBarSymbolKnown(spelling) == 1
}

// whatToDoWhenAMenuRowIsChosen is a package-level variable because an AppKit
// action carries no context of ours, and it is set once before the item goes on
// the bar.
var whatToDoWhenAMenuRowIsChosen = func(string) {}

//export goMenuRowWasChosen
func goMenuRowWasChosen(key *C.char) { aMenuRowWasChosen(C.GoString(key)) }

// aMenuRowWasChosen is goMenuRowWasChosen with the C taken off, which is where
// a test can reach it: cgo is not allowed in the test file of a package that
// exports anything, so the alternative to this line is not testing the callback
// at all.
func aMenuRowWasChosen(key string) {
	// Handed to a goroutine, because goMenuRowWasChosen runs on the main thread
	// while the menu is still tracking, and whatever focusing a session costs
	// is not allowed to be paid with a menu open on the screen.
	go whatToDoWhenAMenuRowIsChosen(key)
}
