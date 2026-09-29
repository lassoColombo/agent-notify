// Package paths resolves every location agent-notify owns, from the environment
// alone, so that no other package ever joins a path itself: a path built in two
// places is a path that will differ in two places, silently (plan.md §A7.2,
// §A14).
package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// TheVariableThatNamesTheRoot is the one environment override: state, runtime
// and the config file all move beneath it (D-20).
const TheVariableThatNamesTheRoot = "AGENT_NOTIFY_ROOT"

// A record carries the text of what an agent last said, which is not
// world-readable material.
const (
	DirMode  os.FileMode = 0o700
	FileMode os.FileMode = 0o600
)

// Layout is every directory and file agent-notify owns, already resolved.
type Layout struct {
	// Root is the AGENT_NOTIFY_ROOT that produced this layout, or "".
	Root string
	// State survives a reboot: the records themselves.
	State string
	// Runtime does not survive a reboot, and must not: the singleton lock and
	// the report.
	Runtime string
	// ConfigFile is the only file here that agent-notify reads and never writes.
	ConfigFile string
}

// FromEnvironment works out the layout for this process. It touches no disk.
func FromEnvironment() (Layout, error) {
	if root := os.Getenv(TheVariableThatNamesTheRoot); root != "" {
		layout, err := Under(root)
		if err != nil {
			return Layout{}, fmt.Errorf("%s=%q is not a usable path: %w",
				TheVariableThatNamesTheRoot, root, err)
		}
		return layout, nil
	}

	state, err := defaultStateDir()
	if err != nil {
		return Layout{}, err
	}
	runtimeDir, err := defaultRuntimeDir()
	if err != nil {
		return Layout{}, err
	}
	configFile, err := defaultConfigFile()
	if err != nil {
		return Layout{}, err
	}
	return Layout{State: state, Runtime: runtimeDir, ConfigFile: configFile}, nil
}

// Under is the layout beneath one named root, and the only place that knows
// what a root contains (D-69). Absolute, because the process that writes a
// record and the one that reads it do not share a working directory.
func Under(root string) (Layout, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return Layout{}, fmt.Errorf("%q is not a usable path: %w", root, err)
	}
	return Layout{
		Root:       absolute,
		State:      filepath.Join(absolute, "state"),
		Runtime:    filepath.Join(absolute, "run"),
		ConfigFile: filepath.Join(absolute, "config.toml"),
	}, nil
}

func defaultStateDir() (string, error) {
	if runtime.GOOS == "darwin" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot locate the state directory: %w", err)
		}
		return filepath.Join(home, "Library", "Application Support", "agent-notify"), nil
	}
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return filepath.Join(xdg, "agent-notify"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate the state directory: %w", err)
	}
	return filepath.Join(home, ".local", "state", "agent-notify"), nil
}

// defaultRuntimeDir is a directory that is already per-user and 0700 where one
// exists, and a uid-suffixed one in the shared temporary space where none does:
// /tmp is writable by everyone, and an unsuffixed name there is a name another
// user can take first.
func defaultRuntimeDir() (string, error) {
	if runtime.GOOS == "darwin" {
		if tmp := os.Getenv("TMPDIR"); tmp != "" {
			return filepath.Join(tmp, "agent-notify"), nil
		}
		return filepath.Join(os.TempDir(), fmt.Sprintf("agent-notify-%d", os.Getuid())), nil
	}
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
		return filepath.Join(xdg, "agent-notify"), nil
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("agent-notify-%d", os.Getuid())), nil
}

func defaultConfigFile() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "agent-notify", "config.toml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate the configuration file: %w", err)
	}
	return filepath.Join(home, ".config", "agent-notify", "config.toml"), nil
}

// Sessions holds the record of every session that has not ended.
func (l Layout) Sessions() string { return filepath.Join(l.State, "sessions") }

// Ended holds records filed away rather than deleted (R6).
func (l Layout) Ended() string { return filepath.Join(l.State, "ended") }

// Locks holds one lock file per session key, never renamed and never deleted
// while referenced (plan.md §A7.2).
func (l Layout) Locks() string { return filepath.Join(l.State, "locks") }

// History holds the last few things each session said and the last few times
// its state moved (§A7.7).
func (l Layout) History() string { return filepath.Join(l.State, "history") }

func (l Layout) SessionFile(key string) string {
	return filepath.Join(l.Sessions(), key+".json")
}
func (l Layout) EndedFile(key string) string {
	return filepath.Join(l.Ended(), key+".json")
}
func (l Layout) HistoryFile(key string) string {
	return filepath.Join(l.History(), key+".json")
}
func (l Layout) LockFile(key string) string {
	return filepath.Join(l.Locks(), key+".lock")
}

// WatcherLock is the flock that makes the session-watcher single (plan.md §A9.2).
func (l Layout) WatcherLock() string {
	return filepath.Join(l.Runtime, "session-watcher.lock")
}

// IntegrationsFile is what the session-watcher knows about the integrations,
// for doctor and a focus to read from another process.
func (l Layout) IntegrationsFile() string {
	return filepath.Join(l.Runtime, "integrations.json")
}

// LogFile is written by every process, which is why it is named after none of
// them. It is in state rather than runtime: a log a reboot empties is missing
// exactly the failures worth reading about.
func (l Layout) LogFile() string { return filepath.Join(l.State, "agent-notify.log") }

// Directories lists every directory agent-notify creates, deepest last.
func (l Layout) Directories() []string {
	return []string{l.State, l.Sessions(), l.Ended(), l.History(), l.Locks(), l.Runtime}
}

// Create makes every directory, mode 0700, chmod'ed after MkdirAll because
// MkdirAll applies the umask.
func (l Layout) Create() error {
	for _, dir := range l.Directories() {
		if err := os.MkdirAll(dir, DirMode); err != nil {
			return fmt.Errorf("cannot create %s: %w", dir, err)
		}
		if err := os.Chmod(dir, DirMode); err != nil {
			return fmt.Errorf("cannot restrict %s to %o: %w", dir, DirMode, err)
		}
	}
	return nil
}
