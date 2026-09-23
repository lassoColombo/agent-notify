//go:build darwin

package main

import (
	"os"
	"testing"
)

// TestTheWalkStartsHereAndClimbs asks the real kernel, because the whole point
// of this file is that it agrees with the kernel.
func TestTheWalkStartsHereAndClimbs(t *testing.T) {
	chain := Ancestry(Self())
	if len(chain) < 2 {
		t.Fatalf("chain = %+v, want at least this process and whatever started it", chain)
	}
	if chain[0].PID != os.Getpid() {
		t.Errorf("chain[0] = %+v, want this process: nearest first", chain[0])
	}
	if chain[1].PID != os.Getppid() {
		t.Errorf("chain[1] = %+v, want this process's parent", chain[1])
	}
	for _, rung := range chain {
		if rung.PID <= 1 {
			t.Errorf("chain = %+v, want it to stop before pid 1", chain)
		}
		if rung.Command == "" {
			t.Errorf("rung %+v has no name, and a chain of bare numbers is not readable", rung)
		}
	}
	if len(chain) > mostAncestorsWorthClimbing {
		t.Errorf("chain is %d long, want at most %d", len(chain), mostAncestorsWorthClimbing)
	}
}

// TestAPidNobodyIsUsingIsAnEmptyChain rather than a hang or a panic. Capture
// runs on the path an agent waits on.
func TestAPidNobodyIsUsingIsAnEmptyChain(t *testing.T) {
	if chain := Ancestry(0); len(chain) != 0 {
		t.Errorf("chain = %+v, want nothing", chain)
	}
	// 99999 is above the default pid ceiling on macOS and reliably nobody.
	if chain := Ancestry(99999); len(chain) != 0 {
		t.Errorf("chain = %+v, want nothing", chain)
	}
}

// TestCommandNameStopsAtTheFirstNul is a measured property of the kernel, not a
// style choice: p_comm is a fixed 17-byte buffer that is not cleared between
// uses, so trimming from the right leaves the previous tenant attached and
// every name comes out looking corrupted.
func TestCommandNameStopsAtTheFirstNul(t *testing.T) {
	buffer := []byte("claude\x00\x00\x00sk\x00\x00\x00\x00\x00\x00\x00\x00")
	if got := commandName(buffer); got != "claude" {
		t.Errorf("commandName = %q, want %q", got, "claude")
	}
}
