// Package config reads the configuration and, whatever it finds there, returns
// something usable: a missing or malformed file must never break the agent
// (plan.md §A14), so Load returns defaults and complaints, never an error.
//
// The configuration is the drop-ins each integration's install wrote, read in
// name order, with the user's own file on top (D-85). The user's file is the
// only one a person edits and the only one any program never writes.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/lassoColombo/agent-notify/internal/paths"
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
	// table without one belongs to something core never runs — the picker,
	// which a keybinding starts — and is here for its settings (D-81).
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

// Load reads every drop-in and then the user's file. An absent file is not a
// complaint; a drop-in that does not parse is named and skipped; the user's
// file not parsing is the one refusal.
func Load(layout paths.Layout) (Config, []error) {
	merged := map[string]any{}
	var problems []error

	dropIns, _ := filepath.Glob(filepath.Join(layout.DropIns(), "*.toml"))
	for _, file := range dropIns {
		data, err := os.ReadFile(file)
		if err != nil {
			problems = append(problems, fmt.Errorf("cannot read %s, skipping it: %w", file, err))
			continue
		}
		problems = append(problems, mergeInto(merged, data, file, false)...)
	}

	data, err := os.ReadFile(layout.ConfigFile)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		problems = append(problems, fmt.Errorf("cannot read %s, using defaults: %w", layout.ConfigFile, err))
	default:
		problems = append(problems, mergeInto(merged, data, layout.ConfigFile, true)...)
	}
	config, valueProblems := decode(merged)
	return config, append(problems, valueProblems...)
}

// ErrRefused marks the user's file not parsing and being ignored whole. An
// integration's settings section is unreachable then, which is a different
// answer from "your table is not there".
var ErrRefused = errors.New("configuration refused")

// Parse is Load for one file's bytes, with no drop-ins.
func Parse(data []byte, name string) (Config, []error) {
	merged := map[string]any{}
	problems := mergeInto(merged, data, name, true)
	config, valueProblems := decode(merged)
	return config, append(problems, valueProblems...)
}

// mergeInto lays one file over what was read before it. A file that does not
// parse as TOML contributes nothing; one that parses but says something
// unrecognised keeps everything it got right.
func mergeInto(into map[string]any, data []byte, name string, theUsers bool) []error {
	var read map[string]any
	if err := toml.Unmarshal(data, &read); err != nil {
		var decodeErr *toml.DecodeError
		problem := fmt.Errorf("%s could not be read and was ignored entirely: %w", name, err)
		if errors.As(err, &decodeErr) {
			problem = fmt.Errorf("%s is malformed and was ignored entirely:\n%s", name, decodeErr.String())
		}
		if theUsers {
			problem = fmt.Errorf("%w, using defaults: %w", problem, ErrRefused)
		}
		return []error{problem}
	}
	deepMerge(into, read)
	return unknownKeys(data, name)
}

// deepMerge lays over on top of into: a table over a table merges, anything
// else replaces.
func deepMerge(into, over map[string]any) {
	for key, value := range over {
		if overTable, isTable := value.(map[string]any); isTable {
			if intoTable, wasTable := into[key].(map[string]any); wasTable {
				deepMerge(intoTable, overTable)
				continue
			}
		}
		into[key] = value
	}
}

// decode turns the merged tables into a Config, every value checked.
func decode(merged map[string]any) (Config, []error) {
	config := Defaults()
	encoded, err := toml.Marshal(merged)
	if err != nil {
		return config, []error{fmt.Errorf("the configuration cannot be re-encoded, using defaults: %w", err)}
	}
	if err := toml.Unmarshal(encoded, &config); err != nil {
		return Defaults(), []error{fmt.Errorf("the configuration cannot be decoded, using defaults: %w", err)}
	}
	return config, config.problemsWith("the configuration")
}

// WriteDropIn files one integration's table, replacing what it wrote before,
// and says where. The directory is created 0700 and the file written 0600,
// like everything else agent-notify owns.
func WriteDropIn(layout paths.Layout, name, table string) (string, error) {
	if err := os.MkdirAll(layout.DropIns(), paths.DirMode); err != nil {
		return "", fmt.Errorf("cannot create %s: %w", layout.DropIns(), err)
	}
	path := layout.DropIn(name)
	temporary := path + ".writing"
	if err := os.WriteFile(temporary, []byte(table), paths.FileMode); err != nil {
		return "", fmt.Errorf("cannot write %s: %w", path, err)
	}
	if err := os.Rename(temporary, path); err != nil {
		return "", fmt.Errorf("cannot write %s: %w", path, err)
	}
	return path, nil
}

// RemoveDropIn takes one integration's table away. Absent is done.
func RemoveDropIn(layout paths.Layout, name string) error {
	if err := os.Remove(layout.DropIn(name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
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
