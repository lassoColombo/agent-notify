package agentnotify_test

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	an "github.com/lassoColombo/agent-notify"
)

// full is a record with something in every field, so that a mutation has to
// produce a genuinely different value rather than merely fill a hole.
func full() an.Record {
	return an.Record{
		Key:        an.Key{Host: "mac", Agent: "claude", SessionID: "alpha"},
		Sequence:   7,
		Kernel:     an.Working,
		Rank:       an.RankWorking,
		Detail:     "running-bash",
		StateSince: time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC),
		CreatedAt:  time.Date(2026, 9, 21, 8, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2026, 9, 21, 9, 30, 0, 0, time.UTC),
		EndedAt:    time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC),
		Name:       "agent-notify",
		Cwd:        "/Users/somebody/projects/agent-notify",
		Branch:     "main",
		Model:      "claude-opus-5",
		Message:    "Shall I delete the branch?",
		Usage:      an.Usage{Input: 10, Output: 20},
		Annotations: map[string]json.RawMessage{
			"pinned": json.RawMessage(`true`)},
		DerivedContext: map[string]json.RawMessage{
			"tab": json.RawMessage(`2`)},
		Process: an.Process{
			PID: 4242, BootID: "boot",
			StartedAt: time.Date(2026, 9, 21, 7, 0, 0, 0, time.UTC)},
		CapturedContext: an.CapturedContext{
			CapturedAt: time.Date(2026, 9, 21, 8, 30, 0, 0, time.UTC),
			Ancestry:   []an.Ancestor{{PID: 4241, Command: "zellij"}},
			By:         map[string]json.RawMessage{"zellij-display": json.RawMessage(`{"pane":7}`)},
		},
	}
}

// TestEachFieldMovedMovesExactlyTheFieldItNames is the contract the whole
// property test rests on.
//
// If a mutation moved nothing, a display would read that as "my renderer does
// not use this field" and pass. If it moved something else as well, it would
// read as a renderer using a field it does not. Either way the test built on
// top of it would be quietly weaker than it looks, which is worse than not
// having it.
func TestEachFieldMovedMovesExactlyTheFieldItNames(t *testing.T) {
	base := full()
	for name, moved := range an.EachFieldMoved(base) {
		if !an.Differs(base, moved, []string{name}) {
			t.Errorf("EachFieldMoved[%q] did not move %q", name, name)
		}
		for _, other := range recordFieldNames(t) {
			if other == name {
				continue
			}
			if an.Differs(base, moved, []string{other}) {
				t.Errorf("EachFieldMoved[%q] also moved %q", name, other)
			}
		}
	}
}

// TestEveryFieldWorthWakingForIsOffered, so that a field added to the record
// cannot slip past the sweep and go untested in every display at once.
func TestEveryFieldWorthWakingForIsOffered(t *testing.T) {
	offered := an.EachFieldMoved(full())
	// The three that cannot move, and therefore cannot wake anybody.
	settled := []string{"key", "schema", "created_at"}

	for _, name := range recordFieldNames(t) {
		_, there := offered[name]
		switch {
		case slices.Contains(settled, name) && there:
			t.Errorf("%q is settled once a session exists and is offered as a way to wake", name)
		case !slices.Contains(settled, name) && !there:
			t.Errorf("%q is a field a display could need and the sweep skips it", name)
		}
	}
}

// TestAMovedRecordIsStillARecord: a mutation that produced something no agent
// could write would test displays against a world that does not happen.
func TestAMovedRecordIsStillARecord(t *testing.T) {
	for name, moved := range an.EachFieldMoved(full()) {
		if name == "kernel" && !moved.Kernel.Known() {
			t.Errorf("moving the kernel produced %q, which no display has an opinion about",
				moved.Kernel)
		}
		encoded, err := json.Marshal(moved)
		if err != nil {
			t.Errorf("the record with %q moved will not encode: %v", name, err)
			continue
		}
		var back an.Record
		if err := json.Unmarshal(encoded, &back); err != nil {
			t.Errorf("the record with %q moved will not decode: %v", name, err)
		}
	}
}

// TestTheBaseIsNotTouched, because a sweep that mutated its input would make
// every comparison after the first one a comparison with itself.
func TestTheBaseIsNotTouched(t *testing.T) {
	base := full()
	before, _ := json.Marshal(base)
	an.EachFieldMoved(base)
	after, _ := json.Marshal(base)
	if string(before) != string(after) {
		t.Errorf("EachFieldMoved changed what it was given:\n%s\n%s", before, after)
	}
}

// recordFieldNames is every json name a record has, read off the struct so
// that this file cannot fall behind it.
func recordFieldNames(t *testing.T) []string {
	t.Helper()
	encoded, err := json.Marshal(full())
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
