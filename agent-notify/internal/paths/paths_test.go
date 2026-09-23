package paths_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/lassoColombo/agent-notify/internal/paths"
)

// TestRootRelocatesEverything is M2's "done when": one variable moves durable
// state, volatile runtime state and the configuration file, so that running an
// isolated instance is one step.
func TestRootRelocatesEverything(t *testing.T) {
	root := t.TempDir()
	t.Setenv(paths.TheVariableThatNamesTheRoot, root)

	layout, err := paths.FromEnvironment()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if layout.Root != root {
		t.Errorf("Root = %q, want %q", layout.Root, root)
	}

	everything := append(layout.Directories(), layout.Sockets()...)
	everything = append(everything,
		layout.ConfigFile, layout.LogFile(), layout.WatcherLock(),
		layout.SessionFile("k"), layout.EndedFile("k"), layout.HistoryFile("k"),
		layout.LockFile("k"))
	for _, path := range everything {
		if !strings.HasPrefix(path, root+string(filepath.Separator)) {
			t.Errorf("%s escaped the root %s", path, root)
		}
	}
}

func TestRootIsMadeAbsolute(t *testing.T) {
	t.Setenv(paths.TheVariableThatNamesTheRoot, "relative/root")
	layout, err := paths.FromEnvironment()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !filepath.IsAbs(layout.State) {
		t.Errorf("State = %q, want an absolute path", layout.State)
	}
}

// TestDefaultsSeparateStateFromRuntime guards the reason there are two
// directories at all: one must survive a reboot and the other must not.
func TestDefaultsSeparateStateFromRuntime(t *testing.T) {
	t.Setenv(paths.TheVariableThatNamesTheRoot, "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	layout, err := paths.FromEnvironment()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if layout.Root != "" {
		t.Errorf("Root = %q, want empty when the override is unset", layout.Root)
	}
	if layout.State == layout.Runtime {
		t.Fatalf("state and runtime resolved to the same directory: %s", layout.State)
	}
	if !strings.HasPrefix(layout.State, home) {
		t.Errorf("State = %q, want it under the home directory %q", layout.State, home)
	}
	if !strings.HasSuffix(layout.ConfigFile, filepath.Join("agent-notify", "config.toml")) {
		t.Errorf("ConfigFile = %q, want it to end in agent-notify/config.toml", layout.ConfigFile)
	}

	wantState := filepath.Join(home, ".local", "state", "agent-notify")
	if runtime.GOOS == "darwin" {
		wantState = filepath.Join(home, "Library", "Application Support", "agent-notify")
	}
	if layout.State != wantState {
		t.Errorf("State = %q, want %q", layout.State, wantState)
	}
}

func TestXDGIsHonoured(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("XDG_STATE_HOME does not apply on darwin; XDG_CONFIG_HOME is covered below")
	}
	t.Setenv(paths.TheVariableThatNamesTheRoot, "")
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)

	layout, err := paths.FromEnvironment()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if layout.State != filepath.Join(state, "agent-notify") {
		t.Errorf("State = %q, want it under XDG_STATE_HOME", layout.State)
	}
}

func TestConfigHonoursXDGOnBothPlatforms(t *testing.T) {
	t.Setenv(paths.TheVariableThatNamesTheRoot, "")
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)

	layout, err := paths.FromEnvironment()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := filepath.Join(config, "agent-notify", "config.toml")
	if layout.ConfigFile != want {
		t.Errorf("ConfigFile = %q, want %q", layout.ConfigFile, want)
	}
}

// TestCreateRestrictsMode checks the mode rather than only the existence: a
// record carries what an agent said, and MkdirAll applies the umask.
func TestCreateRestrictsMode(t *testing.T) {
	t.Setenv(paths.TheVariableThatNamesTheRoot, t.TempDir())
	layout, err := paths.FromEnvironment()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if err := layout.Create(); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := layout.Create(); err != nil {
		t.Fatalf("Create is not idempotent: %v", err)
	}
	for _, dir := range layout.Directories() {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("Stat %s: %v", dir, err)
		}
		if got := info.Mode().Perm(); got != paths.DirMode {
			t.Errorf("%s has mode %o, want %o", dir, got, paths.DirMode)
		}
	}
}

// TestCheckSocketsNamesTheOffender is the whole point of the check: bind would
// have said "invalid argument" and named nothing.
func TestCheckSocketsNamesTheOffender(t *testing.T) {
	root := filepath.Join(t.TempDir(), strings.Repeat("d", 120))
	t.Setenv(paths.TheVariableThatNamesTheRoot, root)
	layout, err := paths.FromEnvironment()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	err = layout.CheckSockets()
	if err == nil {
		t.Fatalf("CheckSockets accepted a path of %d bytes", len(layout.SessionChangesSocket()))
	}
	for _, socket := range layout.Sockets() {
		if !strings.Contains(err.Error(), socket) {
			t.Errorf("the complaint does not name %s:\n%s", socket, err)
		}
	}
	if !strings.Contains(err.Error(), paths.TheVariableThatNamesTheRoot) {
		t.Errorf("the complaint does not say what to do about it:\n%s", err)
	}
}

func TestCheckSocketsAcceptsAShortRoot(t *testing.T) {
	t.Setenv(paths.TheVariableThatNamesTheRoot, shortTempDir(t))
	layout, err := paths.FromEnvironment()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if err := layout.CheckSockets(); err != nil {
		t.Errorf("CheckSockets rejected a short path: %v", err)
	}
}

// TestMaxSocketPathMatchesTheKernel is pinned to what the probe of 2026-09-17
// measured: 103 binds, 104 fails with EINVAL. A limit one byte too generous
// admits exactly the path this check exists to reject.
func TestMaxSocketPathMatchesTheKernel(t *testing.T) {
	want := 103
	if runtime.GOOS == "linux" {
		want = 107
	}
	if got := paths.MaxSocketPath(); got != want {
		t.Errorf("MaxSocketPath() = %d, want %d on %s", got, want, runtime.GOOS)
	}
}

// shortTempDir exists because t.TempDir() does not fit.
//
// On macOS it returns $TMPDIR/<TestName><digits>/001, and $TMPDIR is itself a
// 49-byte /var/folders path, so the directory alone can exceed the 103 bytes a
// unix socket may occupy — before any file name is appended. Every test from M8
// onwards that binds a real socket needs this, not t.TempDir().
//
// The real default runtime directory is unaffected: $TMPDIR/agent-notify is
// about 62 bytes with the longest socket name on the end.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "an")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}
