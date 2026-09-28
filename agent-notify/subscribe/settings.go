// Package subscribe is what a tool-integration imports.
//
// It is what is left of a much larger package, and what went is the point. It
// used to own the socket: connect, declare, receive the opening snapshot,
// coalesce, reconnect, resync, shut down — five hundred lines of protocol every
// display linked. No display links it now. One that core runs is handed a view
// on stdin and exits, and one that must own its process reads
// `agent-notify tail --json`, so the protocol moved to internal/subscriber
// where the only thing that speaks it is core talking to itself.
//
// What an author needs is here:
//
//   - [Integration.Settings], so that nobody locates, reads or parses the
//     config file themselves — the file is core's and its shape is ours to
//     change (§A10.4).
//   - [Integration.Read], the cold read: what is running, straight from the
//     store, with no session-watcher and no socket anywhere (§A7.6).
//   - [RunThroughTheCLI], for the one kind of display that has to own its
//     process, and [LaunchAgentPlist], for the thing that starts it.
package subscribe

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"

	"github.com/lassoColombo/agent-notify/internal/config"
	"github.com/lassoColombo/agent-notify/internal/core"
	"github.com/lassoColombo/agent-notify/internal/paths"
	"github.com/lassoColombo/agent-notify/internal/sessionwatcher"
	"github.com/lassoColombo/agent-notify/session"
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

// Integration is what an author fills in.
type Integration struct {
	// Name is what this calls itself, in the handshake and in the log.
	Name string
	// WakeOn names the record fields worth waking for. Empty means anything
	// but the stamps. "wake me only when kernel changes" is the common case.
	WakeOn []string
	// WantEnded asks to see ended sessions. A bar says no and they never
	// appear; a picker, whose job is offering you something to resume, says
	// yes; and so does a display that painted something into a UI it does not
	// own and must be told when to give it back, since the record is what
	// remembers which pane it was.
	WantEnded bool
	// OnChange is the render function. It is called with the current state,
	// never with a transition, and an error from it is logged and otherwise
	// ignored: a display's failure is its own (R13).
	OnChange func(session.View) error

	// Logger is optional; without one nothing is logged.
	Logger *slog.Logger
	// Root overrides where to look for the session-watcher, for tests and for
	// a fake (StartFake).
	Root string
}

// layout is where this integration's things are.
func (i Integration) layout() (paths.Layout, error) {
	return paths.FromEnvironmentOrUnder(i.Root)
}

// Read is the cold read path: what is running, straight from the store, with no
// session-watcher and no socket anywhere.
//
// It is here so that no integration ever walks our directory layout itself —
// the layout is ours to change (§A10.4). An agent's own statusline, polled
// several times a second to show your *other* agents, is the case this exists
// for (§A7.6).
//
// It applies the liveness decision as it reads and writes nothing, exactly as
// `agent-notify list` does and through the same function: a session whose
// process was killed is handed over as ended even if nothing has got round to
// filing it (D-30). An integration that had to do that for itself would be the
// second implementation of it, and the two would disagree about somebody's
// screen (R24).
func (i Integration) Read() ([]session.Record, error) {
	return i.read(false)
}

// ReadIncludingEnded is Read plus the sessions that are over and still
// resumable — the set bounded by `keep-ended-sessions`.
//
// It is a separate method rather than a flag because the two callers are
// different things: a bar asks the first question and a picker asks the
// second, whose entire job is offering you something to resume (§A7.5).
func (i Integration) ReadIncludingEnded() ([]session.Record, error) {
	return i.read(true)
}

func (i Integration) read(includeEnded bool) ([]session.Record, error) {
	layout, err := i.layout()
	if err != nil {
		return nil, err
	}
	opened, err := core.OpenAt(layout, "subscribe")
	if err != nil {
		return nil, err
	}
	defer opened.Close()

	return opened.WhatIsRunning(includeEnded), nil
}

// History is one session's history: the last few things it said and the last
// few times its state moved (§A7.7).
//
// It is read on demand, for one session, and never arrives with a delta — that
// asymmetry is the whole reason it is a separate file, because a delta carrying
// it would grow from a kilobyte to twenty. The caller is a display with room to
// show more than a row: a preview pane, a chip somebody is hovering over.
//
// A session with nothing recorded yet is an empty History and no error, which
// is the ordinary case for one that has only just started.
func (i Integration) History(key session.Key) (session.History, error) {
	layout, err := i.layout()
	if err != nil {
		return session.History{}, err
	}
	opened, err := core.OpenAt(layout, "subscribe")
	if err != nil {
		return session.History{}, err
	}
	defer opened.Close()

	return opened.Store.History(key)
}
