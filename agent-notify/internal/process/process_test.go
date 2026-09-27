package process_test

import (
	"testing"
	"time"

	"github.com/lassoColombo/agent-notify/internal/process"
	"github.com/lassoColombo/agent-notify/session"
)

// invented is a machine that does not exist, which is the point: every rule in
// this package has to be decidable without anything running.
type invented struct {
	processes map[int]process.Facts
	// broken makes every question fail, which is how "cannot tell" is tested.
	broken bool
	// self is the pid this machine admits to being asked from.
	self int
}

func (m invented) FactsAbout(pid int) (process.Facts, error) {
	if m.broken {
		return process.Facts{}, errBroken
	}
	if facts, ok := m.processes[pid]; ok {
		return facts, nil
	}
	return process.Facts{}, process.ErrNoSuchProcess
}

var errBroken = &brokenMachine{}

type brokenMachine struct{}

func (*brokenMachine) Error() string { return "the machine cannot be read" }

var (
	boot    = "C89F9A5C-5A0C-4674-AF7E-BC8260851DC3"
	started = time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
)

// aRealLookingTree is the ladder a hook actually climbs, measured on this
// machine on 2026-09-17:
//
//	88251 the hook -> 88240 go -> 88236 zsh -> 38514 claude -> 99297 nu -> 3655 zellij
func aRealLookingTree(self int) invented {
	step := func(pid, parent int, command, path string) process.Facts {
		return process.Facts{
			PID: pid, Parent: parent, Command: command, Path: path,
			StartedAt: started,
		}
	}
	return invented{
		self: self,
		processes: map[int]process.Facts{
			88251: step(88251, 88240, "agent-notify", "/usr/local/bin/agent-notify"),
			88240: step(88240, 88236, "go", "/usr/local/go/bin/go"),
			88236: step(88236, 38514, "zsh", "/bin/zsh"),
			38514: step(38514, 99297, "claude", "/opt/homebrew/bin/claude"),
			99297: step(99297, 3655, "nu", "/opt/homebrew/bin/nu"),
			3655:  step(3655, 1, "zellij", "/opt/homebrew/bin/zellij"),
		},
	}
}

func TestTheAgentIsFoundHoweverManyShellsAreInTheWay(t *testing.T) {
	machine := aRealLookingTree(88251)

	found, chain, ok := process.FindAgent("claude", 88251, machine)
	if !ok {
		t.Fatal("did not find claude three rungs up")
	}
	if found.PID != 38514 {
		t.Errorf("found pid %d, want 38514", found.PID)
	}
	if len(chain) != 6 {
		t.Errorf("recorded %d ancestors, want the whole ladder: %+v", len(chain), chain)
	}
	if chain[0].Command != "agent-notify" || chain[len(chain)-1].Command != "zellij" {
		t.Errorf("the chain is not in order: %+v", chain)
	}
}

// TestABinaryIsRecognisedHoweverItIsWritten: all three spellings are things a
// person puts in a config file, and the absolute one is how you disambiguate
// two claudes.
func TestABinaryIsRecognisedHoweverItIsWritten(t *testing.T) {
	machine := aRealLookingTree(88251)
	for _, binary := range []string{"claude", "/opt/homebrew/bin/claude"} {
		found, _, ok := process.FindAgent(binary, 88251, machine)
		if !ok || found.PID != 38514 {
			t.Errorf("FindAgent(%q) found %d, %v", binary, found.PID, ok)
		}
	}
	if _, _, ok := process.FindAgent("", 88251, machine); ok {
		t.Error("an unconfigured binary matched something")
	}
}

func TestTheNearestMatchWins(t *testing.T) {
	// A claude session running inside a claude session. The one you are in is
	// the inner one.
	machine := aRealLookingTree(88251)
	outer := machine.processes[99297]
	outer.Command = "claude"
	machine.processes[99297] = outer

	found, _, ok := process.FindAgent("claude", 88251, machine)
	if !ok || found.PID != 38514 {
		t.Errorf("found pid %d, want the inner claude 38514", found.PID)
	}
}

func TestAnAgentThatIsNotAnAncestorIsNotInvented(t *testing.T) {
	if _, _, ok := process.FindAgent("codex", 88251, aRealLookingTree(88251)); ok {
		t.Error("found an agent that is not in the chain")
	}
}

func TestAnUnreadableMachineProvesNothing(t *testing.T) {
	_, chain, ok := process.FindAgent("claude", 88251, invented{broken: true})
	if ok {
		t.Error("found an agent on a machine that cannot be read")
	}
	if len(chain) != 0 {
		t.Errorf("recorded %d ancestors from a machine that answered nothing", len(chain))
	}
}

func TestTheClimbStopsRatherThanRunningAway(t *testing.T) {
	// A cycle, which a process tree should never contain and which must still
	// not hang a hook.
	cycle := invented{processes: map[int]process.Facts{
		10: {PID: 10, Parent: 11, Command: "a"},
		11: {PID: 11, Parent: 10, Command: "b"},
	}}
	if chain := process.Ancestry(10, cycle); len(chain) != 2 {
		t.Errorf("walked %d steps around a cycle, want 2 then stop", len(chain))
	}

	deep := invented{processes: map[int]process.Facts{}}
	for pid := 2; pid < 200; pid++ {
		deep.processes[pid] = process.Facts{PID: pid, Parent: pid + 1, Command: "sh"}
	}
	if chain := process.Ancestry(2, deep); len(chain) != process.MostAncestorsWorthClimbing {
		t.Errorf("walked %d steps up an endless chain, want the bound of %d",
			len(chain), process.MostAncestorsWorthClimbing)
	}
}

