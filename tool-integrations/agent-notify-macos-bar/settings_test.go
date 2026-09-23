package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agentnotify "github.com/lassoColombo/agent-notify"
	"github.com/lassoColombo/agent-notify/subscribe"
)

// settingsIn writes a config and reads it back the way the program does, which
// means through core's own locating of the file rather than a path assembled
// here (§A10.4).
func settingsIn(t *testing.T, body string) (Resolved, error) {
	t.Helper()
	// The integration as a test sees it: the same value the program builds,
	// with a root of its own so nothing here can reach the real config (D-69).
	given := subscribe.Integration{Name: Name, Root: t.TempDir()}
	path, err := given.ConfigFile()
	if err != nil {
		t.Fatalf("ConfigFile: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// `/usr/bin/true` stands in for core: what matters here is that something
	// is there to be found, because a display that cannot run anything when a
	// row is chosen refuses to start at all.
	whole := "agent-notify-binary = \"/usr/bin/true\"\n" + body
	if err := os.WriteFile(path, []byte(whole), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return Read(given)
}

func TestAUserWithNoOpinionGetsTheDefaults(t *testing.T) {
	settings, err := settingsIn(t, "")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if settings.Rows != defaultRows ||
		settings.Announce != defaultAnnounce || settings.Resting != defaultResting {
		t.Errorf("the defaults did not survive an empty file: %+v", settings)
	}
	sample := agentnotify.Record{Kernel: agentnotify.BlockedOnYou, Rank: agentnotify.RankBlockedOnYou}
	if got := settings.Glyphs.For(sample); got != DefaultGlyphs[agentnotify.BlockedOnYou] {
		t.Errorf("the glyph is %q", got)
	}
}

// TestAColourNobodyCanDrawIsRefusedByName. The failure it prevents is a state
// drawn in the ordinary menu colour, which looks exactly like a state nobody
// configured.
func TestAColourNobodyCanDrawIsRefusedByName(t *testing.T) {
	_, err := settingsIn(t, `
[integration.macos-bar.settings.colors]
blocked-on-you = "scarlet"
`)
	if err == nil {
		t.Fatal("a colour AppKit has never heard of was accepted")
	}
	for _, want := range []string{"blocked-on-you", "scarlet", "systemRed"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not mention %q: %v", want, err)
		}
	}
}

func TestBothSpellingsOfAColourAreAccepted(t *testing.T) {
	if _, err := settingsIn(t, `
[integration.macos-bar.settings.colors]
blocked-on-you = "0xffeb6f92"
working = "systemIndigo"
`); err != nil {
		t.Errorf("Read: %v", err)
	}
}

func TestAFontThatIsNotHereIsRefused(t *testing.T) {
	_, err := settingsIn(t, `
[integration.macos-bar.settings]
font = "No Such Face"
`)
	if err == nil || !strings.Contains(err.Error(), "No Such Face") {
		t.Errorf("err = %v, want a refusal naming the font", err)
	}
}

// TestZeroIsAnAnswerForAnnounce: "the item stays passive" is a thing to ask
// for, so zero is a request rather than an absent key. A negative one is not a
// request at all, and is refused.
func TestZeroIsAnAnswerForAnnounce(t *testing.T) {
	settings, err := settingsIn(t, `
[integration.macos-bar.settings]
announce = 0
`)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if settings.Announce != 0 {
		t.Errorf("announce = %v, want it turned off", settings.Announce)
	}

	// Eight seconds, in the nanoseconds a time.Duration is written in.
	settings, err = settingsIn(t, `
[integration.macos-bar.settings]
announce = 8000000000
`)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if settings.Announce != 8*time.Second {
		t.Errorf("announce = %v, want 8s", settings.Announce)
	}

	if _, err := settingsIn(t, `
[integration.macos-bar.settings]
announce = -1
`); err == nil {
		t.Errorf("a negative announce was accepted")
	}
}

func TestASettingNobodyDeclaredIsRefused(t *testing.T) {
	_, err := settingsIn(t, `
[integration.macos-bar.settings]
positon = "left"
`)
	if err == nil || !strings.Contains(err.Error(), "positon") {
		t.Errorf("err = %v, want the misspelling named", err)
	}
}

// TestRefreshIsNotASetting: how often the item repaints is mechanism, not
// taste, so the key is gone and a file that still carries it is told so by
// name rather than quietly obeyed.
func TestRefreshIsNotASetting(t *testing.T) {
	if _, err := settingsIn(t, `
[integration.macos-bar.settings]
refresh = "5s"
`); err == nil || !strings.Contains(err.Error(), "refresh") {
		t.Errorf("err = %v, want refresh named as a key nobody declared", err)
	}
}
