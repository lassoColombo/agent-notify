package main

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
)

// The mark on a banner: the invader, 11 across and 9 down, on a rounded plum
// tile.
//
// It was written out again rather than shared when the menu bar display wore the
// same sprite, because nine lines of ASCII was a cheaper coupling than a module
// between two programs that never spoke. That display is gone (D-87) and this is
// the only copy left.
//
// Read off the picture it came from rather than typed from memory. There are a
// dozen arrangements of this sprite about and they differ in the antennae and
// the legs, which are exactly the parts anybody looks at.
var invader = [9]string{
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

// The canvas is Rosé Pine's own night, Overlay down to Base, and NOT graphite:
// a grey tile is indistinguishable from the dozen other grey tiles in a column
// of banners, and the whole job of this picture is to be picked out of one
// without being read.
const (
	overlay = 0x26233a
	base    = 0x191724
)

// DrawInvaderPNG writes the sprite at one size, in one 0xRRGGBB colour, to a
// PNG.
//
// This used to be AppKit — NSBezierPath, NSGradient and a bitmap rep — and the
// only reason it could be was that this program already had to link Cocoa to
// post a notification at all. It does not any more (alerter.go), and drawing
// nine rows of squares is not a reason to carry a cgo toolchain, an
// Objective-C file and the Xcode command line tools as a build dependency. So
// it is image/png, which is in the standard library, and the file this writes
// is byte-for-byte a thing anything can read.
//
// The corner radius is 0.2237 of the width, which is the ratio macOS uses, and
// the 5.5% inset is the margin every app icon leaves so that its corners are
// not flush with the next icon's.
func DrawInvaderPNG(path string, rgb uint32, size int) bool {
	if size <= 0 {
		return false
	}
	canvas := image.NewNRGBA(image.Rect(0, 0, size, size))
	side := float64(size)
	margin := side * 0.055
	low, high := margin, side-margin
	radius := (high - low) * 0.2237

	for y := 0; y < size; y++ {
		// Top to bottom, which is what the AppKit original asked for by
		// drawing its gradient at -90 degrees.
		tile := mix(overlay, base, clampTo((float64(y)+0.5-low)/(high-low), 0, 1))
		for x := 0; x < size; x++ {
			if covered := coverage(float64(x)+0.5, float64(y)+0.5, low, high, radius); covered > 0 {
				canvas.SetNRGBA(x, y, color.NRGBA{tile.R, tile.G, tile.B, uint8(covered*255 + 0.5)})
			}
		}
	}

	// Whole pixels, always. A sprite is a grid of squares and the one thing it
	// cannot survive is being drawn on fractional boundaries — half-lit edges
	// at sixteen points read as a mistake rather than as pixel art — so the
	// cell is rounded to an integer before anything is placed, and never goes
	// below one.
	cell := math.Round(side * 0.0545)
	if cell < 1 {
		cell = 1
	}
	left := int(math.Round((side - cell*float64(len(invader[0]))) / 2))
	top := int(math.Round((side - cell*float64(len(invader))) / 2))
	mark := color.NRGBA{uint8(rgb >> 16), uint8(rgb >> 8), uint8(rgb), 0xff}
	for row := range invader {
		for column, lit := range invader[row] {
			if lit != 'X' {
				continue
			}
			for y := 0; y < int(cell); y++ {
				for x := 0; x < int(cell); x++ {
					canvas.SetNRGBA(left+column*int(cell)+x, top+row*int(cell)+y, mark)
				}
			}
		}
	}

	file, err := os.Create(path)
	if err != nil {
		return false
	}
	if err := png.Encode(file, canvas); err != nil {
		file.Close()
		os.Remove(path)
		return false
	}
	return file.Close() == nil
}

// coverage is how much of the pixel at (x, y) the rounded tile covers, 0 to 1.
// Antialiasing the corners is one line of arithmetic, and the alternative is a
// staircase on the one part of the picture anybody looks at the edge of.
func coverage(x, y, low, high, radius float64) float64 {
	if x < low || x > high || y < low || y > high {
		return 0
	}
	away := math.Hypot(x-clampTo(x, low+radius, high-radius), y-clampTo(y, low+radius, high-radius))
	return clampTo(radius+0.5-away, 0, 1)
}

func clampTo(value, low, high float64) float64 { return math.Max(low, math.Min(high, value)) }

// mix is one step along the gradient, in the 0xRRGGBB the palette is written in.
func mix(from, to uint32, at float64) color.NRGBA {
	band := func(shift uint) uint8 {
		start, end := float64((from>>shift)&0xff), float64((to>>shift)&0xff)
		return uint8(start + (end-start)*at + 0.5)
	}
	return color.NRGBA{band(16), band(8), band(0), 0xff}
}
