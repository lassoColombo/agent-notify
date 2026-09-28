// Package config reads the one configuration file and, whatever it finds there,
// returns something usable: a missing or malformed file must never break the
// agent (plan.md §A14), so Load returns defaults and complaints, never an error.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"time"

	"github.com/lassoColombo/agent-notify/session"
	"github.com/pelletier/go-toml/v2"
)

// Config is the whole file: what a person has an opinion about, and nothing
// else. Timeouts and intervals are named constants beside the code they bound
// (R18, D-62).
type Config struct {
	// KeepEndedSessions is how long an ended session's record survives, so
	// that resuming it is recognised as a return rather than a birth.
	KeepEndedSessions session.Duration `toml:"keep-ended-sessions"`

	// HistoryMessages and HistoryChanges bound the per-session history by
	// count (§A7.7). Zero keeps none.
	HistoryMessages int `toml:"history-messages"`
	HistoryChanges  int `toml:"history-changes"`

	// AgentNotifyBinary is where agent-notify lives, for whoever has to start a
	// session-watcher: a hook's PATH is not your shell's, and inside
	// agent-notify-claude "this executable" is the wrong answer.
	AgentNotifyBinary string `toml:"agent-notify-binary"`

	// Agent is keyed by the agent's name, which never appears in core's source
	// (R10).
	Agent map[string]Agent `toml:"agent"`

	// Integration is keyed by the tool's name, one table per program.
	Integration map[string]Integration `toml:"integration"`

	Container Container `toml:"container"`
}

// Agent is what core needs to recognise one agent's processes.
type Agent struct {
	// Binary is matched against the process ancestry (plan.md §A8.4).
	Binary string `toml:"binary"`
}

// Integration is one tool's table, and everything in it is the user's to write.
type Integration struct {
	// Enabled is a pointer so that absent and false are distinguishable.
	// Writing the table is the ask, so absent means enabled.
	Enabled *bool `toml:"enabled"`

	// Binary is this program, and naming one means "core may run this". A
	// table without one belongs to something core never runs — a menu bar
	// launchd starts — and is here for its settings (D-81).
	Binary string `toml:"binary"`

	// Settings is handed to the integration verbatim and never read by core
	// (R7).
	Settings map[string]any `toml:"settings"`
}

func (i Integration) IsEnabled() bool { return i.Enabled == nil || *i.Enabled }

// Container carries the one thing about containers that is policy rather than
// a property of any one of them.
type Container struct {
	// Order is outermost first. Nesting is not discoverable (plan.md §A11).
	Order []string `toml:"order"`
}

// Enabled is every integration table that is on, sorted.
func (c Config) Enabled() []string {
	var names []string
	for name, integration := range c.Integration {
		if integration.IsEnabled() {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// Runnable is every enabled integration with a binary: what core may run.
func (c Config) Runnable() []string {
	var names []string
	for _, name := range c.Enabled() {
		if c.Integration[name].Binary != "" {
			names = append(names, name)
		}
	}
	return names
}

// Defaults is the configuration of a machine with no configuration file.
func Defaults() Config {
	return Config{
		KeepEndedSessions: session.NewDuration(7 * 24 * time.Hour),
		HistoryMessages:   20,
		HistoryChanges:    100,
		Agent:             map[string]Agent{},
		Integration:       map[string]Integration{},
		Container:         Container{},
	}
}

// Load reads the file at path. An absent file is not a complaint.
func Load(path string) (Config, []error) {
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Defaults(), nil
	case err != nil:
		return Defaults(), []error{fmt.Errorf("cannot read %s, using defaults: %w", path, err)}
	}
	return Parse(data, path)
}

// ErrRefused marks a file that does not parse and was refused whole. An
// integration's settings section is unreachable then, which is a different
// answer from "your table is not there".
var ErrRefused = errors.New("configuration refused")

// Parse is Load without the filesystem. A file that does not parse as TOML is
// refused whole; one that parses but says something unrecognised keeps
// everything it got right.
func Parse(data []byte, name string) (Config, []error) {
	config := Defaults()
	if err := toml.NewDecoder(bytes.NewReader(data)).Decode(&config); err != nil {
		var decodeErr *toml.DecodeError
		if errors.As(err, &decodeErr) {
			return Defaults(), []error{fmt.Errorf(
				"%s is malformed and was ignored entirely, using defaults:\n%s: %w",
				name, decodeErr.String(), ErrRefused)}
		}
		return Defaults(), []error{fmt.Errorf(
			"%s could not be read, using defaults: %w: %w", name, err, ErrRefused)}
	}

	problems := unknownKeys(data, name)
	problems = append(problems, config.problemsWith(name)...)
	return config, problems
}

// unknownKeys re-decodes strictly, purely to report typos: the first pass must
// populate the config whatever happens.
func unknownKeys(data []byte, name string) []error {
	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var ignored Config
	err := decoder.Decode(&ignored)
	if err == nil {
		return nil
	}

	var strict *toml.StrictMissingError
	if !errors.As(err, &strict) {
		return nil
	}
	return []error{fmt.Errorf("%s: %d key(s) not recognised and ignored:\n%s",
		name, len(strict.Errors), strict.String())}
}

// problemsWith checks the values the file did supply. Every problem replaces
// one value with its default and keeps going.
func (c *Config) problemsWith(name string) []error {
	defaults := Defaults()
	var problems []error

	if bad := c.KeepEndedSessions.Unreadable(); bad != "" {
		problems = append(problems, fmt.Errorf(
			"%s: keep-ended-sessions = %s; write it as \"168h\" or \"30m\"; using %s",
			name, c.KeepEndedSessions, defaults.KeepEndedSessions))
		c.KeepEndedSessions = defaults.KeepEndedSessions
	} else if c.KeepEndedSessions.Duration() <= 0 {
		problems = append(problems, fmt.Errorf(
			"%s: keep-ended-sessions must be positive, not %s; using %s",
			name, c.KeepEndedSessions, defaults.KeepEndedSessions))
		c.KeepEndedSessions = defaults.KeepEndedSessions
	}

	for _, count := range []struct {
		key   string
		value *int
	}{
		{"history-messages", &c.HistoryMessages},
		{"history-changes", &c.HistoryChanges},
	} {
		if *count.value < 0 {
			problems = append(problems, fmt.Errorf(
				"%s: %s cannot be negative; using 0, which keeps none", name, count.key))
			*count.value = 0
		}
	}

	if c.Agent == nil {
		c.Agent = map[string]Agent{}
	}
	if c.Integration == nil {
		c.Integration = map[string]Integration{}
	}
	return problems
}
