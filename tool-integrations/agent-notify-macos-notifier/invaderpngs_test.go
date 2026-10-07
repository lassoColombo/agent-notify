package main

import (
	"os"
	"strings"
	"testing"

	"github.com/lassoColombo/agent-notify/session"
)

// TestTheMarkIsDrawnInTheStatesColour, which is the whole point of it: a banner
// and the bar are about the same thing at the same moment, and they say it in
// the same colour.
func TestTheMarkIsDrawnInTheStatesColour(t *testing.T) {
	marks := &InvaderPNGs{Size: 128, Directory: t.TempDir()}
	path := marks.PathOfTheInvaderDrawnIn(DefaultColours[session.BlockedOnYou])
	if path == "" {
		t.Fatal("no mark was drawn")
	}
	drawn := decoded(t, path)
	// The middle of the sprite is a lit cell in every arrangement of it.
	if got := pixel(drawn, 64, 64); got != 0xeb6f92 {
		t.Errorf("the mark is %06x, want Love eb6f92", got)
	}
}

// TestEachColourIsDrawnOnce. A notification arrives at whatever rate agents
// change state, and redrawing a PNG for every one of them would be work nobody
// asked for.
func TestEachColourIsDrawnOnce(t *testing.T) {
	marks := &InvaderPNGs{Size: 64, Directory: t.TempDir()}
	first := marks.PathOfTheInvaderDrawnIn("0xffeb6f92")
	if first == "" {
		t.Fatal("no mark was drawn")
	}
	stat, err := os.Stat(first)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	again := marks.PathOfTheInvaderDrawnIn("0xffeb6f92")
	if again != first {
		t.Errorf("the second ask gave %q, want the file already drawn", again)
	}
	after, err := os.Stat(again)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !after.ModTime().Equal(stat.ModTime()) {
		t.Errorf("the file was drawn again")
	}
}

// TestAMarkTakenAwayIsDrawnAgain is the measured behaviour of an attachment:
// macOS MOVES the file into its own store when the request is added, so the
// second notification about a state finds the path empty. A cache that only
// remembered it had drawn something would post one picture and then none.
func TestAMarkTakenAwayIsDrawnAgain(t *testing.T) {
	marks := &InvaderPNGs{Size: 64, Directory: t.TempDir()}
	first := marks.PathOfTheInvaderDrawnIn("0xfff6c177")
	if first == "" {
		t.Fatal("no mark was drawn")
	}
	if err := os.Remove(first); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	again := marks.PathOfTheInvaderDrawnIn("0xfff6c177")
	if again == "" {
		t.Fatal("nothing was drawn the second time")
	}
	if _, err := os.Stat(again); err != nil {
		t.Errorf("the mark was not drawn again: %v", err)
	}
}

// TestAColourNobodyCanParseIsNoPictureAndNotAPanic. A banner with no picture is
// still a banner; the words are the part that matters (R13).
func TestAColourNobodyCanParseIsNoPictureAndNotAPanic(t *testing.T) {
	marks := &InvaderPNGs{Size: 64, Directory: t.TempDir()}
	for _, colour := range []string{"", "labelColor", "0xzz", "#eb6f92"} {
		if got := marks.PathOfTheInvaderDrawnIn(colour); got != "" {
			t.Errorf("For(%q) = %q, want nothing to attach", colour, got)
		}
	}
}

// TestTheAlphaIsDropped. Colours are written `0xAARRGGBB` everywhere in this
// system, and a banner's picture is composited by macOS onto a surface nobody
// here knows the colour of — so a half-transparent sprite is a sprite nobody
// can read.
func TestTheAlphaIsDropped(t *testing.T) {
	for _, one := range []struct {
		colour string
		want   uint32
	}{
		{"0xffeb6f92", 0xeb6f92},
		{"0xcce0def4", 0xe0def4},
		{"0x00eb6f92", 0xeb6f92},
		{"eb6f92", 0xeb6f92},
	} {
		got, ok := theRGBWithoutTheAlpha(one.colour)
		if !ok || got != one.want {
			t.Errorf("rgb(%q) = %06x, %v; want %06x", one.colour, got, ok, one.want)
		}
	}
}

// TestAColourAgentNotifyWritesIsAColourThisCanDraw, for every default, so that
// the two tables cannot drift into a state that has no picture.
func TestEveryDefaultColourCanBeDrawn(t *testing.T) {
	for kernel, colour := range DefaultColours {
		if _, ok := theRGBWithoutTheAlpha(colour); !ok {
			t.Errorf("%s is %q, which cannot be drawn", kernel, colour)
		}
	}
}

// TestOneDirectoryForEveryRun. A fresh temporary directory per process is the
// obvious way to write this and it litters: one empty directory per restart,
// for ever, in a place nothing sweeps until the machine reboots.
func TestTheInvaderPNGsShareOneFixedDirectory(t *testing.T) {
	if !strings.HasSuffix(whereTheInvaderPNGsGo(), "agent-notify-invaders") {
		t.Errorf("they live in %q, which is not a fixed name", whereTheInvaderPNGsGo())
	}
	where := t.TempDir()
	first := &InvaderPNGs{Size: 64, Directory: where}
	second := &InvaderPNGs{Size: 64, Directory: where}
	a := first.PathOfTheInvaderDrawnIn("0xffeb6f92")
	b := second.PathOfTheInvaderDrawnIn("0xffeb6f92")
	if a != b {
		t.Errorf("two runs drew %q and %q, want the same file", a, b)
	}
	entries, err := os.ReadDir(where)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("%d files for one colour drawn twice", len(entries))
	}
}
