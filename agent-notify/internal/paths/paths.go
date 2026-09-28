// Package paths resolves every location agent-notify owns, on every platform it
// runs on, from the environment alone.
//
// It exists so that no other package ever joins a path itself. A path built in
// two places is a path that will differ in two places, and the two processes
// that disagree here — a hook writing a record and a session-watcher reading it
// — fail silently when they do: the record is written, nothing reads it, and
// nothing reports an error.
//
// See plan.md §A7.2 for the layout and §A14 (D-20) for the one override.
package paths

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// TheVariableThatNamesTheRoot is the one environment override the design
// allows. Setting it moves durable state, volatile runtime state and the configuration file beneath a
// single directory, which is what makes "run an isolated instance" one step
// rather than three (plan.md §A14, D-20).
const TheVariableThatNamesTheRoot = "AGENT_NOTIFY_ROOT"

// DirMode and FileMode are not suggestions. A record carries the text of what an
// agent last said, which is not world-readable material (plan.md §A7.2).
const (
	DirMode  os.FileMode = 0o700
	FileMode os.FileMode = 0o600
)

// Layout is every directory and file agent-notify owns, already resolved.
//
// The two directories are separate because one must survive a reboot and the
// other must not: a socket or a lock that outlived the process holding it is a
// ghost that takes manual cleanup, and a session record that vanished on reboot
// is a session you can no longer resume.
type Layout struct {
	// Root is the AGENT_NOTIFY_ROOT that produced this layout, or "" when the
	// platform defaults did. Diagnostics print it, because "why is it looking
	// there" is the first question an isolated instance provokes.
	Root string

	// State survives a reboot: the records themselves.
	State string

	// Runtime does not survive a reboot, and must not: sockets and the
	// singleton lock.
	Runtime string

	// ConfigFile is the only file here that agent-notify reads and never writes.
	ConfigFile string
}

// FromEnvironment works out the layout for this process: the one override if
// it is set, and the platform's own conventions otherwise.
//
// It touches no disk. Creating the directories is Create, separately, because
// the read paths (plan.md §A7.6) must work without ever having written.
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

// Under is the layout beneath one named root, and it is the ONLY place that
// knows what the root contains.
//
// It exists because there were two such places (D-69). The subscriber SDK, when
// handed a root outright rather than through the environment, built the layout
// again by concatenating strings — no [filepath.Abs], no [filepath.Join], and
// no error for a path that cannot be one. So the same root resolved two
// different ways depending on which door it came in by: a relative one landed
// against the process's own working directory, which for a supervised child is
// not yours, and a trailing slash produced `root//config.toml`. Every Fake in
// every test went through that second copy.
//
// This is the package comment's own rule applied to the package: a path built
// in two places is a path that will differ in two places.
//
// Absolute, because a root is a place and not a direction from wherever the
// caller happens to be standing. The process that writes a record and the
// process that reads it do not share a working directory, and a relative root
// is how they come to disagree about where the record is while both believing
// they are right.
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

