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
//
// It is assembled in one place because the order matters and is easy to get
// subtly wrong. The log is opened before the configuration is read, so that
// complaints about the configuration have somewhere to go.
type Core struct {
	Layout   paths.Layout
	Settings config.Config
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

// OpenAt is OpenEverythingACommandNeeds for a caller that already knows where
// everything is — the session-watcher, which is handed its layout explicitly
// rather than resolving it again.
//
// That is not redundancy. The session-watcher is spawned with a sanitised
// environment, and path resolution reads the environment: strip TMPDIR on
// darwin, or the XDG variables on Linux, and the daemon would resolve a
// *different* runtime directory than the clients poking it. Nothing would
// error — the hook would write its record, the daemon would watch an empty
// directory, and the bar would stay blank.
func OpenAt(layout paths.Layout, component string) (*Core, error) {
	if err := layout.Create(); err != nil {
		return nil, err
	}

	logger, closer := logs.OpenFile(layout.LogFile(), component)
	settings, problems := config.Load(layout.ConfigFile)
	for _, problem := range problems {
		logger.Warn("configuration", "problem", problem.Error())
	}

	opened, err := sessionstore.Open(layout, settings)
	if err != nil {
		closer.Close()
		return nil, err
	}
	return &Core{
		Layout: layout, Settings: settings,
		Logger: logger, Store: opened, closeLog: closer,
	}, nil
}

// Close releases the log file. Every other handle goes when the process does.
func (c *Core) Close() { _ = c.closeLog.Close() }
