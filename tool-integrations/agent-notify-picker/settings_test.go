package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	fzf "github.com/junegunn/fzf/src"

	"github.com/lassoColombo/agent-notify/session"
	"github.com/lassoColombo/agent-notify/subscribe"
)

// keptAsItWas puts the hues and the keys back when the test ends. Apply assigns
// package variables — one job per process is what makes that safe in the
// program and not in a test binary, where every test after this one asserts on
// the defaults. It walks the slots table rather than naming twelve variables,
// so a thirteenth slot is covered the day it is added.
func keptAsItWas(t *testing.T) {
	t.Helper()
	was := map[*string]string{}
	for _, slot := range (Colours{}).slots() {
		was[slot.into] = *slot.into
	}
	keys, wasRebound := keymap, rebound
	t.Cleanup(func() {
		for into, hue := range was {
			*into = hue
		}
		restyle()
		keymap, rebound = keys, wasRebound
	})
}

// appliedWith writes a config file and reads it back the way the program
// does, which means through core's own locating of the file rather than a path
// assembled here (§A10.4), under a root of its own so that nothing in a test
// can reach the real one (D-69).
func appliedWith(t *testing.T, body string) []string {
	t.Helper()
	keptAsItWas(t)

	given := subscribe.Integration{Name: Name, Root: t.TempDir()}
	path, err := given.ConfigFile()
	if err != nil {
		t.Fatalf("ConfigFile: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return Apply(given)
}

func TestAUserWithNoOpinionKeepsThePalette(t *testing.T) {
	if complaints := appliedWith(t, ""); len(complaints) > 0 {
		t.Fatalf("an empty file is not a mistake: %v", complaints)
	}
	if base05 != "#e0def4" || base08 != "#eb6f92" {
		t.Errorf("the defaults did not survive an empty file: %s %s", base05, base08)
	}
}

// TestAHueIsSpentEverywhereItIsUsed is the test the whole design is for: one
// table, three renderers. base0D is paths in a row, the prompt and the label in
// fzf's chrome, and a link in a rendered message — and a window half in your
// palette and half in mine is worse than one wholly in mine.
func TestAHueIsSpentEverywhereItIsUsed(t *testing.T) {
	if complaints := appliedWith(t, `
[integration.picker.settings.colors]
base0D = "#010203"
base13 = "#040506"
`); len(complaints) > 0 {
		t.Fatalf("a colour was refused: %v", complaints)
	}

	row := Row(aSession(), noon, Measure([]session.Record{aSession()}))
	if !strings.Contains(row, "1;2;3") {
		t.Errorf("the path in a row is not drawn in the hue that was asked for: %q", row)
	}
	if !strings.Contains(fzfColours(), "#010203") {
		t.Errorf("fzf's chrome is still in the default iris: %s", fzfColours())
	}
	// Rendered rather than substituted: glamour has to have accepted the file
	// as well, and a stylesheet it refuses is a message printed as its source.
	if heading := Markdowned("# Heading\n", 60); !strings.Contains(heading, "4;5;6") {
		t.Errorf("a heading in a message is still in the default bright rose: %q", heading)
	}
}

// TestAColourThatIsNotOneKeepsTheDefault. A picker that refused to start here
// would be a floating pane that appears and vanishes — `close_on_exit true` —
// which is indistinguishable from a crash, so the window opens and says so
// (§A14).
func TestAColourThatIsNotOneKeepsTheDefault(t *testing.T) {
	complaints := appliedWith(t, `
[integration.picker.settings.colors]
base05 = "rose"
`)
	if len(complaints) != 1 {
		t.Fatalf("complaints = %v", complaints)
	}
	for _, want := range []string{"base05", "rose", "#e0def4"} {
		if !strings.Contains(complaints[0], want) {
			t.Errorf("the complaint does not mention %q: %s", want, complaints[0])
		}
	}
	if base05 != "#e0def4" {
		t.Errorf("text is %s: a value that could not be used took the default with it", base05)
	}
	if Complaint(complaints) == "" {
		t.Error("the window has nothing to say about a colour it could not use")
	}
}

// TestASlotNobodyPaintsWithIsRefusedByName: base0E is a real base24 slot and
// nothing here spends it, so accepting it would be the silence the strictness
// exists to break — a person edits a value, sees nothing change, and concludes
// the feature does not work.
func TestASlotNobodyPaintsWithIsRefusedByName(t *testing.T) {
	complaints := appliedWith(t, `
[integration.picker.settings.colors]
base0E = "#ffffff"
`)
	if len(complaints) != 1 || !strings.Contains(complaints[0], "base0E") {
		t.Fatalf("complaints = %v", complaints)
	}
	if base05 != "#e0def4" {
		t.Error("a table that could not be decoded should leave every default standing")
	}
}

// TestTheStylesheetHasNoColoursOfItsOwn: a slot added to the embedded style and
// not to the replacer would reach glamour as the literal "{base0F}", which
// glamour draws in nothing at all.
func TestTheStylesheetHasNoColoursOfItsOwn(t *testing.T) {
	if style := string(stylesheet()); strings.Contains(style, "{base") {
		at := strings.Index(style, "{base")
		t.Errorf("a slot nothing resolves: %s", style[at:min(at+12, len(style))])
	}
}

// TestAWindowWithNothingToComplainAboutHasNoHeader: the ordinary run is the
// run somebody makes a hundred times, and it is asked for exactly what it was
// asked for before this table existed.
func TestAWindowWithNothingToComplainAboutHasNoHeader(t *testing.T) {
	quiet := strings.Join(Arguments("/usr/bin/true", "down,34", Ghost(false), ""), " ")
	if strings.Contains(quiet, "--header") {
		t.Errorf("a header on a run with nothing to say: %s", quiet)
	}
	loud := strings.Join(
		Arguments("/usr/bin/true", "down,34", Ghost(false), "base05 is not a colour"), " ")
	if !strings.Contains(loud, "--header base05 is not a colour") {
		t.Errorf("the complaint never reached the window: %s", loud)
	}
}

// TestTheHeaderSaysOneThing: a file with a dozen typos in it would otherwise
// push the sessions off the screen, which is the picker refusing to open by
// another route.
func TestTheHeaderSaysOneThing(t *testing.T) {
	said := Complaint([]string{"base02 is not a colour", "base03 either", "nor base04"})
	if strings.Contains(said, "\n") {
		t.Errorf("the header is more than one line: %q", said)
	}
	if !strings.Contains(said, "base02") || !strings.Contains(said, "2 more") {
		t.Errorf("the header = %q", said)
	}
}

// TestTheKeysAreTodaysBehaviourWrittenDown: what fzf is told about keys used to
// be four binds and whatever fzf did with the rest, which is not something a
// person can read off a source file.
func TestTheKeysAreTodaysBehaviourWrittenDown(t *testing.T) {
	if complaints := appliedWith(t, ""); len(complaints) > 0 {
		t.Fatalf("an empty file is not a mistake: %v", complaints)
	}
	for _, want := range []string{
		"ctrl-j:preview-down", "ctrl-k:preview-up",
		"ctrl-d:preview-half-page-down", "ctrl-u:preview-half-page-up",
		"down:down", "ctrl-n:down", "up:up", "ctrl-p:up",
		"enter:accept", "esc:abort", "ctrl-c:abort",
	} {
		if !strings.Contains(fzfBinds(), want) {
			t.Errorf("%s is not among the binds: %s", want, fzfBinds())
		}
	}
}

// TestAKeyIsTheUsersAndAnActionIsNot: both spellings of a value, and a key that
// was replaced rather than added to.
func TestAKeyIsTheUsersAndAnActionIsNot(t *testing.T) {
	if complaints := appliedWith(t, `
[integration.picker.settings.keys]
preview-down = "ctrl-f"
leave = ["esc", "ctrl-g"]
`); len(complaints) > 0 {
		t.Fatalf("a keymap was refused: %v", complaints)
	}

	binds := fzfBinds()
	for _, want := range []string{"ctrl-f:preview-down", "esc:abort", "ctrl-g:abort"} {
		if !strings.Contains(binds, want) {
			t.Errorf("%s is not among the binds: %s", want, binds)
		}
	}
	// Replaced, not added to: that is how a key is given back to fzf, whose own
	// default for it stands underneath.
	if strings.Contains(binds, "ctrl-j:") {
		t.Errorf("ctrl-j still scrolls the preview: %s", binds)
	}
	// And an action nobody moved keeps what it had.
	if !strings.Contains(binds, "ctrl-k:preview-up") {
		t.Errorf("an action nobody touched lost its key: %s", binds)
	}
}

// TestAKeyAskedToDoTwoThingsIsRefusedWhole. fzf would take one of them and say
// nothing, and which one is its business rather than anybody's intention. The
// ordinary version of this is a key moved onto an action while the action that
// had it was left where it was.
func TestAKeyAskedToDoTwoThingsIsRefusedWhole(t *testing.T) {
	complaints := appliedWith(t, `
[integration.picker.settings.keys]
preview-down = "ctrl-p"
`)
	if len(complaints) != 1 {
		t.Fatalf("complaints = %v", complaints)
	}
	for _, want := range []string{"ctrl-p", "up", "preview-down"} {
		if !strings.Contains(complaints[0], want) {
			t.Errorf("the complaint does not mention %q: %s", want, complaints[0])
		}
	}
	if binds := fzfBinds(); !strings.Contains(binds, "ctrl-j:preview-down") {
		t.Errorf("half a keymap survived: %s", binds)
	}
}

// TestFzfIsWhatChecksAKeyName: a key name is fzf's vocabulary, and a second
// copy of it here would be one that drifts from the library this is built
// against. So what it will not take is dropped for what this program shipped
// with — the fallback chooseOne makes on the one keypress somebody makes.
func TestFzfIsWhatChecksAKeyName(t *testing.T) {
	if complaints := appliedWith(t, `
[integration.picker.settings.keys]
down = "ctrl-shmuck"
`); len(complaints) > 0 {
		t.Fatalf("this program does not check key names: %v", complaints)
	}
	if !rebound {
		t.Fatal("a keymap out of the config file was not noticed as one")
	}

	told := Arguments("/usr/bin/true", "down,34", Ghost(false), "")
	_, err := fzf.ParseOptions(true, told)
	if err == nil {
		t.Fatal("fzf took a key nobody has")
	}

	said := TheKeysWeShippedWith(err)
	if !strings.Contains(said, "ctrl-shmuck") || !strings.Contains(said, "keys this program") {
		t.Errorf("the window would not say why: %s", said)
	}
	if _, err := fzf.ParseOptions(true,
		Arguments("/usr/bin/true", "down,34", Ghost(false), said)); err != nil {
		t.Fatalf("the fallback is not one: %v", err)
	}
}

// TestTheLabelBindIsNotTheUsers: `focus` redraws the preview's label and is not
// a key at all, so it lives in a --bind of its own where no table can take it.
func TestTheLabelBindIsNotTheUsers(t *testing.T) {
	if complaints := appliedWith(t, `
[integration.picker.settings.keys]
down = ["j"]
`); len(complaints) > 0 {
		t.Fatalf("a keymap was refused: %v", complaints)
	}
	told := strings.Join(Arguments("/usr/bin/true", "down,34", Ghost(false), ""), " ")
	if !strings.Contains(told, "focus:bg-transform-preview-label") {
		t.Errorf("the label stopped being redrawn: %s", told)
	}
}

// TestAKeyCanBeGivenBack: an empty list and an empty string are the same ask —
// nothing here asks for that action — and the key underneath goes back to
// whatever fzf does with it, which for ctrl-d is quitting on an empty query.
func TestAKeyCanBeGivenBack(t *testing.T) {
	if complaints := appliedWith(t, `
[integration.picker.settings.keys]
preview-page-down = []
preview-page-up = ""
`); len(complaints) > 0 {
		t.Fatalf("a keymap was refused: %v", complaints)
	}
	if !rebound {
		t.Fatal("a table that was written was read as no table at all")
	}
	binds := fzfBinds()
	if strings.Contains(binds, "ctrl-d:") || strings.Contains(binds, "ctrl-u:") {
		t.Errorf("a key that was given back is still bound: %s", binds)
	}
	if !strings.Contains(binds, "ctrl-j:preview-down") {
		t.Errorf("the rest of the keymap went with it: %s", binds)
	}
}
