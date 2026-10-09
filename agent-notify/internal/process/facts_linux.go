//go:build linux

package process

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// userHz is USER_HZ, the unit /proc counts start times in. It is part of the
// kernel's interface with userspace rather than a setting, and it is 100 on
// every architecture Go builds for.
const userHz = 100

// ProcessesOnThisMachine answers by asking this kernel, which is the only
// implementation that is not invented by a test.
type ProcessesOnThisMachine struct{}

// FactsAbout reads one process out of /proc/<pid>/stat.
//
// A stat file that cannot be read is not enough to call a process gone: a /proc
// that is not mounted fails every read. `kill(pid, 0)` is asked to settle it, as
// on macOS, and only ESRCH from that is treated as death. Anything else is
// "cannot tell" (R5).
func (ProcessesOnThisMachine) FactsAbout(pid int) (Facts, error) {
	if pid <= 0 {
		return Facts{}, ErrNoSuchProcess
	}

	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		if unix.Kill(pid, 0) == unix.ESRCH {
			return Facts{}, ErrNoSuchProcess
		}
		return Facts{}, fmt.Errorf("cannot read process %d: %w", pid, err)
	}

	// The name is the second field, in parentheses, and is whatever the process
	// called itself: spaces and parentheses included. So it ends at the last
	// ")", and the fields after it are numbered from 3 in proc(5).
	open, end := bytes.IndexByte(stat, '('), bytes.LastIndexByte(stat, ')')
	if open < 0 || end < open {
		return Facts{}, fmt.Errorf("cannot read process %d: %q is not a stat line", pid, stat)
	}
	fields := strings.Fields(string(stat[end+1:]))
	if len(fields) < 20 {
		return Facts{}, fmt.Errorf("cannot read process %d: %q is not a stat line", pid, stat)
	}

	if state := fields[0]; state == "Z" || state == "X" {
		// It has exited; what is left is an exit status nobody has collected.
		// Calling that alive would keep a session on the bar until whoever
		// forked it got round to reaping it. agent-notify never forks an
		// agent, so this is defence against somebody else's bookkeeping.
		return Facts{}, ErrNoSuchProcess
	}
	parent, err := strconv.Atoi(fields[1])
	if err != nil {
		return Facts{}, fmt.Errorf("cannot read process %d: parent %q: %w", pid, fields[1], err)
	}
	// Field 22, in clock ticks since boot. It is kept that way rather than
	// added to the boot time in /proc/stat, which is now minus the time since
	// boot and moves whenever the clock is set. A start time that moves ends
	// every session on the machine at once (D-89).
	ticks, err := strconv.ParseInt(fields[19], 10, 64)
	if err != nil {
		return Facts{}, fmt.Errorf("cannot read process %d: start time %q: %w", pid, fields[19], err)
	}

	facts := Facts{
		PID:       pid,
		StartedAt: time.Unix(0, 0).Add(time.Duration(ticks) * (time.Second / userHz)).UTC(),
		Command:   string(stat[open+1 : end]),
		Parent:    parent,
	}
	// Best effort, and deliberately not an error: another user's process
	// refuses this, and a missing path only costs a configured absolute path
	// its match. Unlike macOS this is the file the kernel ran, with symlinks
	// resolved, not the path that was asked for.
	facts.Path, _ = os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	return facts, nil
}

// BootIdentity names this boot, so that a record written before a reboot can be
// recognised without probing anything.
//
// /proc/sys/kernel/random/boot_id is a UUID the kernel mints once per boot, the
// same kind of thing as `kern.bootsessionuuid` on macOS.
func BootIdentity() (string, error) {
	identity, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", fmt.Errorf("cannot read this boot's identity: %w", err)
	}
	return strings.TrimSpace(string(identity)), nil
}
