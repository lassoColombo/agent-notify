package process_test

import (
	"os"
	"os/exec"
	"testing"
	"time"

	agentnotify "github.com/lassoColombo/agent-notify"
	"github.com/lassoColombo/agent-notify/internal/process"
)

// The tests above prove the rules. These prove that the machine answers the way
// the rules assume — which is the half no invented prober can check.

func TestThisVeryProcessIsRunning(t *testing.T) {
	machine := process.ProcessesOnThisMachine{}
	facts, err := machine.FactsAbout(process.Self())
	if err != nil {
		t.Fatalf("cannot see this process: %v", err)
	}
	if facts.PID != process.Self() {
		t.Errorf("FactsAbout(%d) returned pid %d", process.Self(), facts.PID)
	}
	if facts.StartedAt.IsZero() || facts.StartedAt.After(time.Now()) {
		t.Errorf("StartedAt = %v, which is not a time this process started", facts.StartedAt)
	}
	if facts.Parent <= 0 {
		t.Errorf("Parent = %d", facts.Parent)
	}
	if facts.Command == "" {
		t.Error("Command is empty")
	}
	// The trailing-garbage bug: p_comm is a buffer the kernel does not clear,
	// so a name trimmed from the right keeps whatever was there before.
	for _, b := range []byte(facts.Command) {
		if b == 0 || b < 0x20 {
			t.Errorf("Command = %q, which has not been cut at the terminator", facts.Command)
			break
		}
	}
}

func TestAKilledProcessIsJudgedDead(t *testing.T) {
	machine := process.ProcessesOnThisMachine{}

	victim := exec.Command("/bin/sleep", "30")
	if err := victim.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	facts, err := machine.FactsAbout(victim.Process.Pid)
	if err != nil {
		t.Fatalf("cannot see the process we just started: %v", err)
	}

	record := agentnotify.Record{
		Kernel:  agentnotify.Working,
		Process: agentnotify.Process{PID: facts.PID, StartedAt: facts.StartedAt},
	}
	if got := process.LivenessOf(record, "", machine); got != process.Running {
		t.Fatalf("a running process was judged %v", got)
	}

	if err := victim.Process.Kill(); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	// Reaping matters: an unreaped child is a zombie, and a zombie still has a
	// kinfo_proc. Look treats SZOMB as gone, but a test that never waited would
	// be proving that rather than what it claims to prove.
	_ = victim.Wait()

	if got := process.LivenessOf(record, "", machine); got != process.Gone {
		t.Errorf("a killed process was judged %v, want gone", got)
	}
}

// TestAReusedPidIsNotJudgedAlive is the reason a start time is stored beside
// the number. This process is certainly running; it is not the process the
// record describes.
func TestAReusedPidIsNotJudgedAlive(t *testing.T) {
	record := agentnotify.Record{
		Kernel: agentnotify.Working,
		Process: agentnotify.Process{
			PID:       process.Self(),
			StartedAt: time.Unix(1, 0).UTC(),
		},
	}
	if got := process.LivenessOf(record, "", process.ProcessesOnThisMachine{}); got != process.Gone {
		t.Errorf("a stranger wearing our pid was judged %v, want gone", got)
	}
}

func TestBootIdentityIsAStableUUID(t *testing.T) {
	first, err := process.BootIdentity()
	if err != nil {
		t.Fatalf("BootIdentity: %v", err)
	}
	if len(first) != 36 {
		t.Errorf("BootIdentity() = %q, want a UUID", first)
	}
	second, err := process.BootIdentity()
	if err != nil {
		t.Fatalf("BootIdentity: %v", err)
	}
	if first != second {
		t.Errorf("the boot identity changed between two reads: %q then %q", first, second)
	}
}

// TestTheRealAncestryIsWalkable climbs from this test process and checks the
// shape of what comes back, without asserting anything about what happens to be
// running it.
func TestTheRealAncestryIsWalkable(t *testing.T) {
	chain := process.Ancestry(process.Self(), process.ProcessesOnThisMachine{})
	if len(chain) == 0 {
		t.Fatal("walked nothing at all from a process that certainly has a parent")
	}
	if chain[0].PID != process.Self() {
		t.Errorf("the chain starts at %d, not at this process", chain[0].PID)
	}
	for i, ancestor := range chain {
		if ancestor.PID <= 0 || ancestor.Command == "" {
			t.Errorf("step %d is not a process: %+v", i, ancestor)
		}
		if ancestor.StartedAt.IsZero() {
			t.Errorf("step %d has no start time, so its pid means nothing: %+v", i, ancestor)
		}
	}
	t.Logf("walked %d ancestors from this test: %s", len(chain), describe(chain))

	found, _, ok := process.FindAgent("go", process.Self(), process.ProcessesOnThisMachine{})
	if ok {
		t.Logf("`go` is in the chain at pid %d, which is what a hook's climb looks like", found.PID)
	}
}

func TestAPidNothingIsUsingIsNotThere(t *testing.T) {
	// A pid above the kernel's maximum cannot be in use, so this is not a race
	// against something starting.
	if _, err := (process.ProcessesOnThisMachine{}).FactsAbout(1 << 30); err != process.ErrNoSuchProcess {
		t.Errorf("FactsAbout(2^30) = %v, want ErrNoSuchProcess", err)
	}
	if os.Getpid() == 1 {
		t.Skip("running as pid 1")
	}
}

func describe(chain []agentnotify.Ancestor) string {
	var text string
	for i, ancestor := range chain {
		if i > 0 {
			text += " <- "
		}
		text += ancestor.Command
	}
	return text
}
