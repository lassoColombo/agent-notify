package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// soundsIn stands up a directory of sound files, so that what is being tested
// is the lookup and not this machine's /System/Library/Sounds.
func soundsIn(t *testing.T, names ...string) string {
	t.Helper()
	where := t.TempDir()
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(where, name), []byte("not really a sound"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	return where
}

// TestANameIsMatchedLooselyAndPassedOnExactly. Forgiving about what somebody
// writes and exact about what macOS is handed: the lookup there matches on the
// file's own name, extension and all.
func TestANameIsMatchedLooselyAndPassedOnExactly(t *testing.T) {
	where := soundsIn(t, "Submarine.aiff", "TD_Default.wav")
	for _, written := range []string{"Submarine", "submarine", "SUBMARINE", "Submarine.aiff", " Submarine "} {
		got, found := TheSoundFileCalled(written, []string{where})
		if !found || got != "Submarine.aiff" {
			t.Errorf("%q gave (%q, %v), want Submarine.aiff", written, got, found)
		}
	}
	// An extension that is not .aiff is nothing special: whatever is in the
	// directory is a name that can be written.
	if got, found := TheSoundFileCalled("TD_Default", []string{where}); !found || got != "TD_Default.wav" {
		t.Errorf("TD_Default gave (%q, %v)", got, found)
	}
	if _, found := TheSoundFileCalled("Submarien", []string{where}); found {
		t.Errorf("a misspelling was found")
	}
	if _, found := TheSoundFileCalled("", []string{where}); found {
		t.Errorf("the empty name was found")
	}
}

// TestTheFirstDirectoryWins, which is macOS's own order: a sound of your own in
// ~/Library/Sounds shadows the system one of the same name, and this has to
// agree or the file checked is not the file played.
func TestTheFirstDirectoryWins(t *testing.T) {
	mine := soundsIn(t, "Submarine.wav")
	system := soundsIn(t, "Submarine.aiff", "Glass.aiff")
	got, found := TheSoundFileCalled("Submarine", []string{mine, system})
	if !found || got != "Submarine.wav" {
		t.Errorf("got (%q, %v), want mine to shadow the system's", got, found)
	}
}

// TestADirectoryThatIsNotThereIsNotAProblem: three of the four macOS looks in
// are absent on a machine nobody has put a sound on.
func TestADirectoryThatIsNotThereIsNotAProblem(t *testing.T) {
	where := soundsIn(t, "Glass.aiff")
	got, found := TheSoundFileCalled("Glass", []string{"/no/such/place", where})
	if !found || got != "Glass.aiff" {
		t.Errorf("got (%q, %v)", got, found)
	}
	if names := TheSoundsOnThisMachine([]string{"/no/such/place"}); len(names) != 0 {
		t.Errorf("names = %v for nowhere at all", names)
	}
}

// TestWhatCanBeWrittenIsListedOnce, sorted and without extensions, because it
// is read by somebody who has just been told their name was wrong.
func TestWhatCanBeWrittenIsListedOnce(t *testing.T) {
	mine := soundsIn(t, "Submarine.wav")
	system := soundsIn(t, "Submarine.aiff", "Glass.aiff", "Basso.aiff")
	got := TheSoundsOnThisMachine([]string{mine, system})
	want := []string{"Basso", "Glass", "Submarine"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestTheSoundDirectoriesAreTheOnesMacOSLooksIn, in macOS's own order. Checked
// against a different list than the one being read would be a check of nothing.
func TestTheSoundDirectoriesAreTheOnesMacOSLooksIn(t *testing.T) {
	places := SoundDirectories()
	if !slices.Contains(places, "/System/Library/Sounds") {
		t.Errorf("places = %v, want the system's own", places)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory here")
	}
	if places[0] != filepath.Join(home, "Library", "Sounds") {
		t.Errorf("places[0] = %q, want yours to come first", places[0])
	}
}

// TestWhatEachSoundSaysItWillDo, which is the startup line and `check`. A line
// that said "banners and sound" while every notification went out silent is the
// first thing somebody reads and the last thing they believe.
func TestWhatEachSoundSaysItWillDo(t *testing.T) {
	for _, each := range []struct {
		sound Sound
		want  string
	}{
		{Sound{}, "silently (sound = false)"},
		{Sound{Plays: true}, "with the default chime"},
		{Sound{Plays: true, Name: "Submarine.aiff"}, "with Submarine.aiff"},
	} {
		if got := each.sound.Describe(); got != each.want {
			t.Errorf("%+v said %q, want %q", each.sound, got, each.want)
		}
	}
}
