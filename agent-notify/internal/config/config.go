// Package config reads the configuration and, whatever it finds there, returns
// something usable: a missing or malformed file must never break the agent
// (plan.md §A14), so Load returns defaults and complaints, never an error.
//
// The configuration is one YAML file, the user's, and no program writes it
// (D-66, D-101, D-103). It holds opinions: what runs is every
// `agent-notify-<name>` on PATH (D-102), and a name appears in the file only
// to be turned off or tuned. Nothing outside core reads the file: an
// integration is handed its own `settings` by core (§A10.4).
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/lassoColombo/agent-notify/internal/onpath"
	"github.com/lassoColombo/agent-notify/internal/paths"
	"github.com/lassoColombo/agent-notify/session"
	"gopkg.in/yaml.v3"
)

// Config is the whole file: what a person has an opinion about, and nothing
// else. Timeouts and intervals are named constants beside the code they bound
// (R18, D-62).
type Config struct {
	// KeepEndedSessions is how long an ended session's record survives, so
	// that resuming it is recognised as a return rather than a birth.
	KeepEndedSessions session.Duration `yaml:"keep-ended-sessions"`

	// HistoryMessages and HistoryChanges bound the per-session history by
	// count (§A7.7). Zero keeps none.
	HistoryMessages int `yaml:"history-messages"`
	HistoryChanges  int `yaml:"history-changes"`

	// Integrations is keyed by the program's name, one entry per program a
	// person has something to say about.
	Integrations map[string]Integration `yaml:"integrations"`

	// Containers is the order, outermost first. Nesting is not discoverable
	// (plan.md §A11); with one container the order is that one (D-101).
	Containers []string `yaml:"containers"`

	// Installed is every agent-notify-<name> on PATH: the set the file's
	// opinions apply to (D-102). Not a key, and not read with the file: PATH
	// is scanned the first time something asks, through OnPath, and most
	// processes never ask (D-108). A test hands over its own list by setting
	// it.
	Installed []string `yaml:"-"`
	scanned   bool
}

// Integration is one program's entry, and everything in it is the user's to
// write.
type Integration struct {
	// Enabled is a pointer so that absent and false are distinguishable.
	// Absent means enabled.
	Enabled *bool `yaml:"enabled"`

	// Settings is handed to the integration verbatim and never read by core
	// (R7).
	Settings map[string]any `yaml:"settings"`
}

func (i Integration) IsEnabled() bool { return i.Enabled == nil || *i.Enabled }

// Program is what core runs for one name: the naming convention is the whole
// of what core knows about an integration (R10), and PATH is where it is
// (D-101). A bare name, so that exec resolves it where the process runs.
func Program(name string) string { return "agent-notify-" + name }

// OnPath is Installed, scanning PATH the first time and not before. The
// scan is one ReadDir per PATH entry, and the two processes that act on the
// answer are the session-watcher and the hook; a render, a preview and
// `list` open the configuration and never ask (D-108).
func (c *Config) OnPath() []string {
	if !c.scanned && c.Installed == nil {
		c.Installed, c.scanned = onpath.Integrations(), true
	}
	return c.Installed
}

// Enabled is every installed integration that is not turned off, sorted:
// what core may run.
func (c *Config) Enabled() []string {
	var names []string
	for _, name := range c.OnPath() {
		if c.Integrations[name].IsEnabled() {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// WrittenAndNotOnPath is every integration the file has an opinion about
// with no program behind it: a misspelling, or something uninstalled. It is
// asked by `doctor` and the session-watcher, the two that look at PATH, and
// not by Load, which does not (D-108).
func (c *Config) WrittenAndNotOnPath(file string) []error {
	installed := c.OnPath()
	var problems []error
	for _, name := range slices.Sorted(maps.Keys(c.Integrations)) {
		if !slices.Contains(installed, name) {
			problems = append(problems, fmt.Errorf(
				"%s: integrations.%s is written, and there is no %s on PATH",
				file, name, Program(name)))
		}
	}
	return problems
}

// Defaults is the configuration of a machine with no configuration file.
func Defaults() Config {
	return Config{
		KeepEndedSessions: session.NewDuration(7 * 24 * time.Hour),
		HistoryMessages:   20,
		HistoryChanges:    100,
		Integrations:      map[string]Integration{},
	}
}

// Load reads the user's file. An absent file is not a complaint; one that
// does not parse is the one refusal. PATH is left for OnPath.
func Load(layout paths.Layout) (Config, []error) {
	data, err := os.ReadFile(layout.ConfigFile)
	var problems []error
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		problems = append(problems, fmt.Errorf("cannot read %s, using defaults: %w", layout.ConfigFile, err))
		data = nil
	}
	config, parseProblems := Parse(data, layout.ConfigFile)
	return config, append(problems, parseProblems...)
}

// ErrRefused marks the user's file not parsing and being ignored whole. An
// integration's settings section is unreachable then, which is a different
// answer from "your entry is not there".
var ErrRefused = errors.New("configuration refused")

// Parse is Load for one file's bytes. A file that does
// not parse as YAML is refused whole; one that parses but says something
// unrecognised keeps everything it got right.
func Parse(data []byte, name string) (Config, []error) {
	config := Defaults()
	if err := yaml.Unmarshal(data, &config); err != nil {
		return Defaults(), []error{fmt.Errorf(
			"%s is malformed and was ignored entirely: %w, using defaults: %w", name, err, ErrRefused)}
	}
	if config.Integrations == nil {
		config.Integrations = map[string]Integration{}
	}
	problems := UnknownKeys(data, &Config{}, name)
	return config, append(problems, config.problemsWith(name)...)
}

// UnknownKeys re-decodes strictly into a throwaway of the same shape, purely
// to report typos: the first pass must populate the value whatever happens.
// `where` names the file or the section, for the message.
func UnknownKeys(data []byte, into any, where string) []error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	err := decoder.Decode(into)
	var strict *yaml.TypeError
	if err == nil || errors.Is(err, io.EOF) || !errors.As(err, &strict) {
		return nil
	}
	return []error{fmt.Errorf("%s: %d key(s) not recognised and ignored:\n  %s",
		where, len(strict.Errors), strings.Join(strict.Errors, "\n  "))}
}

// problemsWith checks the values the file did supply. Every problem replaces
// one value with its default and keeps going.
func (c *Config) problemsWith(name string) []error {
	defaults := Defaults()
	var problems []error

	if bad := c.KeepEndedSessions.Unreadable(); bad != "" {
		problems = append(problems, fmt.Errorf(
			"%s: keep-ended-sessions = %s; write it as 168h or 30m; using %s",
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
	return problems
}
