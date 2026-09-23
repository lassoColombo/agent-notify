package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
)

// install writes this program into Claude's own settings.json, which is how a
// hook comes to exist at all.
//
// It is idempotent and it is conservative: it adds what is missing, updates a
// stale path to this program, and touches nothing else. A person's settings
// file holds their own hooks, their model, their permissions — losing any of
// that to an installer would be unforgivable, so everything not ours is
// round-tripped through json.RawMessage and written back exactly as it came.
func install(arguments []string) int {
	flags := flag.NewFlagSet("install", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	print := flags.Bool("print", false, "show what would be written and write nothing")
	settingsPath := flags.String("settings", defaultSettings(), "Claude's settings file")

	if err := flags.Parse(arguments); err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify-claude install: %v\n", err)
		return 2
	}

	program, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify-claude install: cannot find my own path: %v\n", err)
		return 1
	}

	settings, err := readSettings(*settingsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify-claude install: %v\n", err)
		return 1
	}

	updated, changes := merge(settings, program)
	rendered, err := json.MarshalIndent(updated, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify-claude install: %v\n", err)
		return 1
	}

	if *print {
		fmt.Println(string(rendered))
		return 0
	}
	if len(changes) == 0 {
		fmt.Printf("already installed in %s\n", *settingsPath)
		return 0
	}
	if err := writeSettings(*settingsPath, rendered); err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify-claude install: %v\n", err)
		return 1
	}
	for _, change := range changes {
		fmt.Println(change)
	}
	fmt.Printf("wrote %s\n", *settingsPath)
	return 0
}

func defaultSettings() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "settings.json"
	}
	return filepath.Join(home, ".claude", "settings.json")
}

// readSettings returns the file as a map of untouched raw values. An absent
// file is an empty one: installing into a machine that has never configured
// Claude is the ordinary case.
func readSettings(path string) (map[string]json.RawMessage, error) {
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]json.RawMessage{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	settings := map[string]json.RawMessage{}
	if err := json.Unmarshal(content, &settings); err != nil {
		return nil, fmt.Errorf("%s is not readable as JSON, and this will not overwrite it: %w", path, err)
	}
	return settings, nil
}

// writeSettings replaces the file atomically, keeping a copy of what was there.
// An installer that corrupts a settings file must leave the old one behind.
func writeSettings(path string, content []byte) error {
	if previous, err := os.ReadFile(path); err == nil {
		if err := os.WriteFile(path+".before-agent-notify", previous, 0o600); err != nil {
			return fmt.Errorf("cannot keep a copy of %s: %w", path, err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary := path + ".writing"
	if err := os.WriteFile(temporary, append(content, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

// one hook entry as Claude's settings spell it.
type command struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

type group struct {
	Matcher string            `json:"matcher,omitempty"`
	Hooks   []json.RawMessage `json:"hooks"`
}

// merge adds this program to every hook it subscribes to, and reports what it
// changed.
func merge(settings map[string]json.RawMessage, program string) (map[string]json.RawMessage, []string) {
	hooks := map[string][]json.RawMessage{}
	if raw, present := settings["hooks"]; present {
		_ = json.Unmarshal(raw, &hooks)
	}

	var changes []string
	for _, event := range SubscribedHooks {
		wanted := command{Type: "command", Command: fmt.Sprintf("%q %s", program, event)}
		groups := hooks[event]

		if updated, what := place(groups, wanted, program); what != "" {
			hooks[event] = updated
			changes = append(changes, what+" "+event)
		}
	}

	if len(changes) == 0 {
		return settings, nil
	}
	encoded, err := json.Marshal(hooks)
	if err != nil {
		return settings, nil
	}
	settings["hooks"] = encoded
	return settings, changes
}

// place puts one command into one hook's groups, replacing an entry that names
// this program at a different path and leaving everybody else's alone.
func place(groups []json.RawMessage, wanted command, program string) ([]json.RawMessage, string) {
	name := filepath.Base(program)

	for i, raw := range groups {
		var parsed group
		if err := json.Unmarshal(raw, &parsed); err != nil {
			continue
		}
		for j, rawCommand := range parsed.Hooks {
			var existing command
			if err := json.Unmarshal(rawCommand, &existing); err != nil {
				continue
			}
			if !namesUs(existing.Command, name) {
				continue
			}
			if existing.Command == wanted.Command {
				return groups, ""
			}
			encoded, err := json.Marshal(wanted)
			if err != nil {
				return groups, ""
			}
			parsed.Hooks[j] = encoded
			if regrouped, err := json.Marshal(parsed); err == nil {
				groups[i] = regrouped
				return groups, "updated the path for"
			}
			return groups, ""
		}
	}

	encoded, err := json.Marshal(wanted)
	if err != nil {
		return groups, ""
	}
	added, err := json.Marshal(group{Hooks: []json.RawMessage{encoded}})
	if err != nil {
		return groups, ""
	}
	return append(slices.Clone(groups), added), "added"
}

// namesUs recognises this program however its path was written, so that moving
// the binary updates the entry instead of adding a second one.
func namesUs(existing, name string) bool {
	for _, field := range splitCommand(existing) {
		if filepath.Base(field) == name {
			return true
		}
	}
	return false
}

// splitCommand is a deliberately small shell-word split: enough to find a
// quoted or unquoted path, and not pretending to be a shell.
func splitCommand(line string) []string {
	var fields []string
	var current []rune
	var quote rune
	for _, r := range line {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote == 0 && (r == '"' || r == '\''):
			quote = r
		case quote == 0 && r == ' ':
			if len(current) > 0 {
				fields = append(fields, string(current))
				current = nil
			}
		default:
			current = append(current, r)
		}
	}
	if len(current) > 0 {
		fields = append(fields, string(current))
	}
	return fields
}
