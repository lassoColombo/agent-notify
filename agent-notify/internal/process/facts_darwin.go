//go:build darwin

package process

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// stateZombie is SZOMB from <sys/proc.h>. x/sys/unix decodes the struct but
// does not export the process states, and five is not a number to leave bare.
const stateZombie = 5

// ProcessesOnThisMachine answers by asking this kernel, which is the only
// implementation that is not invented by a test.
type ProcessesOnThisMachine struct{}

// Look reads one process out of the kernel.
//
// [verified 2026-09-17, macOS 26] `sysctl kern.proc.pid` on a pid nothing is
// using fails with EIO, not ESRCH — which is not what EIO usually means and is
// not enough on its own to call a process gone. `kill(pid, 0)` is asked to
// settle it, and only ESRCH from that is treated as death. Anything else is
// "cannot tell" (R5).
func (ProcessesOnThisMachine) FactsAbout(pid int) (Facts, error) {
	if pid <= 0 {
		return Facts{}, ErrNoSuchProcess
	}

	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		if unix.Kill(pid, 0) == unix.ESRCH {
			return Facts{}, ErrNoSuchProcess
		}
		return Facts{}, fmt.Errorf("cannot read process %d: %w", pid, err)
	}

	if info.Proc.P_stat == stateZombie {
		// It has exited; what is left is an exit status nobody has collected.
		// Calling that alive would keep a session on the bar until whoever
		// forked it got round to reaping it. agent-notify never forks an
		// agent, so this is defence against somebody else's bookkeeping.
		return Facts{}, ErrNoSuchProcess
	}

	started := info.Proc.P_starttime
	facts := Facts{
		PID:       pid,
		StartedAt: time.Unix(started.Sec, int64(started.Usec)*1000).UTC(),
		Command:   commandName(info.Proc.P_comm[:]),
		Parent:    int(info.Eproc.Ppid),
	}
	// Best effort, and deliberately not an error: another user's process
	// refuses this, and a missing path only costs a configured absolute path
	// its match.
	facts.Path, _ = executablePath(pid)
	return facts, nil
}

// commandName truncates at the first NUL rather than trimming trailing ones.
//
// [verified 2026-09-17] p_comm is a fixed 17-byte buffer that the kernel does
// not clear between uses, so what follows the terminator is whatever was there
// before: "claude\x00\x00\x00sk", "go\x00f\x00e\x00\x00\x00sk". Trimming from
// the right leaves the garbage attached and every name comparison fails in a
// way that looks like the agent simply is not there.
func commandName(buffer []byte) string {
	if end := bytes.IndexByte(buffer, 0); end >= 0 {
		buffer = buffer[:end]
	}
	return string(buffer)
}

// executablePath reads KERN_PROCARGS2, whose layout begins with a 32-bit argc
// and then the executable path, NUL-terminated, before the arguments.
//
// Only the path is taken. The arguments follow it and are deliberately left
// alone: a command line holds whatever a user typed, and §A15 is about not
// storing what nobody asked for.
func executablePath(pid int) (string, error) {
	raw, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return "", err
	}
	if len(raw) < 4 {
		return "", fmt.Errorf("kern.procargs2 for %d returned %d bytes", pid, len(raw))
	}
	rest := raw[4:]
	_ = binary.LittleEndian.Uint32(raw[:4]) // argc, which we do not need
	if end := bytes.IndexByte(rest, 0); end >= 0 {
		rest = rest[:end]
	}
	return string(rest), nil
}

// BootIdentity names this boot, so that a record written before a reboot can be
// recognised without probing anything.
//
// [verified 2026-09-17, macOS 26] `kern.bootsessionuuid` is a real UUID the
// kernel mints per boot. The plan originally named `kern.boottime`, which is
// derived as (now - uptime) and is therefore adjusted by sleep and by NTP — a
// boot identity that drifts would declare every session on the machine dead
// after a nap. The UUID cannot drift.
func BootIdentity() (string, error) {
	identity, err := unix.Sysctl("kern.bootsessionuuid")
	if err != nil {
		return "", fmt.Errorf("cannot read this boot's identity: %w", err)
	}
	return identity, nil
}

// Self is the pid of this process, which is the one thing a working reader must
// always be able to see (§A8.2).
func Self() int { return os.Getpid() }