// TestTheDecisionTable is M5's "done when": every rule of §A8.2 and §A8.3,
// against a machine with nothing running.
func TestTheDecisionTable(t *testing.T) {
	alive := invented{self: 1, processes: map[int]process.Facts{
		1:   {PID: 1, StartedAt: started},
		500: {PID: 500, StartedAt: started},
	}}

	cases := []struct {
		name    string
		record  session.Record
		boot    string
		machine process.ReadsProcessFacts
		want    process.Verdict
	}{
		{
			"the process we recorded is the process running",
			recordFor(500, started, boot), boot, alive, process.Running,
		},
		{
			"the pid is not in use at all",
			recordFor(999, started, boot), boot, alive, process.Gone,
		},
		{
			"the pid is in use by something that started later — it was reused",
			recordFor(500, started.Add(-time.Hour), boot), boot, alive, process.Gone,
		},
		{
			"the pid started within a second of what we recorded — resolution, not reuse",
			recordFor(500, started.Add(400*time.Millisecond), boot), boot, alive, process.Running,
		},
		{
			"the record is from a previous boot: no probe, no doubt",
			recordFor(500, started, "an-older-boot"), boot, alive, process.Gone,
		},
		{
			"the machine cannot be read",
			recordFor(500, started, boot), boot, invented{broken: true}, process.CannotTell,
		},
		{
			"the record names no process, so it can never be proved dead",
			recordFor(0, time.Time{}, boot), boot, alive, process.CannotTell,
		},
		{
			"a record written before we recorded start times cannot be checked for reuse",
			recordFor(500, time.Time{}, boot), boot, alive, process.Running,
		},
		{
			"we do not know this boot's identity, so we fall back to probing",
			recordFor(500, started, boot), "", alive, process.Running,
		},
		{
			"the record carries no boot identity, so the reboot rule cannot apply",
			recordFor(500, started, ""), boot, alive, process.Running,
		},
	}

	for _, c := range cases {
		if got := process.LivenessOf(c.record, c.boot, c.machine); got != c.want {
			t.Errorf("%s:\n  got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestEndedFindsWhatNobodyAnnounced(t *testing.T) {
	machine := invented{self: 1, processes: map[int]process.Facts{
		1:   {PID: 1, StartedAt: started},
		500: {PID: 500, StartedAt: started},
	}}

	live := recordFor(500, started, boot)
	live.Key = session.Key{Host: "mac", Agent: "claude", SessionID: "live"}
	live.UpdatedAt = started.Add(time.Hour)

	killed := recordFor(999, started, boot)
	killed.Key = session.Key{Host: "mac", Agent: "claude", SessionID: "killed"}

	closed := recordFor(500, started, boot)
	closed.Key = session.Key{Host: "mac", Agent: "claude", SessionID: "cleared"}
	closed.UpdatedAt = started // older than live, same process: /clear happened

	already := recordFor(500, started, boot)
	already.Key = session.Key{Host: "mac", Agent: "claude", SessionID: "already"}
	already.Kernel = session.Ended

	ending := process.Ended([]session.Record{live, killed, closed, already}, boot, machine.self, machine)

	found := map[string]string{}
	for _, end := range ending {
		found[end.Key.SessionID] = end.Detail
	}
	if len(found) != 2 {
		t.Fatalf("ended %d sessions, want 2: %v", len(found), found)
	}
	if found["killed"] != process.ProcessGone {
		t.Errorf("the killed session ended as %q", found["killed"])
	}
	if found["cleared"] != process.Superseded {
		t.Errorf("the cleared session ended as %q", found["cleared"])
	}
	if _, ended := found["live"]; ended {
		t.Error("the newest session in a living process was ended")
	}
}

// TestASnapshotThatCannotSeeUsIsNotASnapshot is the rail that makes every other
// verdict safe: without it, one broken moment ends every session at once.
func TestASnapshotThatCannotSeeUsIsNotASnapshot(t *testing.T) {
	// A machine that answers about other pids but not about the process asking
	// is not describing this machine.
	blind := invented{self: 1, processes: map[int]process.Facts{
		500: {PID: 500, StartedAt: started},
	}}
	killed := recordFor(999, started, boot)
	killed.Key = session.Key{Host: "mac", Agent: "claude", SessionID: "killed"}

	if ending := process.Ended([]session.Record{killed}, boot, blind.self, blind); len(ending) != 0 {
		t.Errorf("ended %d sessions on a machine that cannot see this process", len(ending))
	}
}

func recordFor(pid int, startedAt time.Time, bootID string) session.Record {
	return session.Record{
		Key:    session.Key{Host: "mac", Agent: "claude", SessionID: "s"},
		Kernel: session.Working,
		Process: session.Process{
			PID: pid, StartedAt: startedAt, BootID: bootID,
		},
	}
}
