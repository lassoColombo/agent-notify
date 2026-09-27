package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/lassoColombo/agent-notify/session"
)

// TestReplayOfRealCaptures runs every codex hook payload this machine has
// recorded through the mapping and the reducer, and reports what came out.
//
// It is skipped where there are no captures, which is everywhere but the
// machine that made them. It is not a substitute for the fixtures in
// mapping_test.go — it is how those fixtures were chosen, and it is what
// settled the three questions codex asked that claude never did: whether a
// compaction says anything useful, whether a permission request clears itself,
// and what an interrupted turn leaves behind.
func TestReplayOfRealCaptures(t *testing.T) {
	steps, total := replayCorpus(t)
	if total == 0 {
		t.Skip("no captured codex events on this machine")
	}

	bySession := map[string][]replayed{}
	ignored := map[string]int{}
	for _, step := range steps {
		if !step.worth {
			ignored[step.hook]++
			continue
		}
		bySession[step.report.Key.SessionID] = append(bySession[step.report.Key.SessionID], step)
	}

	t.Logf("replayed %d captured payloads across %d sessions", total, len(bySession))
	t.Logf("ignored: %v", ignored)

	endings := map[session.Kernel]int{}
	var longest string
	var longestSteps []replayed
	for id, events := range bySession {
		sort.Slice(events, func(i, j int) bool { return events[i].when < events[j].when })
		kernel := session.Kernel("")
		for _, step := range events {
			kernel = session.Reduce(kernel, step.report.Event)
		}
		endings[kernel]++
		if len(events) > len(longestSteps) {
			longest, longestSteps = id, events
		}
	}
	t.Logf("final states across %d sessions: %v", len(bySession), endings)

	t.Logf("the busiest session (%s) had %d events; every transition:", longest[:8], len(longestSteps))
	kernel := session.Kernel("")
	for _, step := range longestSteps {
		next := session.Reduce(kernel, step.report.Event)
		if next != kernel {
			detail := step.report.Detail
			if detail != "" {
				detail = "/" + detail
			}
			t.Logf("   %s  %-18s -> %s%s", step.when[11:23], step.hook, next, detail)
		}
		kernel = next
	}
}

// TestNoRealSessionIsLeftClaimingToWork is the assertion the replay exists for,
// rather than a log of it.
//
// A session that stops being touched while its record says `working` is the
// failure this whole system is built to prevent: a bar that reads "2 agents
// working" is worth glancing at only if it is true. Every recorded codex
// session that reached its end — an interrupt, a permission prompt nobody
// answered, an exit — must leave a record that is honest about it.
func TestNoRealSessionIsLeftClaimingToWork(t *testing.T) {
	steps, total := replayCorpus(t)
	if total == 0 {
		t.Skip("no captured codex events on this machine")
	}

	bySession := map[string][]replayed{}
	for _, step := range steps {
		if step.worth {
			bySession[step.report.Key.SessionID] = append(bySession[step.report.Key.SessionID], step)
		}
	}

	for id, events := range bySession {
		sort.Slice(events, func(i, j int) bool { return events[i].when < events[j].when })
		kernel := session.Kernel("")
		for _, step := range events {
			kernel = session.Reduce(kernel, step.report.Event)
		}
		last := events[len(events)-1]
		if kernel != session.Working {
			continue
		}
		// Working is only honest if the last thing that happened was the agent
		// doing something. A session whose last recorded hook was an interrupt
		// or a permission prompt and which still reads `working` is a lie the
		// mapping told.
		switch last.hook {
		case "PostToolUse", "UserPromptSubmit":
		default:
			t.Errorf("session %s reads %q after its last hook was %s at %s — a session nobody "+
				"is touching must not claim to be working", id[:8], kernel, last.hook, last.when[11:23])
		}
	}
}

type replayed struct {
	when   string
	hook   string
	report session.Report
	worth  bool
}

// replayCorpus reads what the capture rig wrote: one tab-separated file per
// hook invocation, with the payload in the fifth field.
func replayCorpus(t *testing.T) ([]replayed, int) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, 0
	}
	directory := filepath.Join(home, ".codex", "hooks", "events.d")
	if override := os.Getenv("CODEX_HOME"); override != "" {
		directory = filepath.Join(override, "hooks", "events.d")
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, 0
	}

	var steps []replayed
	var total int
	for _, entry := range entries {
		content, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			continue
		}
		fields := strings.Split(string(content), "\t")
		if len(fields) < 5 {
			continue
		}
		var payload Payload
		if err := json.Unmarshal([]byte(fields[4]), &payload); err != nil {
			continue
		}
		total++
		report, worth := Translate(payload, "", Spending{})
		steps = append(steps, replayed{
			when: fields[0], hook: payload.HookEventName, report: report, worth: worth,
		})
	}
	return steps, total
}
