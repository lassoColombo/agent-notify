package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	agentnotify "github.com/lassoColombo/agent-notify"
	"github.com/lassoColombo/agent-notify/subscribe"
)

// Name is what this integration calls itself: its config table, its handshake,
// and the prefix of every item it puts on the bar.
const Name = "sketchybar"

// me is this integration: how everything here asks where agent-notify's files
// are, and what its own settings say.
//
// Root is left empty on purpose — empty means "wherever this process's
// environment says", which is right everywhere but a test (D-69).
var me = subscribe.Integration{Name: Name}

// Painted is the order the counters sit in, left to right, and it never
// changes with the data.
//
// A bar whose items move about is a bar nobody can click, so the position of a
// state is fixed by its rank and a state with nothing in it is simply not
// drawn. `ended` is absent entirely: an ended session is not displayed, period
// (D-26), and there is nothing to count.
func Painted() []agentnotify.Kernel {
	var painted []agentnotify.Kernel
	for _, kernel := range agentnotify.Kernels() {
		if kernel != agentnotify.Ended {
			painted = append(painted, kernel)
		}
	}
	return painted
}

// DefaultGlyphs is the mark on each counter. Nerd Font, Font Awesome's BMP
// block, written as escapes rather than as literals for the reason the zellij
// display found out the hard way: these are private-use codepoints and a great
// deal of tooling drops them silently.
var DefaultGlyphs = map[agentnotify.Kernel]string{
	agentnotify.BlockedOnYou:  "\uf071", // warning triangle
	agentnotify.Broke:         "\uf00d", // a cross
	agentnotify.FinishedATurn: "\uf075", // speech bubble
	agentnotify.Working:       "\uf021", // circular arrows
	agentnotify.Idle:          "\uf111", // a dot
}

// DefaultColours is what each state is worth looking at in.
//
// A bar has colour where a zellij title does not, so here the colour carries
// the urgency and the glyph only says which kind. Rosé Pine, because it is a
// palette people actually run and because every value is overridable in one
// table anyway.
var DefaultColours = map[agentnotify.Kernel]string{
	agentnotify.BlockedOnYou:  "0xffeb6f92", // love — answer this now
	agentnotify.Broke:         "0xfff6c177", // gold — something went wrong
	agentnotify.FinishedATurn: "0xff9ccfd8", // foam — ready for you
	agentnotify.Working:       "0xff31748f", // pine — in progress
	agentnotify.Idle:          "0xff6e6a86", // muted — nothing
}

// Settings is `[integration.sketchybar.settings]`, and nothing else in the file
// is ours. A key nobody declared is refused by name.
type Settings struct {
	// Sketchybar is where sketchybar is, as an absolute path.
	//
	// Required, and named after the tool rather than called `binary` because
	// the table above it has a `binary` of its own — this program's — and the
	// install prints both together (D-67).
	Sketchybar string `toml:"sketchybar"`
	// Position is where the counters sit: left, right, center, or one of
	// sketchybar's e/q variants. Handed through without interpretation.
	Position string `toml:"position"`
	// Rows bounds how many sessions a chip lists before it says "and n more".
	Rows int `toml:"rows"`
	// Before and After name an item in your sketchybarrc that these counters
	// should sit next to. Without one they land at the far end of their side,
	// because this display connects after your config has finished running.
	Before string `toml:"before"`
	After  string `toml:"after"`

	Glyphs map[string]string `toml:"glyphs"`
	Colors map[string]string `toml:"colors"`

	// Popup is how the chip behind each counter is drawn.
	Popup PopupSettings `toml:"popup"`

	// Announce is how long a state change lights the bar up: the counter
	// highlighted, its chip opened on the agent's own words, and the row
	// highlighted inside it. 0 turns it off and leaves the bar passive.
	//
	// In NANOSECONDS, because that is what a time.Duration natively is: Go
	// gives the type no text codec, so `= "8s"` does not decode and a number
	// does. Eight seconds is 8000000000.
	Announce time.Duration `toml:"announce"`

	// Preview is the agent's last message, shown under the chip's rows when you
	// hover one.
	Preview PreviewSettings `toml:"preview"`
}

// PreviewSettings is `[integration.sketchybar.settings.preview]`.
//
// Depth and Lines are two different numbers on purpose: every line is WRITTEN
// to the bar and only the first Lines of them are DRAWN. Scrolling then moves
// nothing but `drawing`, which is what keeps a wheel off the agent's words
// entirely — see the scroll in render.go.
type PreviewSettings struct {
	Lines *int   `toml:"lines"`
	Depth *int   `toml:"depth"`
	Width *int   `toml:"width"`
	Text  string `toml:"text"`
}

// PopupSettings is `[integration.sketchybar.settings.popup]`.
//
// A chip has to be opaque. A bar can be frosted and beautiful with a background
// at 1% alpha; a panel of text floating over a terminal cannot, and what you get
// instead is two documents printed on top of each other.
type PopupSettings struct {
	Background string `toml:"background"`
	// Border is a colour, and "" means no border rather than a black one.
	Border string `toml:"border"`
	Radius *int   `toml:"radius"`
}

// Resolved is Settings after everything that can fail has been done once, at
// startup, where it can still be reported to a person.
type Resolved struct {
	Binary   string
	Core     string
	Position string
	Before   string
	After    string
	Rows     int
	Glyphs   agentnotify.Palette
	Colours  agentnotify.Palette
	Popup    Popup
	Announce time.Duration
	Preview  Preview
}

const (
	defaultRows     = 8
	defaultPosition = "left"
)

