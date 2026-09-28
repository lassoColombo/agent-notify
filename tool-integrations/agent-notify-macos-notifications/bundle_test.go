package main

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lassoColombo/agent-notify/subscribe"
)

// The launch agent names the binary inside the bundle, and the table names no
// binary at all: `binary` means "core may run this".
func TestTheLaunchAgentPointsInsideTheBundle(t *testing.T) {
	bundle := subscribe.Bundle{Path: "/Applications/x.app", Program: program}
	plist := subscribe.LaunchAgentPlist(Identifier, bundle.PathOfTheBinaryInside())
	if !strings.Contains(plist, ".app/Contents/MacOS/"+program) {
		t.Errorf("the launch agent names %q, which is not inside a bundle", plist)
	}
	if table := theConfigTableToAdd(""); strings.Contains(table, "binary") {
		t.Errorf("the table still names a binary, so core would try to run it:\n%s", table)
	}
}

// A bare binary must be able to tell that it is one, or the warning never
// fires.
func TestThisTestBinaryIsNotBundled(t *testing.T) {
	if got := RunningIn(); got != "" {
		t.Errorf("RunningIn() = %q, but `go test` does not run from a bundle", got)
	}
}

// TestTheIconIsDrawnInThePalette.
//
// The icon is the one drawing in this program that nobody is looking at while
// it is made — it is written at install time, into a bundle, and the next place
// it appears is a notification banner — so the palette it is drawn in is worth
// a test rather than a glance.
//
// It also pins the colour space. AppKit draws in Apple's calibrated RGB, and a
// PNG written straight out of that holds `#b693e1` where `#c4a7e7` was asked
// for: right on screen, because the profile travels with the file, and wrong to
// everything that reads the pixels.
func TestTheIconIsDrawnInThePalette(t *testing.T) {
	if !Available() {
		t.Skip("nothing to draw with here")
	}
	into := t.TempDir()
	if !WriteIconset(into) {
		t.Fatal("the iconset was not drawn")
	}
	drawn := decoded(t, filepath.Join(into, "icon_512x512.png"))

	// The mark is Iris, and deliberately neither hue a state wears: an icon in
	// Love would read as an agent waiting for you every time it appeared.
	if got := pixel(drawn, 256, 256); got != iris {
		t.Errorf("the mark is %06x, want Iris %06x", got, iris)
	}
	// The canvas is the palette's night, Overlay at the top down to Base at the
	// bottom. Within one step, because a gradient is sampled a few pixels away
	// from each end.
	for _, one := range []struct {
		y    int
		want uint32
		name string
	}{{40, 0x26233a, "Overlay"}, {470, 0x191724, "Base"}} {
		if got := pixel(drawn, 256, one.y); !alike(got, one.want, 2) {
			t.Errorf("the canvas at y=%d is %06x, want %s %06x", one.y, got, one.name, one.want)
		}
	}
	// And the corner is nothing at all: an app icon is a rounded rect with
	// transparent shoulders, not a square.
	if _, _, _, opacity := drawn.At(2, 2).RGBA(); opacity != 0 {
		t.Errorf("the corner has alpha %d, want a rounded icon", opacity>>8)
	}
}

func decoded(t *testing.T, path string) image.Image {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer file.Close()
	drawn, err := png.Decode(file)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	return drawn
}

// pixel is one point as 0xRRGGBB.
func pixel(drawn image.Image, x, y int) uint32 {
	red, green, blue, _ := drawn.At(x, y).RGBA()
	return (red>>8)<<16 | (green>>8)<<8 | blue>>8
}

func alike(got, want, step uint32) bool {
	for shift := 0; shift <= 16; shift += 8 {
		one, other := (got>>shift)&0xff, (want>>shift)&0xff
		if one > other+step || other > one+step {
			return false
		}
	}
	return true
}

// iris is the one colour the mark is drawn in.
const iris = 0xc4a7e7

// invader is the sprite, and it is written down twice on purpose: here, and in
// the Objective-C that draws it. There are a dozen arrangements of this alien
// about, they differ in the antennae and the legs, and this is the one that was
// asked for.
var invader = []string{
	"..X.....X..",
	"X..X...X..X",
	"X.XXXXXXX.X",
	"XXXXXXXXXXX",
	"XXX.XXX.XXX",
	"XXXXXXXXXXX",
	".XXXXXXXXX.",
	"..X.....X..",
	".X.......X.",
}

// TestTheIconIsTheInvader reads the sprite back out of the drawn icon.
//
// Cell by cell, off the picture, rather than by sampling a pixel or two: a
// sprite is a shape, the thing that can go wrong with it is being one row out
// or a mirror image of itself, and neither of those is visible in a spot check.
func TestTheIconIsTheInvader(t *testing.T) {
	if !Available() {
		t.Skip("nothing to draw with here")
	}
	into := t.TempDir()
	if !WriteIconset(into) {
		t.Fatal("the iconset was not drawn")
	}
	drawn := decoded(t, filepath.Join(into, "icon_512x512.png"))

	// Where the mark is, without knowing anything about how big it was drawn.
	box := drawn.Bounds()
	left, right, top, bottom := box.Max.X, box.Min.X, box.Max.Y, box.Min.Y
	for y := box.Min.Y; y < box.Max.Y; y++ {
		for x := box.Min.X; x < box.Max.X; x++ {
			if pixel(drawn, x, y) != iris {
				continue
			}
			left, right = min(left, x), max(right, x)
			top, bottom = min(top, y), max(bottom, y)
		}
	}
	across, down := right-left+1, bottom-top+1
	if across <= 0 || down <= 0 {
		t.Fatal("there is no mark on the icon at all")
	}

	// A sprite is square cells or it is not a sprite.
	cell := across / len(invader[0])
	if cell != down/len(invader) || cell*len(invader[0]) != across || cell*len(invader) != down {
		t.Fatalf("the mark is %dx%d, which is not %d by %d whole square cells",
			across, down, len(invader[0]), len(invader))
	}

	for row := range invader {
		read := ""
		for column := range invader[row] {
			if pixel(drawn, left+column*cell+cell/2, top+row*cell+cell/2) == iris {
				read += "X"
			} else {
				read += "."
			}
		}
		if read != invader[row] {
			t.Errorf("row %d of the icon reads %q, want %q", row, read, invader[row])
		}
	}
}
