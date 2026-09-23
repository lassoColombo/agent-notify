package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	// is there to be found, because a program that could not run anything when
	// a banner is tapped refuses to start at all.
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
	if settings.Preview != defaultPreview {
		t.Errorf("the defaults did not survive an empty file: %+v", settings)
	}
	if settings.Core == "" {
		t.Errorf("nothing was found to run when a banner is tapped")
	}
	if settings.Sound != (Sound{Plays: true}) {
		t.Errorf("sound = %+v for a file that says nothing, want the default chime", settings.Sound)
	}
}

// TestSoundTakesThreeShapes: true, false, and a name. One key rather than two,
// because `sound` and a separate `sound-name` would have a case where they
// disagree and one of them quietly wins.
func TestSoundTakesThreeShapes(t *testing.T) {
	// A name is resolved against the real machine, because Read reads the real
	// machine — the lookup itself is tested against a directory of its own in
	// sounds_test.go. Submarine has shipped with macOS for thirty years, but a
	// machine that has had its sounds taken away should skip, not fail.
	if _, found := TheSoundFileCalled("Submarine", SoundDirectories()); !found {
		t.Skip("this machine has no Submarine.aiff")
	}
	for _, shape := range []struct {
		written string
		want    Sound
	}{
		{"sound = false", Sound{}},
		{"sound = true", Sound{Plays: true}},
		// Named by its bare name and matched case-insensitively, and what comes
		// back is the FILE's name: that is what macOS's lookup matches on.
		{`sound = "submarine"`, Sound{Plays: true, Name: "Submarine.aiff"}},
		{`sound = "Submarine.aiff"`, Sound{Plays: true, Name: "Submarine.aiff"}},
	} {
		settings, err := settingsIn(t, "[integration.macos-notifications.settings]\n"+shape.written)
		if err != nil {
			t.Fatalf("%s: %v", shape.written, err)
		}
		if settings.Sound != shape.want {
			t.Errorf("%s gave %+v, want %+v", shape.written, settings.Sound, shape.want)
		}
	}
}

// TestASoundThatIsNotOnThisMachineIsRefused, and this is the whole reason the
// lookup exists. macOS accepts any string, resolves it in the notification
// daemon, and plays the DEFAULT chime when it finds nothing — so a misspelling
// that was passed through would be a wrong noise forever with nothing to read.
func TestASoundThatIsNotOnThisMachineIsRefused(t *testing.T) {
	if _, found := TheSoundFileCalled("Submarine", SoundDirectories()); !found {
		t.Skip("this machine has no Submarine.aiff to offer in the refusal")
	}
	_, err := settingsIn(t, `
[integration.macos-notifications.settings]
sound = "Submarien"
`)
	if err == nil {
		t.Fatal("a sound nothing can play was accepted")
	}
	if !strings.Contains(err.Error(), "Submarien") {
		t.Errorf("err = %v, want the misspelling named", err)
	}
	if !strings.Contains(err.Error(), "Submarine") {
		t.Errorf("err = %v, want the real names offered", err)
	}
}

// TestSoundIsNotAnythingElse. A number where a name belongs says so, rather
// than being read as false by a switch with a lazy default.
func TestSoundIsNotAnythingElse(t *testing.T) {
	if _, err := settingsIn(t, `
[integration.macos-notifications.settings]
sound = 3
`); err == nil {
		t.Errorf("sound = 3 was accepted")
	}
}

func TestThePreviewCanBeChangedAndCanBeWrong(t *testing.T) {
	settings, err := settingsIn(t, `
[integration.macos-notifications.settings.preview]
lines = 3
width = 40
`)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if settings.Preview != (Preview{Lines: 3, Width: 40}) {
		t.Errorf("preview = %+v", settings.Preview)
	}

	if _, err := settingsIn(t, `
[integration.macos-notifications.settings.preview]
width = 4
`); err == nil {
		t.Errorf("a preview four characters wide was accepted")
	}
}

// TestAKeyNobodyDeclaredIsRefused, by name, because a misspelled setting that
// changes nothing and says nothing is the config bug people give up on.
func TestAKeyNobodyDeclaredIsRefused(t *testing.T) {
	_, err := settingsIn(t, `
[integration.macos-notifications.settings]
sing = "agent-notify self-signed"
`)
	if err == nil {
		t.Fatal("a key nobody declared was accepted")
	}
	if !strings.Contains(err.Error(), "sing") {
		t.Errorf("err = %v, want the misspelling named", err)
	}
}

// TestSomebodyElsesTableIsNotOurs. Every integration reads one table out of a
// file it shares with all the others, and reading somebody else's keys — or
// refusing the file because of them — is the failure this checks for.
func TestSomebodyElsesTableIsNotOurs(t *testing.T) {
	settings, err := settingsIn(t, `
[integration.macos-bar]
binary = "/usr/bin/true"

[integration.macos-bar.settings]
rows = 3
resting = "0xff8c88a6"

[integration.macos-notifications.settings.preview]
lines = 2
`)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if settings.Preview.Lines != 2 {
		t.Errorf("preview.lines = %d, want ours and not the bar's", settings.Preview.Lines)
	}
}

// TestSignIsRememberedForInstallAndIgnoredAtRuntime. It is written by
// `install --sign` and read by `install` alone; nothing in Resolved carries it,
// because nothing at runtime may quietly re-sign anything.
func TestSignIsDeclaredSoItIsNotRefused(t *testing.T) {
	if _, err := settingsIn(t, `
[integration.macos-notifications.settings]
sign = "agent-notify self-signed"
`); err != nil {
		t.Errorf("the identity install writes was refused when read back: %v", err)
	}
}

// TestAColourThatCannotBeDrawnIsRefusedWhenTheConfigIsRead, by name. AppKit's
// colour names are no use here: the mark goes into a PNG, where a colour that
// adapts to the appearance has nothing to adapt to.
func TestAColourThatCannotBeDrawnIsRefused(t *testing.T) {
	if _, err := settingsIn(t, `
[integration.macos-notifications.settings.colors]
broke = "systemOrange"
`); err == nil || !strings.Contains(err.Error(), "broke") {
		t.Errorf("err = %v, want the state named", err)
	}

	settings, err := settingsIn(t, `
[integration.macos-notifications.settings.colors]
broke = "0xfff6c177"
`)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	sample := agentnotify.Record{Kernel: agentnotify.Broke, Rank: agentnotify.RankBroke}
	if got := settings.Colours.For(sample); got != "0xfff6c177" {
		t.Errorf("colours.For(broke) = %q", got)
	}
}
