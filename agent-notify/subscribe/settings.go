// Package subscribe is what a tool-integration imports.
//
// [Integration.Settings] decodes its own table; [Integration.Read] is the cold
// read of the store; [Main] answers the subcommands core runs; [Run] is the
// loop for a display that owns its process; [Integration.Focus] brings a
// session to the front. An integration never locates, reads or parses core's
// files itself (§A10.4).
package subscribe

import (
	"bytes"
	"errors"
	"fmt"
	"sync"

	"github.com/lassoColombo/agent-notify/internal/config"
	"github.com/lassoColombo/agent-notify/internal/core"
	"github.com/lassoColombo/agent-notify/internal/onewatcher"
	"github.com/lassoColombo/agent-notify/internal/paths"
	"github.com/lassoColombo/agent-notify/session"
	"github.com/pelletier/go-toml/v2"
)

// Integration is what an author fills in: what this program is called, and
// what it declares to core.
type Integration struct {
	// Name is the config table, the handshake and the log component.
	Name string
	// WakeOn names the record fields worth waking for (R23). Empty means
	// anything but the stamps.
	WakeOn []string
	// WantEnded asks for ended sessions too: a picker, or a display that owns
	// panes it must give back (D-26).
	WantEnded bool
	// Reads answers `capture-environment`, from inside the agent (D-27):
	// read local state and return, never ask the tool anything. Whatever it
	// returns is stored verbatim under this integration's name, opaque to
	// core (R7). Nil answers an empty object.
	Reads func() (any, error)
	// Root overrides where agent-notify's files are; empty means the
	// environment says (D-69).
	Root string
}

func (i Integration) layout() (paths.Layout, error) {
	if i.Root == "" {
		return paths.FromEnvironment()
	}
	return paths.Under(i.Root)
}

// Core is opened once per process and kept: every read after the first costs
// no directory, no log file and no parse.
var (
	openedMu sync.Mutex
	opened   = map[string]*core.Core{}
)

func (i Integration) core() (*core.Core, error) {
	openedMu.Lock()
	defer openedMu.Unlock()
	key := i.Name + "\x00" + i.Root
	if have, found := opened[key]; found {
		return have, nil
	}
	layout, err := i.layout()
	if err != nil {
		return nil, err
	}
	have, err := core.OpenAt(layout, i.Name)
	if err != nil {
		return nil, err
	}
	opened[key] = have
	return have, nil
}

// Settings decodes this integration's own `settings` table into whatever shape
// it declares. An unrecognised key is refused by name, because a misspelled
// glyph that changes nothing and says nothing is the config bug people give
// up on. No table at all leaves `into` as it was.
func (i Integration) Settings(into any) error {
	opened, err := i.core()
	if err != nil {
		return err
	}
	for _, problem := range opened.Problems {
		// A file refused whole took this table down with it; anything else is
		// core's to report.
		if errors.Is(problem, config.ErrRefused) {
			return fmt.Errorf("%s: %w", opened.Layout.ConfigFile, problem)
		}
	}

	section := opened.Settings.Integration[i.Name].Settings
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
		var strict *toml.StrictMissingError
		if errors.As(err, &strict) {
			return fmt.Errorf("[integration.%s.settings]: %d key(s) nobody declared:\n%s",
				i.Name, len(strict.Errors), strict.String())
		}
		return fmt.Errorf("[integration.%s.settings]: %w", i.Name, err)
	}
	return nil
}

// ConfigFile is where agent-notify's configuration lives, for an `install`
// that has a table to tell somebody about.
func (i Integration) ConfigFile() (string, error) {
	layout, err := i.layout()
	if err != nil {
		return "", err
	}
	return layout.ConfigFile, nil
}

// CoreBinary is where the agent-notify command lives: what the configuration
// names, or a lookup, since a launchd job's PATH is not your shell's (D-33).
func (i Integration) CoreBinary() (string, error) {
	opened, err := i.core()
	if err != nil {
		return "", err
	}
	return onewatcher.CoreBinary(opened.Settings.AgentNotifyBinary)
}

// Read is the cold read: what is running, straight from the store, with the
// liveness decision applied and nothing written, exactly as `agent-notify
// list` does (D-30).
func (i Integration) Read() ([]session.Record, error) {
	return i.read(false)
}

// ReadIncludingEnded is Read plus the sessions that are over and still
// resumable (§A7.5).
func (i Integration) ReadIncludingEnded() ([]session.Record, error) {
	return i.read(true)
}

func (i Integration) read(includeEnded bool) ([]session.Record, error) {
	opened, err := i.core()
	if err != nil {
		return nil, err
	}
	return opened.WhatIsRunning(includeEnded), nil
}

// History is one session's history, read on demand (§A7.7). A session with
// nothing recorded yet is an empty History and no error.
func (i Integration) History(key session.Key) (session.History, error) {
	opened, err := i.core()
	if err != nil {
		return session.History{}, err
	}
	return opened.Store.History(key)
}
