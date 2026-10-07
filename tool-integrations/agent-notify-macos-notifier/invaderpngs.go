package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// InvaderPNGs is the picture on a banner: this program's sprite, in the colour of the
// state the banner is about.
//
// It is a cache and it has to be, because of how macOS takes an attachment: the
// file is MOVED into the notification system's own store when the request is
// added, so a path that worked a moment ago is a path to nothing. Drawing on
// every notification would be correct and wasteful; drawing on every
// notification whose file has gone is correct and costs one `stat`.
//
// Nothing is ever cleaned up, and that is deliberate — but only because the
// directory is FIXED. At most one file per colour ever exists, each about
// thirty kilobytes, and a restart reuses them. The first version made a fresh
// `MkdirTemp` per process, which is the same reasoning with one word wrong: it
// left a directory behind on every start, and a day of restarting a display
// left thirty-nine.
type InvaderPNGs struct {
	// Size is how big the picture is drawn. A banner shows it small; this is
	// generous because the same file is what Notification Centre shows when
	// somebody opens the banner up, and because a sprite that has to be scaled
	// down is far better than one being scaled up.
	Size int
	// Directory they are kept. Empty is one fixed directory under TMPDIR, which is
	// per-user on macOS — and fixed on purpose: a fresh `MkdirTemp` per process
	// leaves one directory behind on every restart, and thirty-nine of them
	// after a day of restarting a display. Tests set it to keep out of each
	// other's way.
	Directory string

	mu                      sync.Mutex
	theDirectoryHasBeenMade bool
}

// whereTheInvaderPNGsGo is the directory an InvaderPNGs uses when it is not
// told one.
func whereTheInvaderPNGsGo() string {
	return filepath.Join(os.TempDir(), "agent-notify-invaders")
}

// invaderPNGSizeInPixels is 512: four device pixels per sprite cell at the size
// a banner draws it, which is enough that the cells stay square after the
// system's own scaling.
const invaderPNGSizeInPixels = 512

// PathOfTheInvaderDrawnIn is the file to attach for a state's colour, or "" when
// there is nothing to attach — which is not an error. A banner with no picture
// is still a banner; the words are the part that matters.
func (m *InvaderPNGs) PathOfTheInvaderDrawnIn(colour string) string {
	rgb, ok := theRGBWithoutTheAlpha(colour)
	if !ok {
		return ""
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Directory == "" {
		m.Directory = whereTheInvaderPNGsGo()
	}
	if !m.theDirectoryHasBeenMade {
		if err := os.MkdirAll(m.Directory, 0o700); err != nil {
			return ""
		}
		m.theDirectoryHasBeenMade = true
	}
	size := m.Size
	if size <= 0 {
		size = invaderPNGSizeInPixels
	}

	path := filepath.Join(m.Directory, fmt.Sprintf("%06x.png", rgb))
	if _, err := os.Stat(path); err == nil {
		return path
	}
	if !DrawInvaderPNG(path, rgb, size) {
		return ""
	}
	return path
}

// theRGBWithoutTheAlpha takes the `0xAARRGGBB` this system writes colours in
// and gives back the three bytes that are drawn. The alpha is dropped rather than honoured: a
// banner's picture is composited by macOS onto a surface nobody here knows the
// colour of, and a half-transparent sprite on it is a sprite nobody can read.
func theRGBWithoutTheAlpha(colour string) (uint32, bool) {
	text := strings.TrimPrefix(strings.TrimPrefix(colour, "0x"), "0X")
	if len(text) != 8 && len(text) != 6 {
		return 0, false
	}
	value, err := strconv.ParseUint(text, 16, 32)
	if err != nil {
		return 0, false
	}
	return uint32(value) & 0xffffff, true
}
