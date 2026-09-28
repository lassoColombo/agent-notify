// Package config reads the one configuration file and, whatever it finds there,
// returns something usable.
//
// The rule that outranks every other rule in plan.md §A14: a missing or
// malformed configuration must never break the agent. Load therefore has no
// error return at all. It returns defaults and a list of complaints, and the
// caller decides how loudly to say them — record-agent-event logs and exits 0
// (§A17 R2), doctor prints and exits 1.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/lassoColombo/agent-notify/session"
	"github.com/pelletier/go-toml/v2"
)

// Config is the whole file: what a person has an opinion about, and nothing
// else.
//
// Timeouts, intervals and lock waits are deliberately not here. Every one of
// them was mechanism — how long to wait for a subprocess, how often to sweep,
// how long to hold a lock — and a person editing this file has no information
// with which to choose a better value than the code does. They are named
// constants beside the code they bound now, which keeps R18 (every duration has
// a name and a defined behaviour on expiry) and drops the pretence that any of
// them was a preference.
type Config struct {
	// KeepEndedSessions is how long an ended session's record survives, so
	// that resuming it is recognised as a return rather than a birth.
	//
	// It is the only retention duration, and there was briefly a second one —
	// how long an ended session stayed on displays. That was presentation
	// policy in core (R9), it made a display's contents change because the
	// clock moved rather than because anything happened (R26), and it was in
	// direct conflict with the picker, whose whole job is to show you the
	// sessions you can still resume. Whether an ended session is rendered is
	// the subscriber's own decision (D-26).
	//
	// It is also the only duration left in this file, and it is written the way
	// a person writes one: "168h" is the week it defaults to, "30m" is half an
	// hour (session.Duration, D-78). Everything else that was once configurable
	// here was a timeout, an interval or a lock wait — mechanism rather than
	// preference — and mechanism now lives as a named constant beside the code
	// it bounds. R18 is satisfied the same way it always was: every duration
	// has a name and a defined behaviour on expiry; the name is simply in the
	// source now rather than in the file.
	KeepEndedSessions session.Duration `toml:"keep-ended-sessions"`

	// HistoryMessages and HistoryChanges bound the per-session history by
	// count, not by time (§A7.7). Zero keeps none, which §A15 requires to be
	// expressible.
	HistoryMessages int `toml:"history-messages"`
	HistoryChanges  int `toml:"history-changes"`

	// AgentNotifyBinary is where the agent-notify binary lives, for the one
	// job that needs to start it: a hook whose poke found no session-watcher.
	//
	// It exists because a hook's PATH is not your shell's PATH, and because the
	// hook library is linked into binaries that are not agent-notify — inside
	// agent-notify-claude, "this executable" is the wrong answer. Empty means
	// "work it out", which succeeds on most machines.
	AgentNotifyBinary string `toml:"agent-notify-binary"`

	// Agent is keyed by the agent's name, which is the user's word for it and
	// never appears in core's source (plan.md §A17 R10).
	Agent map[string]Agent `toml:"agent"`

	// Integration is keyed by the tool's name. One table per integration, not
	// three: the role it plays is declared by the integration itself (§A10.3),
	// so there is nothing left to configure per role.
	Integration map[string]Integration `toml:"integration"`

	Container Container `toml:"container"`
}

// Agent is what core needs to recognise one agent's processes.
type Agent struct {
	// Binary is matched against the process ancestry (plan.md §A8.4).
	Binary string `toml:"binary"`
}

// Integration is one tool's table, and everything in it is the user's to write.
//
// What an integration NEEDS is not here and must not be: which environment
// variables it reads is knowledge the integration already holds in its own
// source, and a second copy in a file the user edits is a copy that can
// disagree with the first — silently, because a capture that names the wrong
// variable produces no error, just a session with no coordinates (D-57).
type Integration struct {
	// Enabled is a pointer so that an absent key and an explicit false are
	// distinguishable. Writing the table at all is the act of asking for the
	// integration, so absence means enabled.
	Enabled *bool `toml:"enabled"`

	// Binary is this integration's program, and naming one means "core may run
	// this": the hook runs it on the agent's path, the session-watcher runs it
	// to ask what it answers and then to render or to focus. Looked up on PATH
	// unless it is an absolute path.
	//
	// Leaving it out is a decision rather than an omission. A table with no
	// binary belongs to something core never runs — a menu bar, which has to
	// own its process and is started by launchd — and it is here for its
	// settings, which is what a table is for.
	Binary string `toml:"binary"`

	// Settings is handed to the integration verbatim and never read by core.
	// Glyphs, colours and thresholds all live in here: core must not know
	// sketchybar's colour keys (plan.md §A17 R7).
	Settings map[string]any `toml:"settings"`
}

// IsEnabled reports whether this integration should run. Absence means yes,
// because writing the table is how you ask for it.
func (i Integration) IsEnabled() bool { return i.Enabled == nil || *i.Enabled }

// Container carries the one thing about containers that is policy rather than a
// property of any single one of them.
type Container struct {
	// Order is outermost first. It is configured rather than discovered
	// because nesting is not discoverable: zellij inside a window that
	// aerospace manages looks, from inside, exactly like zellij on its own
	// (plan.md §A11).
	Order []string `toml:"order"`
}

