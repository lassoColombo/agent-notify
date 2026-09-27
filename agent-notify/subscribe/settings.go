package subscribe

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/lassoColombo/agent-notify/internal/config"
	"github.com/lassoColombo/agent-notify/internal/sessionwatcher"
	"github.com/pelletier/go-toml/v2"
)

// Settings decodes this integration's own `settings` table into whatever shape
// it declares.
//
//	var mine struct {
//	    Binary string            `toml:"binary"`
//	    Glyphs map[string]string `toml:"glyphs"`
//	}
//	err := integration.Settings(&mine)
//
// It exists so that no integration ever locates, reads or parses the config
// file itself (§A10.4) — the file is core's and its shape is ours to change —
// while the contents of that one table stay entirely the integration's business
// (R7). Core never reads what is in here; this runs in the integration's own
// process, compiled in from the SDK.
//
// An unrecognised key is an error rather than a silence. The whole failure mode
// this is here to prevent is a misspelled glyph name that changes nothing and
// says nothing, leaving a person to conclude the feature does not work.
//
// No table at all is not an error: it leaves `into` exactly as it was, which is
// how a display's own defaults survive a user who has no opinion.
func (i Integration) Settings(into any) error {
	layout, err := i.layout()
	if err != nil {
		return err
	}
	settings, problems := config.Load(layout.ConfigFile)
	for _, problem := range problems {
		// A file core refused whole took this integration's table down with
		// it, and handing back the display's defaults as if the user had
		// written nothing would be a lie. Core's other notes — an
		// unrecognised key three tables away, a duration that is not one —
		// are about a file that was otherwise used, and are core's business
		// to report, not this integration's to fail on.
		if errors.Is(problem, config.ErrRefused) {
			return fmt.Errorf("%s: %w", layout.ConfigFile, problem)
		}
	}

	section := settings.Integration[i.Name].Settings
	if len(section) == 0 {
		return nil
	}
	encoded, err := toml.Marshal(section)
	if err != nil {
		return fmt.Errorf("[integration.%s.settings] cannot be re-encoded: %w", i.Name, err)
	}
	decoder := toml.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		// go-toml's own message for this says only that something is missing
		// from the target struct. The whole value of refusing is naming the
		// key, so the position report is what gets carried up.
		var strict *toml.StrictMissingError
		if errors.As(err, &strict) {
			return fmt.Errorf("[integration.%s.settings]: %d key(s) nobody declared:\n%s",
				i.Name, len(strict.Errors), strict.String())
		}
		return fmt.Errorf("[integration.%s.settings]: %w", i.Name, err)
	}
	return nil
}

// There is deliberately no Glyphs shortcut here. A display declares
//
//	Glyphs map[string]string `toml:"glyphs"`
//
// in its own settings struct and hands it to session.NewPalette, which is
// one read of the file instead of two and keeps the strictness above honest: a
// shortcut that read `glyphs` separately would have to be excused from
// DisallowUnknownFields, and the excuse would grow.

// ConfigFile is where agent-notify's configuration lives.
//
// It is here for one caller: an integration's own `install`, which has a table
// to add to that file and must not work out where it is by rebuilding core's
// rules — XDG, the one environment override, the platform default. A path
// derived twice is a path that will differ once, and the failure is silent:
// the table lands in a file nothing reads, and the integration is correct and
// invisible.
//
// Nothing else needs it. Reading settings is Settings, reading sessions is
// Read, and neither requires knowing where anything is.
func (i Integration) ConfigFile() (string, error) {
	layout, err := i.layout()
	if err != nil {
		return "", err
	}
	return layout.ConfigFile, nil
}

// CoreBinary is where the agent-notify command lives.
//
// A display that offers a click has to be able to run something when it is
// clicked, and the thing it runs is `agent-notify focus-session`. Finding it is
// the same problem the session-watcher already solved for itself: a supervised
// child's PATH is not your shell's PATH, which is exactly why
// `agent-notify-binary` exists in the configuration (D-33).
func (i Integration) CoreBinary() (string, error) {
	layout, err := i.layout()
	if err != nil {
		return "", err
	}
	settings, _ := config.Load(layout.ConfigFile)
	return sessionwatcher.CoreBinary(settings.AgentNotifyBinary)
}
