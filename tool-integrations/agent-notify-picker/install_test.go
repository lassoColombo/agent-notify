package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

// written runs install into a fresh root and returns the drop-in it filed
// and what it said.
func written(t *testing.T) (table string, complaints string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("AGENT_NOTIFY_ROOT", root)
	var out, problems strings.Builder
	if code := printTable(&out, &problems, nil); code != 0 {
		t.Fatalf("install exited %d: %s", code, problems.String())
	}
	filed, err := os.ReadFile(filepath.Join(root, "conf.d", Name+".toml"))
	if err != nil {
		t.Fatalf("install filed nothing: %v", err)
	}
	return string(filed), problems.String()
}

// TestTheTableNamesNoBinary is the whole of what this install has to get right.
//
// `binary` means "core may run this": the hook runs it on the path an agent is
// waiting on, and the session-watcher runs it to render or to focus. A picker
// needs a terminal, so every one of those is a process with nowhere to draw —
// and it used to happen, five times, before the supervisor gave up on it.
func TestTheTableNamesNoBinary(t *testing.T) {
	table, _ := written(t)

	var parsed struct {
		Integration map[string]struct {
			Binary  string `toml:"binary"`
			Enabled *bool  `toml:"enabled"`
		} `toml:"integration"`
	}
	if err := toml.Unmarshal([]byte(table), &parsed); err != nil {
		t.Fatalf("what install printed is not TOML: %v\n%s", err, table)
	}

	mine, present := parsed.Integration[Name]
	if !present {
		t.Fatalf("no [integration.%s] table in:\n%s", Name, table)
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
	table, said := written(t)
	if !strings.Contains(said, `Run "/`) {
		t.Errorf("the keybinding does not name an absolute path:\n%s", said)
	}
	if strings.Contains(table, `Run "/`) {
		t.Errorf("the keybinding is KDL and landed in the TOML drop-in:\n%s", table)
	}
}
