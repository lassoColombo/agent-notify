//go:build darwin

package main

import (
	"bytes"
	"os"

	"golang.org/x/sys/unix"
)

// mostAncestorsWorthClimbing bounds the walk.
//
// The distance from here to the terminal is not fixed — this program, the hook
// that ran it, the agent, a shell, a login, and whatever wrapper somebody put
// in between — so the walk climbs to the top rather than counting steps. The
// bound exists because "a process tree cannot contain a cycle" is not the same
// sentence as "a process tree does not contain a cycle", and this runs on the
// path an agent is waiting on.
const mostAncestorsWorthClimbing = 12

// Ancestry walks up from a pid, recording what it passes.
//
// It MUST run inside the agent's process tree, which is why it is part of
// `capture-environment` and of nothing else: by the time the session-watcher
// reads the event, this program and the hook that ran it have both exited and
// there is no chain left to walk from.
//
// The first two rungs are this program and the hook, and they are kept rather
// than skipped. They own no windows, so they cost nothing and they are the
// difference between a chain a person can recognise and a list of numbers.
func Ancestry(start int) []Ancestor {
	var chain []Ancestor
	seen := map[int]bool{}

	for pid := start; pid > 1 && len(chain) < mostAncestorsWorthClimbing; {
		if seen[pid] {
			break
		}
		seen[pid] = true

		info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
		if err != nil {
			// A chain that stops early is worth more than none: the rungs
			// already on it are the ones nearest the agent, and the one this
			// is looking for — the terminal — is usually reachable anyway.
			break
		}
		chain = append(chain, Ancestor{PID: pid, Command: commandName(info.Proc.P_comm[:])})
		pid = int(info.Eproc.Ppid)
	}
	return chain
}

// Self is the pid to start from.
func Self() int { return os.Getpid() }

// commandName truncates at the first NUL rather than trimming trailing ones.
//
// p_comm is a fixed 17-byte buffer the kernel does not clear between uses, so
// what follows the terminator is whatever was there before: "claude\x00\x00sk".
// Trimming from the right leaves that attached, and every name printed in a
// diagnostic comes out looking corrupted.
func commandName(buffer []byte) string {
	if end := bytes.IndexByte(buffer, 0); end >= 0 {
		buffer = buffer[:end]
	}
	return string(buffer)
}
