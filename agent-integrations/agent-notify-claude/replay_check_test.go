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

// TestReplayOfRealCaptures runs every hook payload this machine has recorded
// through the mapping and the reducer, and reports what came out.
//
// It is skipped where there are no captures, which is everywhere but the
// machine that made them. It is not a substitute for the fixtures below — it is
// how the fixtures were chosen.
func TestReplayOfRealCaptures(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	dir := filepath.Join(home, ".claude", "hooks", "events.d")
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		t.Skip("no captured events on this machine")
	}

	type step struct {
		when   string
		hook   string
		report session.Report
		worth  bool
	}
	bySession := map[string][]step{}
	ignored := map[string]int{}
	var total int

	for _, entry := range entries {
		content, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		parts := strings.Split(string(content), "\t")
		if len(parts) < 5 {
			continue
		}
		var payload Payload
		if err := json.Unmarshal([]byte(parts[4]), &payload); err != nil {
			continue
		}
		var named struct {
			Hook string `json:"hook_event_name"`
		}
		_ = json.Unmarshal([]byte(parts[4]), &named)

		total++
		report, worth := Translate(named.Hook, payload, ClaudeSession{}, ClaudeTranscript{}, "")
		if !worth {
			ignored[named.Hook+"/"+payload.NotificationType]++
			continue
		}
		bySession[payload.SessionID] = append(bySession[payload.SessionID],
			step{when: parts[0], hook: named.Hook, report: report, worth: worth})
	}

	t.Logf("replayed %d captured payloads across %d sessions", total, len(bySession))
	t.Logf("ignored: %v", ignored)

	// Reduce each session's events in order and count where they ended up.
	endings := map[session.Kernel]int{}
	var longest string
	var longestSteps []step
	for sid, steps := range bySession {
		sort.Slice(steps, func(i, j int) bool { return steps[i].when < steps[j].when })
		kernel := session.Kernel("")
		for _, s := range steps {
			kernel = session.Reduce(kernel, s.report.Event)
		}
		endings[kernel]++
		if len(steps) > len(longestSteps) {
			longest, longestSteps = sid, steps
		}
	}
	t.Logf("final states across %d sessions: %v", len(bySession), endings)

	// Show the shape of one busy session, which is what a person would watch.
	t.Logf("the busiest session (%s) had %d events; its first 25 transitions:", longest[:8], len(longestSteps))
	kernel := session.Kernel("")
	shown := 0
	for _, s := range longestSteps {
		next := session.Reduce(kernel, s.report.Event)
		if next != kernel && shown < 25 {
			detail := s.report.Detail
			if detail != "" {
				detail = "/" + detail
			}
			t.Logf("   %s  %-16s -> %s%s", s.when[11:23], s.hook, next, detail)
			shown++
		}
		kernel = next
	}
}
