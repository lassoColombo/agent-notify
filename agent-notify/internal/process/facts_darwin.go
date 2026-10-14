//go:build darwin

package process

import (
	"bytes"
	"fmt"
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
	return Facts{
		PID:       pid,
		StartedAt: time.Unix(started.Sec, int64(started.Usec)*1000).UTC(),
		Command:   commandName(info.Proc.P_comm[:]),
		Parent:    int(info.Eproc.Ppid),
	}, nil
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
