package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// install writes this program into Claude's own settings.json, which is how a
// hook comes to exist at all, and nothing else: agent-notify's own
// configuration is yours and no program writes it (D-101).
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

	updated, changes, err := merge(settings, program)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify-claude install: %s: %v\n", *settingsPath, err)
		return 1
	}
	rendered := encode(updated, "  ")

	if *print {
		fmt.Print(string(rendered))
		return 0
	}
	if len(changes) == 0 {
		fmt.Printf("already installed in %s\n", *settingsPath)
	} else {
		if err := writeSettings(*settingsPath, rendered); err != nil {
			fmt.Fprintf(os.Stderr, "agent-notify-claude install: %v\n", err)
			return 1
		}
		for _, change := range changes {
			fmt.Println(change)
		}
		fmt.Printf("wrote %s\n", *settingsPath)
	}
	return 0
}

// uninstall takes this program out of every hook it was in. Everything else
// in the settings file is kept exactly as it was.
func uninstall(arguments []string) int {
	flags := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	settingsPath := flags.String("settings", defaultSettings(), "Claude's settings file")
	if err := flags.Parse(arguments); err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify-claude uninstall: %v\n", err)
		return 2
	}
	program, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify-claude uninstall: cannot find my own path: %v\n", err)
		return 1
	}
	settings, err := readSettings(*settingsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify-claude uninstall: %v\n", err)
		return 1
	}
	updated, removed, err := unmerge(settings, program)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-notify-claude uninstall: %s: %v\n", *settingsPath, err)
		return 1
	}
	if removed > 0 {
		if err := writeSettings(*settingsPath, encode(updated, "  ")); err != nil {
			fmt.Fprintf(os.Stderr, "agent-notify-claude uninstall: %v\n", err)
			return 1
		}
		fmt.Printf("removed %d hook(s) from %s\n", removed, *settingsPath)
	} else {
		fmt.Printf("not in %s\n", *settingsPath)
	}
	return 0
}

// unmerge is merge's opposite: every command naming this program goes, a
// group left empty goes with it, and everybody else's stays byte for byte.
func unmerge(settings map[string]json.RawMessage, program string) (map[string]json.RawMessage, int, error) {
	hooks, err := hooksIn(settings)
	if err != nil {
		return nil, 0, err
	}
	name := filepath.Base(program)
	ours := func(rawCommand json.RawMessage) bool {
		var existing command
		return json.Unmarshal(rawCommand, &existing) == nil && namesUs(existing.Command, name)
	}
	removed := 0
	for event, groups := range hooks {
		var kept []json.RawMessage
		for _, raw := range groups {
			var parsed group
			if err := json.Unmarshal(raw, &parsed); err != nil {
				kept = append(kept, raw)
				continue
			}
			remaining := slices.DeleteFunc(parsed.Hooks, ours)
			if len(remaining) == len(parsed.Hooks) {
				kept = append(kept, raw)
				continue
			}
			removed += len(parsed.Hooks) - len(remaining)
			if len(remaining) == 0 {
				continue
			}
			parsed.Hooks = remaining
			kept = append(kept, encode(parsed, ""))
		}
		if len(kept) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = kept
		}
	}
	if removed == 0 {
		return settings, 0, nil
	}
	if len(hooks) == 0 {
		delete(settings, "hooks")
	} else {
		settings["hooks"] = encode(hooks, "")
	}
	return settings, removed, nil
}

// defaultSettings is ~/.claude/settings.json, or wherever the user moved the
// whole configuration directory to.
//
// It reads the same variable the runtime half reads (see
// whereClaudeKeepsItsLiveSessions). An installer that wrote to ~/.claude while
// the hook read CLAUDE_CONFIG_DIR would report success and install nothing.
func defaultSettings() string {
	if configured := strings.TrimSpace(os.Getenv(configDirectoryVariable)); configured != "" {
		return filepath.Join(configured, "settings.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "settings.json"
	}
	return filepath.Join(home, ".claude", "settings.json")
}

// readSettings returns the file as a map of untouched raw values. An absent
// file is an empty one: installing into a machine that has never configured
// Claude is the ordinary case. So is a file with nothing in it, or `null`,
// which decodes to no map at all.
func readSettings(path string) (map[string]json.RawMessage, error) {
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) || err == nil && len(bytes.TrimSpace(content)) == 0 {
		return map[string]json.RawMessage{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(content, &settings); err != nil {
		return nil, fmt.Errorf("%s is not readable as JSON, and this will not overwrite it: %w", path, err)
	}
	if settings == nil {
		settings = map[string]json.RawMessage{}
	}
	return settings, nil
}

// hooksIn is the settings' `hooks`, which is an object of arrays, or a refusal:
// one of any other shape is the user's, and writing ours over it would lose it.
func hooksIn(settings map[string]json.RawMessage) (map[string][]json.RawMessage, error) {
	hooks := map[string][]json.RawMessage{}
	if raw, present := settings["hooks"]; present {
		if err := json.Unmarshal(raw, &hooks); err != nil {
			return nil, fmt.Errorf("`hooks` is not the shape Claude writes, and this will not overwrite it: %w", err)
		}
	}
	if hooks == nil {
		hooks = map[string][]json.RawMessage{}
	}
	return hooks, nil
}

// encode is json.Marshal, indented, without its HTML escaping: a settings file
// holds commands, and `&&` written back as `\u0026\u0026` is the same JSON
// and no longer the file its owner wrote. It cannot fail: everything handed
// to it is strings and raw values that have just been decoded.
func encode(value any, indent string) []byte {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", indent)
	_ = encoder.Encode(value)
	if indent == "" {
		return bytes.TrimSuffix(out.Bytes(), []byte("\n"))
	}
	return out.Bytes()
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
	if err := os.WriteFile(temporary, content, 0o600); err != nil {
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
func merge(settings map[string]json.RawMessage, program string) (map[string]json.RawMessage, []string, error) {
	hooks, err := hooksIn(settings)
	if err != nil {
		return nil, nil, err
	}

	var changes []string
	for _, event := range SubscribedHooks {
		wanted := command{Type: "command", Command: fmt.Sprintf("%q", program)}
		groups := hooks[event]

		if updated, what := place(groups, wanted, program); what != "" {
			hooks[event] = updated
			changes = append(changes, what+" "+event)
		}
	}

	if len(changes) == 0 {
		return settings, nil, nil
	}
	settings["hooks"] = encode(hooks, "")
	return settings, changes, nil
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
			parsed.Hooks[j] = encode(wanted, "")
			groups[i] = encode(parsed, "")
			return groups, "updated the path for"
		}
	}

	added := encode(group{Hooks: []json.RawMessage{encode(wanted, "")}}, "")
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
