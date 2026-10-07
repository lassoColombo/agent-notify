package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Which sound a banner plays is the one thing about a notification's
// presentation this program decides, and this file is how a name written in the
// config is turned into something macOS will actually find.
//
// It has to be checked HERE, when the config is read, because macOS will not
// check it. `[UNNotificationSound soundNamed:]` accepts any string whatever,
// returns an object, and reports nothing: resolution happens later, inside the
// notification daemon, and a name that resolves to nothing falls back to the
// default chime [measured 2026-09-19 — the object is opaque, and identical for
// a real name and a nonsense one]. So the failure mode without this file is a
// misspelled sound that plays the wrong noise forever and never says why, which
// is the same failure the colours are refused for.

// SoundDirectories is where macOS looks for a notification sound by name, most
// specific first, which is the order a name is resolved in here too.
//
// Not the app bundle, although macOS would look there as well: `install`
// rewrites the bundle from scratch on every rebuild, so a sound file kept
// inside it is a sound file that disappears. `~/Library/Sounds` is where a
// sound of your own belongs and it survives everything this program does.
func SoundDirectories() []string {
	places := []string{"/Library/Sounds", "/Network/Library/Sounds", "/System/Library/Sounds"}
	if home, err := os.UserHomeDir(); err == nil {
		places = append([]string{filepath.Join(home, "Library", "Sounds")}, places...)
	}
	return places
}

// TheSoundFileCalled finds what a written name refers to and answers with the
// name macOS should be handed, which is the file's own — extension and all,
// because that is what the lookup matches on.
//
// Forgiving about what is written and exact about what is passed on:
// "submarine", "Submarine" and "Submarine.aiff" are all the same file, and all
// three come back as "Submarine.aiff".
func TheSoundFileCalled(name string, directories []string) (string, bool) {
	wanted := strings.ToLower(strings.TrimSpace(name))
	if wanted == "" {
		return "", false
	}
	for _, directory := range directories {
		entries, err := os.ReadDir(directory)
		if err != nil {
			// A directory that is not there is not a problem: three of the four
			// are absent on a machine nobody has put a sound on.
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			file := entry.Name()
			bare := strings.TrimSuffix(file, filepath.Ext(file))
			if strings.ToLower(file) == wanted || strings.ToLower(bare) == wanted {
				return file, true
			}
		}
	}
	return "", false
}

// TheSoundsOnThisMachine is every name that could be written, for the error
// message when one that cannot be is. A refusal that does not say what the
// alternatives are sends somebody to a search engine for a list macOS keeps in
// a directory.
func TheSoundsOnThisMachine(directories []string) []string {
	seen := map[string]bool{}
	var names []string
	for _, directory := range directories {
		entries, err := os.ReadDir(directory)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			bare := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
			if bare != "" && !seen[strings.ToLower(bare)] {
				seen[strings.ToLower(bare)] = true
				names = append(names, bare)
			}
		}
	}
	sort.Strings(names)
	return names
}

// Sound is what a banner plays.
type Sound struct {
	// Plays is false for silence, which is `sound = false`.
	Plays bool
	// Name is the sound file macOS is asked for, or "" for the system default.
	Name string
}

// theSoundIn reads the `sound` key, which is the one setting here that takes
// more than one kind of value: `true`, `false`, or a name. It is declared as
// `any` and taken apart by hand because TOML is typed and go-toml has no hook
// for a value that may be either — and two keys for one idea (`sound` and
// `sound-name`, one of which quietly wins) would be worse than this switch.
func theSoundIn(written any, directories []string) (Sound, error) {
	switch value := written.(type) {
	case nil:
		return Sound{Plays: true}, nil
	case bool:
		return Sound{Plays: value}, nil
	case string:
		file, found := TheSoundFileCalled(value, directories)
		if !found {
			return Sound{}, fmt.Errorf(
				"[integration.%s.settings] sound = %q is not a sound on this machine.\n"+
					"macOS is given the name and never says it could not find it — it plays the\n"+
					"default chime instead — so it is refused here. There is: %s.\n"+
					"A sound of your own goes in ~/Library/Sounds and can be called anything.",
				Name, value, strings.Join(TheSoundsOnThisMachine(directories), ", "))
		}
		return Sound{Plays: true, Name: file}, nil
	default:
		return Sound{}, fmt.Errorf(
			"[integration.%s.settings] sound = %v is neither true, false, nor the name of a "+
				"sound like \"Submarine\"", Name, written)
	}
}

// Describe is what this sound will do, for `check` and the line at startup.
func (s Sound) Describe() string {
	switch {
	case !s.Plays:
		return "silently (sound = false)"
	case s.Name != "":
		return fmt.Sprintf("with %s", s.Name)
	default:
		return "with the default chime"
	}
}