// Refresh is how often the bar is repainted with nothing new to say, so that
// the ages on the chips stay true. Elapsed time is rendering and is never a
// state change (R26), so keeping it current is this display's own business —
// and how often it does it is this display's business too, not a person's.
const Refresh = 30 * time.Second

// Timeout bounds one sketchybar invocation. On expiry that paint is abandoned
// and the next change paints again, which is what R18 asks of every duration:
// a name, and a defined behaviour when it runs out.
const Timeout = 3 * time.Second

// The chip's default look: Rosé Pine, like the state colours above, and the
// background deliberately at 95% rather than 100% — enough to read through
// nothing, little enough to belong to a frosted bar.
// The chip's preview: six lines of the agent's last message at a time, forty
// written, sixty-four characters wide. Forty lines is about a screenful and a
// half of an answer, which is as much as anybody reads off a menu bar.
var defaultPreview = Preview{Lines: 6, Depth: 40, Width: 64, Text: "0xffe0def4"}

// How long a state change is worth interrupting for. Long enough to look up at,
// short enough that a bar which has been lit for a minute means something is
// wrong rather than that something happened.
const defaultAnnounce = 8 * time.Second

var defaultPopup = Popup{
	Background: "0xf2191724", // base, near-opaque
	Border:     "0xff524f67", // highlight-high
	Radius:     10,
}

func Read(given subscribe.Integration) (Resolved, error) {
	var settings Settings
	if err := given.Settings(&settings); err != nil {
		return Resolved{}, err
	}

	resolved := Resolved{
		Position: defaultPosition, Rows: defaultRows,
		Glyphs:   agentnotify.NewPalette(DefaultGlyphs, settings.Glyphs),
		Colours:  agentnotify.NewPalette(DefaultColours, settings.Colors),
		Popup:    defaultPopup,
		Preview:  defaultPreview,
		Announce: defaultAnnounce,
	}
	if settings.Preview.Lines != nil {
		resolved.Preview.Lines = *settings.Preview.Lines
	}
	if settings.Preview.Depth != nil {
		resolved.Preview.Depth = *settings.Preview.Depth
	}
	if settings.Preview.Width != nil {
		resolved.Preview.Width = *settings.Preview.Width
	}
	if settings.Preview.Text != "" {
		resolved.Preview.Text = settings.Preview.Text
	}
	if problem := resolved.Preview.sane(); problem != "" {
		return Resolved{}, fmt.Errorf("[integration.sketchybar.settings.preview] %s", problem)
	}
	if settings.Popup.Background != "" {
		resolved.Popup.Background = settings.Popup.Background
	}
	if settings.Popup.Border != "" {
		resolved.Popup.Border = settings.Popup.Border
	}
	if settings.Popup.Radius != nil {
		// A pointer, because 0 is a real radius — square corners — and an
		// absent key is not a request for them.
		resolved.Popup.Radius = *settings.Popup.Radius
	}
	if settings.Position != "" {
		resolved.Position = settings.Position
	}
	if settings.Rows > 0 {
		resolved.Rows = settings.Rows
	}
	if settings.Before != "" && settings.After != "" {
		return Resolved{}, fmt.Errorf(
			"[integration.sketchybar.settings] names both before = %q and after = %q, "+
				"and the counters can only be in one place", settings.Before, settings.After)
	}
	resolved.Before, resolved.After = settings.Before, settings.After
	if settings.Announce != 0 {
		// Zero is not "unset" here, it is a request: "the bar stays passive".
		// So the only thing left to refuse is a negative one.
		if settings.Announce < 0 {
			return Resolved{}, fmt.Errorf(
				"[integration.sketchybar.settings] announce = %d cannot be negative "+
					"(it is nanoseconds: 8000000000 is eight seconds, 0 keeps the bar passive)",
				settings.Announce)
		}
		resolved.Announce = settings.Announce
	}

	binary, err := TheSketchybarToRun(settings.Sketchybar)
	if err != nil {
		return Resolved{}, err
	}
	resolved.Binary = binary

	// What a click runs. Without it the chips are decoration, so this is a
	// startup error rather than something discovered when somebody clicks.
	core, err := given.CoreBinary()
	if err != nil {
		return Resolved{}, fmt.Errorf("a chip has to be able to run something when it is "+
			"clicked, and %w", err)
	}
	resolved.Core = core
	return resolved, nil
}

// TheSketchybarToRun is the path out of the configuration, checked.
//
// There is no search here and no PATH fallback, and the absence is the design
// (D-67). This program is started by the session-watcher, whose PATH is not
// your shell's — a launchd job's is /usr/bin:/bin and nothing else — so a
// lookup performed HERE is performed in the one context that cannot answer it.
// The list of Homebrew prefixes that used to sit at this line was an attempt to
// guess what the environment would not say.
//
// The lookup happens at install instead, which runs in your shell, where the
// answer is simply available.
func TheSketchybarToRun(given string) (string, error) {
	const key = "[integration." + Name + ".settings] sketchybar"
	switch {
	case given == "":
		return "", fmt.Errorf("%s is not set — run `agent-notify install %s`, which finds "+
			"sketchybar in your own shell and prints the line to add", key, Name)
	case !filepath.IsAbs(given):
		return "", fmt.Errorf("%s = %q must be an absolute path: a supervised child's PATH "+
			"is not yours, so a bare name means something different here than it does to you",
			key, given)
	}
	if _, err := os.Stat(given); err != nil {
		return "", fmt.Errorf("%s = %q: %w", key, given, err)
	}
	return given, nil
}
