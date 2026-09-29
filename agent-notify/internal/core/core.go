// Package core assembles what every command and every hook needs before it
// can do anything.
package core

import (
	"io"
	"log/slog"

	"github.com/lassoColombo/agent-notify/internal/config"
	"github.com/lassoColombo/agent-notify/internal/paths"
	"github.com/lassoColombo/agent-notify/internal/sessionstore"
	"github.com/lassoColombo/agent-notify/logs"
)

// core is everything a command needs before it can do anything: where things
// live, what the user configured, somewhere to complain, and the store.
type Core struct {
	Layout   paths.Layout
	Settings config.Config
	// Problems is what the configuration loader complained about, already
	// logged, for a caller that has to say it again.
	Problems []error
	Logger   *slog.Logger
	Store    *sessionstore.SessionStore
	closeLog io.Closer
}

// OpenEverythingACommandNeeds prepares a command or a hook. It fails only for
// reasons that make the whole thing impossible — no home directory, an unwritable state directory —
// never for a configuration it did not like (R2).
func OpenEverythingACommandNeeds(component string) (*Core, error) {
	layout, err := paths.FromEnvironment()
	if err != nil {
		return nil, err
	}
	return OpenAt(layout, component)
}

// OpenAt is OpenEverythingACommandNeeds for a caller that already has a
// layout. The store creates the directories; the log lives in one of them.
func OpenAt(layout paths.Layout, component string) (*Core, error) {
	settings, problems := config.Load(layout)
	opened, err := sessionstore.Open(layout, settings)
	if err != nil {
		return nil, err
	}
	logger, closer := logs.OpenFile(layout.LogFile(), component)
	for _, problem := range problems {
		logger.Warn("configuration", "problem", problem.Error())
	}
	return &Core{
		Layout: layout, Settings: settings, Problems: problems,
		Logger: logger, Store: opened, closeLog: closer,
	}, nil
}

// Close releases the log file. Every other handle goes when the process does.
func (c *Core) Close() { _ = c.closeLog.Close() }
