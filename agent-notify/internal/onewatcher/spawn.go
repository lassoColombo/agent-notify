package onewatcher

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/lassoColombo/agent-notify/internal/paths"
	"github.com/lassoColombo/agent-notify/logs"
)

// Spawn starts a detached session-watcher and returns immediately.
//
// The caller is usually a hook, so the child inherits four things from the
// agent and all four are wrong for a process meant to outlive it (§A9.2). Each
// is cut here, and the second one is the one that will actually happen:
//
//   - **The terminal session.** Closing that window sends a hangup to
//     everything in it, killing the session-watcher along with the terminal
//     that happened to start it. Cut by setsid.
//   - **The hook's open pipes.** Claude hands the hook a pipe and waits for it
//     to close as the signal that the hook has finished. If the session-watcher
//     inherits that pipe, it never closes, and Claude waits forever. What a
//     user sees is "the agent hangs after every tool call", with nothing in any
//     log, because nothing failed — something merely never ended. Cut by
//     giving it streams of our choosing: the null device for stdin and stdout,
//     and the log file for stderr, so that a runtime fatal error in a detached
//     daemon is not written to a pipe nobody holds.
//   - **The working directory.** It would pin the agent's directory forever: a
//     disk that cannot be unmounted, a deleted directory that never goes away.
//     Cut by chdir to /.
//   - **The agent's environment**, which holds API keys, and which the
//     session-watcher would then keep in memory for days. Cut by passing an
//     explicit short list, which keeps everything path resolution reads, so
//     the child resolves the same layout this process did.
func Spawn(layout paths.Layout, configured string) error {
	program, err := CoreBinary(configured)
	if err != nil {
		return err
	}

	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("cannot open %s: %w", os.DevNull, err)
	}
	defer null.Close()

	command := exec.Command(program, "watcher", "run")
	command.Stdin, command.Stdout, command.Stderr = null, null, null
	// Only what the runtime writes over the program's head lands here; the
	// session-watcher logs everything it means to say through logs.OpenFile,
	// into this same file.
	if file, err := logs.File(layout.LogFile()); err == nil {
		defer file.Close()
		command.Stderr = file
	}
	command.Dir = "/"
	command.Env = keptEnvironment()
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := command.Start(); err != nil {
		return fmt.Errorf("cannot start a session-watcher: %w", err)
	}
	// Let go of it entirely. It is not a child we supervise — it outlives us on
	// purpose, and the lock is what keeps it single.
	return command.Process.Release()
}

// CoreBinary finds the agent-notify binary.
//
// It cannot simply be this executable, and that is the whole problem: the hook
// library is linked into binaries we did not write. Inside
// agent-notify-claude, os.Executable() is agent-notify-claude, and
// `agent-notify-claude watcher run` is not a thing.
//
// So, in order: what the configuration named, this executable if it is
// agent-notify, then PATH. PATH is last and not trusted much — a hook's PATH is
// not your shell's PATH, which is exactly why `agent-notify-binary` exists in
// the configuration. An integration that runs `agent-notify focus-session` on
// a click has the same problem: a launchd job's PATH is not your shell's
// either.
//
// Failing to find it is not fatal to anything. The record is already written,
// `list` still reads it and still applies liveness as it reads; what is lost is
// the daemon, and with it only the speed of noticing a death.
func CoreBinary(configured string) (string, error) {
	if configured != "" {
		if found, err := exec.LookPath(configured); err == nil {
			return found, nil
		}
		return "", fmt.Errorf("agent-notify-binary names %q, which is not runnable", configured)
	}
	if self, err := os.Executable(); err == nil && filepath.Base(self) == "agent-notify" {
		return self, nil
	}
	if found, err := exec.LookPath("agent-notify"); err == nil {
		return found, nil
	}
	return "", fmt.Errorf(
		"cannot find the agent-notify binary to start a session-watcher: it is not this " +
			"program and not on PATH. Name it as `agent-notify-binary` in the configuration.")
}

// keptEnvironment is the explicit short list. Everything not named here is
// left behind, which is the point: an agent's environment holds API keys and a
// session-watcher would otherwise keep them in memory for days.
//
// Two kinds of thing are on it. Everything that decides where things are:
// TMPDIR and the XDG variables are part of what paths.FromEnvironment answers,
// and a child that resolved a different directory would watch an empty one.
// And who you are: a per-user service is found by name on macOS (D-43).
// Nothing here can hold a secret.
func keptEnvironment() []string {
	var kept []string
	for _, name := range []string{
		"HOME", "PATH", "TMPDIR", "USER", "LOGNAME",
		"XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_RUNTIME_DIR", "XDG_DATA_HOME",
		paths.TheVariableThatNamesTheRoot, logs.TheVariableThatSetsTheLevel,
	} {
		if value, present := os.LookupEnv(name); present {
			kept = append(kept, name+"="+value)
		}
	}
	return kept
}