// Defaults is the configuration of a machine with no configuration file, and the
// starting point for one that has. It is a function rather than a variable
// because a package-level map is a package-level map somebody will mutate.
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

// Load reads the file at path. It never fails.
//
// An absent file is not a complaint: running with no configuration is the
// supported case, not a degraded one. Everything else is — a file that exists
// and cannot be understood is a file somebody meant something by.
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

// ErrRefused marks the one grade of failure that loses everything the user
// wrote: a file that does not parse, which is refused whole rather than decoded
// halfway. Everything else — an unrecognised key, a duration that is not one —
// is a note about a file that was otherwise used, and a caller that cannot act
// on notes should ignore them rather than treat them as refusal.
//
// It exists because the SDK hands an integration its own settings section and
// has to distinguish "your table is not there" from "nothing in this file is
// there". Those look identical from the outside and mean opposite things.
var ErrRefused = errors.New("configuration refused")

// Parse is Load without the filesystem, which is what the tests use and what a
// future "check this file before I save it" would use.
//
// Failure is graded. A file that does not parse as TOML is refused whole,
// because a half-decoded file silently mixes the user's intent with defaults in
// a way nobody can see. A file that parses but says something unrecognised keeps
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

	problems := removedKeys(data, name)
	problems = append(problems, unknownKeys(data, name)...)
	problems = append(problems, config.problemsWith(name)...)
	return config, problems
}

// removedKeys names the keys that used to do something, so that a file carrying
// one gets an answer rather than the generic "not recognised" the strict pass
// would otherwise give.
//
// It is here because the generic message is true and useless: it says a key was
// ignored, not that the thing the key did has gone and what to do instead. A
// person whose display quietly stopped placing sessions deserves the sentence
// that explains it (D-57).
//
// It can be deleted once no config file in the world still has them.
func removedKeys(data []byte, name string) []error {
	var file struct {
		Integration     map[string]map[string]any `toml:"integration"`
		SubscriberQueue any                       `toml:"subscriber-queue"`
	}
	if err := toml.Unmarshal(data, &file); err != nil {
		// The first pass already reported whatever is wrong with it.
		return nil
	}

	var problems []error
	if file.SubscriberQueue != nil {
		problems = append(problems, fmt.Errorf(
			"%s: subscriber-queue is no longer read. There is no queue to bound: a "+
				"subscriber is sent the whole world and the newest one replaces any "+
				"still waiting, so falling behind costs worlds nobody would have "+
				"seen rather than a queue to size. Delete the line", name))
	}

	// Each key with the sentence that says what happened to it. "Ignored" on
	// its own is no use to somebody whose display quietly stopped placing
	// sessions; what they need is the thing the key did and where it went.
	why := map[string]string{
		"capture": "Capturing is now the integration's own job: it declares what it " +
			"needs in its own source and answers `capture-environment`",
		"focus": "Capturing is now the integration's own job: it declares what it " +
			"needs in its own source and answers `capture-environment`",
		"capture-environment": "Every integration is asked `capture-environment` now, " +
			"so there is nothing left to say yes to. One that reads nothing answers " +
			"with an empty object and core records nothing for it",
	}

	for tool, table := range file.Integration {
		for _, key := range []string{"capture", "focus", "capture-environment"} {
			if _, present := table[key]; !present {
				continue
			}
			problems = append(problems, fmt.Errorf(
				"%s: [integration.%s] %s is no longer read. %s. Delete the line; "+
					"`agent-notify install %s` prints the table it wants",
				name, tool, key, why[key], tool))
		}
	}
	slices.SortFunc(problems, func(a, b error) int {
		return strings.Compare(a.Error(), b.Error())
	})
	return problems
}

// unknownKeys re-decodes strictly, purely to report typos.
//
// It is a second pass on purpose: the first pass must populate the config
// whatever happens, and a strict decoder that stops at the first unknown key
// would decide how much of the file survives based on the order the user
// happened to write it in.
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
		// Anything else was already reported by the first pass.
		return nil
	}
	return []error{fmt.Errorf("%s: %d key(s) not recognised and ignored:\n%s",
		name, len(strict.Errors), strict.String())}
}

// problemsWith checks the values the file did supply. Every problem it finds
// replaces one value with its default and keeps going: one bad duration should
// not cost the user the other twelve lines they got right.
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

	counts := []struct {
		key   string
		value *int
	}{
		{"history-messages", &c.HistoryMessages},
		{"history-changes", &c.HistoryChanges},
	}
	for _, count := range counts {
		if *count.value < 0 {
			problems = append(problems, fmt.Errorf(
				"%s: %s cannot be negative; using 0, which keeps none",
				name, count.key))
			*count.value = 0
		}
	}

	// A table with no `binary` used to be reported here as "nothing will happen
	// for it". It is a declaration now, not an omission (D-81): the absence is
	// how a client says core must not run it, which is what both macOS displays
	// and the picker say. `install` prints exactly such a table for all three,
	// so this check called its own output a failure.
	//
	// Where a binary IS still required, the requirement belongs to whatever
	// needs to run one — [container] order says so for a container, because a
	// container that cannot be run cannot answer `focus`.

	if c.Agent == nil {
		c.Agent = map[string]Agent{}
	}
	if c.Integration == nil {
		c.Integration = map[string]Integration{}
	}
	return problems
}
