package main

import (
	"image"
	"image/png"
	"os"
	"testing"
)

// decoded reads a PNG back as pixels, because every claim in this file is
// about what ends up in the file rather than about what the drawing code
// believed it was doing.
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

// pixel is one 0xRRGGBB out of a decoded image, alpha dropped.
func pixel(drawn image.Image, x, y int) uint32 {
	r, g, b, _ := drawn.At(x, y).RGBA()
	return (r>>8)<<16 | (g>>8)<<8 | b>>8
}

// TestTheColourSurvivesTheRoundTrip. The AppKit version drew in a calibrated
// space and had to be converted to sRGB on the way out, or `#c4a7e7` landed on
// disk as `#b693e1` — correct on a colour-managed Mac and wrong to everything
// that reads the pixels, this test included. image/png has no such space, and
// this is what says so.
func TestTheMarkIsExactlyTheColourItWasAskedFor(t *testing.T) {
	path := t.TempDir() + "/mark.png"
	if !DrawInvaderPNG(path, 0xc4a7e7, 512) {
		t.Fatal("nothing was drawn")
	}
	drawn := decoded(t, path)
	if got := pixel(drawn, 256, 256); got != 0xc4a7e7 {
		t.Errorf("the middle of the sprite is %06x, want Iris c4a7e7", got)
	}
}

// TestTheTileIsRoundedAndTheCornersAreTransparent, which is what makes this
// read as an icon rather than as a square somebody pasted on a banner.
func TestTheCornersAreTransparent(t *testing.T) {
	path := t.TempDir() + "/mark.png"
	if !DrawInvaderPNG(path, 0xeb6f92, 512) {
		t.Fatal("nothing was drawn")
	}
	drawn := decoded(t, path)
	for _, corner := range [][2]int{{0, 0}, {511, 0}, {0, 511}, {511, 511}} {
		if _, _, _, alpha := drawn.At(corner[0], corner[1]).RGBA(); alpha != 0 {
			t.Errorf("the pixel at %v has alpha %d, want nothing drawn outside the tile",
				corner, alpha)
		}
	}
	if _, _, _, alpha := drawn.At(256, 256).RGBA(); alpha == 0 {
		t.Error("the middle is transparent, so there is no tile at all")
	}
}

// TestTheTileIsTheNightGradientAndNotGrey. A grey tile is indistinguishable
// from the dozen other grey tiles in a column of banners, and the whole job of
// this picture is to be picked out of one without being read.
func TestTheTileIsDarkerAtTheFootThanAtTheTop(t *testing.T) {
	path := t.TempDir() + "/mark.png"
	if !DrawInvaderPNG(path, 0xeb6f92, 512) {
		t.Fatal("nothing was drawn")
	}
	drawn := decoded(t, path)
	// Clear of the sprite, which is centred: the margin above and below it.
	top, foot := pixel(drawn, 256, 60), pixel(drawn, 256, 450)
	blue := func(one uint32) uint32 { return one & 0xff }
	red := func(one uint32) uint32 { return one >> 16 }
	if blue(top) <= blue(foot) {
		t.Errorf("the tile runs %06x to %06x, want it darker at the foot", top, foot)
	}
	for _, one := range []uint32{top, foot} {
		if blue(one) <= red(one) {
			t.Errorf("%06x is grey, and a grey tile is every other grey tile in a "+
				"column of banners", one)
		}
	}
}

// TestASizeNobodyCanDrawIsNoFileAndNotAPanic.
func TestANonsenseSizeDrawsNothing(t *testing.T) {
	path := t.TempDir() + "/mark.png"
	if DrawInvaderPNG(path, 0xeb6f92, 0) {
		t.Error("a zero-sized mark was reported drawn")
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("a file was left behind for a mark that was not drawn")
	}
}