// defaultRuntimeDir picks a directory that is already per-user and already
// mode 0700 wherever one exists, and falls back to a uid-suffixed directory in
// the shared temporary space where none does. The suffix is not decoration:
// /tmp is writable by everyone, and an unsuffixed name there is a name another
// user can take first.
func defaultRuntimeDir() (string, error) {
	if runtime.GOOS == "darwin" {
		// TMPDIR under launchd is /var/folders/<..>/T, per-user and 0700.
		// os.TempDir falls back to /tmp when it is unset, which is why the
		// uid suffix below is not darwin-only paranoia.
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

// Ended holds records filed away rather than deleted: death is a transition,
// and a session that returns is recognised (plan.md §A17 R6).
func (l Layout) Ended() string { return filepath.Join(l.State, "ended") }

// Locks holds one lock file per session key. They are never renamed and never
// deleted while referenced, because a lock whose file was replaced is a lock two
// writers can hold at once (plan.md §A7.2).
func (l Layout) Locks() string { return filepath.Join(l.State, "locks") }

// History holds the last few things each session said and the last few times
// its state moved — read on demand, for one session, never delivered with a
// delta (§A7.7). It is its own directory so that a session moving between
// sessions/ and ended/ never has to drag it along.
func (l Layout) History() string { return filepath.Join(l.State, "history") }

// SessionFile, EndedFile, HistoryFile and LockFile take a session key, already
// sanitised by its constructor in §A6 — this package does not re-validate it,
// it only joins.
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

// SubscribersSocket is the stream link, session-watcher to integrations.
func (l Layout) SubscribersSocket() string {
	return filepath.Join(l.Runtime, "subscribers.sock")
}

// IntegrationsFile is what the session-watcher knows about its integrations, written for
// doctor to read from another process.
//
// It is in the runtime directory because it must not survive a reboot: a report
// about children of a session-watcher that no longer exists is worse than no
// report at all.
func (l Layout) IntegrationsFile() string {
	return filepath.Join(l.Runtime, "integrations.json")
}

// SessionChangesSocket is the datagram link, record-agent-event to session-watcher.
func (l Layout) SessionChangesSocket() string {
	return filepath.Join(l.Runtime, "session-changes.sock")
}

// LogFile is written by every process — the session-watcher, record-agent-event,
// the commands, and every integration — which is why it is named after none of
// them. A hook must never write to stdout (plan.md §A17 R2), so this is where it
// says what happened, and one file is what lets a session be followed across all
// of them.
//
// It is in the state directory rather than the runtime one. The log's whole job
// is to still be there when you come and ask what went wrong, and a directory
// the system sweeps — or a reboot empties — loses the evidence for exactly the
// failures worth reading about.
func (l Layout) LogFile() string { return filepath.Join(l.State, "agent-notify.log") }

// Directories lists every directory agent-notify creates, deepest last so that
// creating them in order is safe.
func (l Layout) Directories() []string {
	return []string{l.State, l.Sessions(), l.Ended(), l.History(), l.Locks(), l.Runtime}
}

// Create makes every directory, mode 0700.
//
// MkdirAll applies the umask, so each directory is chmod'ed afterwards. Only
// directories agent-notify owns are touched: a umask that would have made
// ~/Library/Application Support group-readable is the user's business, but the
// directory holding what an agent said is ours.
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

// MaxSocketPath is the longest AF_UNIX path this platform will bind.
//
// sun_path is a fixed 104-byte array on darwin and 108 on linux, and the kernel
// requires room for a terminating NUL, so the usable maximum is one less.
//
// [verified 2026-09-17, macOS 26] 103 bytes binds for both "unix" and
// "unixgram"; 104 fails with EINVAL. The 104 in plan.md §A7.2 is the size of the
// struct field, not the longest usable path — checking against it would admit
// exactly the one path that then fails at bind, which is the failure this check
// exists to prevent.
func MaxSocketPath() int {
	switch runtime.GOOS {
	case "linux":
		return 107
	default:
		return 103
	}
}

// Sockets lists every path that will be bound, so the check below and the
// diagnostics both stay honest when a third one is added.
func (l Layout) Sockets() []string {
	return []string{l.SubscribersSocket(), l.SessionChangesSocket()}
}

// CheckSockets reports every socket path too long to bind, naming the path and
// what to do about it. The alternative is a bare "invalid argument" from bind,
// which names nothing and points nowhere.
func (l Layout) CheckSockets() error {
	limit := MaxSocketPath()
	var problems []error
	for _, path := range l.Sockets() {
		if len(path) > limit {
			problems = append(problems, fmt.Errorf(
				"socket path is %d bytes, over the %d this platform allows: %s\n"+
					"    set %s to a shorter directory", len(path), limit, path, TheVariableThatNamesTheRoot))
		}
	}
	return errors.Join(problems...)
}

// LongestSocket is what a diagnostic prints when everything is fine: a margin,
// so that a path at 101 of 103 can be seen coming.
func (l Layout) LongestSocket() (path string, length int) {
	for _, candidate := range l.Sockets() {
		if len(candidate) > length {
			path, length = candidate, len(candidate)
		}
	}
	return path, length
}

// FromEnvironmentOrUnder is one or the other: the root this process's
// environment names, or the one it is handed.
//
// Both branches end in the same code, and that is the point of it looking this
// dull: naming a root outright used to reach a second, wronger implementation
// of what a root contains (D-69). It is here rather than beside either caller
// because there are two of them now — the SDK an integration links, and core's
// own client of the socket.
func FromEnvironmentOrUnder(root string) (Layout, error) {
	if root == "" {
		return FromEnvironment()
	}
	return Under(root)
}
