package main

import (
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

// TestTheTableNamesNoBinary is the whole of what this install has to get right.
//
// `binary` means "core may run this": the hook runs it on the path an agent is
// waiting on, and the session-watcher runs it to render or to focus. A picker
// needs a terminal, so every one of those is a process with nowhere to draw —
// and it used to happen, five times, before the supervisor gave up on it.
func TestTheTableNamesNoBinary(t *testing.T) {
	t.Setenv("AGENT_NOTIFY_ROOT", t.TempDir())

	var table, complaints strings.Builder
	if code := printTable(&table, &complaints, nil); code != 0 {
		t.Fatalf("install exited %d: %s", code, complaints.String())
	}

	var parsed struct {
		Integration map[string]struct {
			Binary  string `toml:"binary"`
			Enabled *bool  `toml:"enabled"`
		} `toml:"integration"`
	}
	if err := toml.Unmarshal([]byte(table.String()), &parsed); err != nil {
		t.Fatalf("what install printed is not TOML: %v\n%s", err, table.String())
	}

	mine, present := parsed.Integration[Name]
	if !present {
		t.Fatalf("no [integration.%s] table in:\n%s", Name, table.String())
	}
	if mine.Binary != "" {
		t.Errorf("binary = %q, so core would try to run a picker", mine.Binary)
	}
	// And not by saying it is off, which is what used to stand in for this: the
	// table is on, and there is simply nothing in it for core to run.
	if mine.Enabled != nil {
		t.Errorf("enabled = %v; an absent binary is what says core must not run it",
			*mine.Enabled)
	}
}

// TestTheKeybindingCarriesTheResolvedPath: it is the only thing that runs this,
// so it is the one place the path has to be right — and absolute, because a
// keybinding's PATH is not your shell's.
func TestTheKeybindingCarriesTheResolvedPath(t *testing.T) {
	t.Setenv("AGENT_NOTIFY_ROOT", t.TempDir())

	var table, complaints strings.Builder
	if code := printTable(&table, &complaints, nil); code != 0 {
		t.Fatalf("install exited %d: %s", code, complaints.String())
	}

	said := complaints.String()
	if !strings.Contains(said, `Run "/`) {
		t.Errorf("the keybinding does not name an absolute path:\n%s", said)
	}
	if strings.Contains(table.String(), `Run "/`) {
		t.Errorf("the keybinding is KDL and landed on stdout, where a TOML file "+
			"is waiting for it:\n%s", table.String())
	}
}
