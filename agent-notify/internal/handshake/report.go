package handshake

import (
	"encoding/json"
	"os"
	"slices"
	"time"

	"github.com/lassoColombo/agent-notify/internal/config"
	"github.com/lassoColombo/agent-notify/internal/paths"
	"github.com/lassoColombo/agent-notify/session"
)

// Integration is one line of the report.
type Integration struct {
	Name    string               `json:"name"`
	Answers session.Capabilities `json:"answers"`
	// Problem is why it could not be asked. Kept because a program that
	// answers no methods and one that could not be asked look identical and
	// mean opposite things.
	Problem string `json:"problem,omitempty"`
	State   string `json:"state"`
}

// Report is what the session-watcher knows about the integrations, written as
// a file so that `doctor` and a focus off a keybinding can read it without
// asking a process anything.
type Report struct {
	PID          int           `json:"pid"`
	Written      string        `json:"written"`
	Integrations []Integration `json:"integrations"`
}

// Write files the report: every enabled integration, what it answered and why
// it did not.
func Write(layout paths.Layout, settings config.Config,
	answers map[string]session.Capabilities, unanswered map[string]string) {
	report := Report{PID: os.Getpid(), Written: time.Now().UTC().Format(time.RFC3339)}

	for _, name := range settings.Enabled() {
		line := Integration{Name: name, Answers: answers[name], Problem: unanswered[name]}
		switch {
		case line.Problem != "":
			line.State = "could not be asked"
		case !slices.ContainsFunc(line.Answers.Methods, session.CoreRuns):
			// An agent-integration, run by its agent's hooks; a picker, run
			// by a keybinding. Core asked what it answers and will never
			// call any of it.
			line.State = "yours to start; core never runs it"
		case slices.Contains(line.Answers.Methods, session.MethodRender):
			line.State = "drawn when something it watches moves"
		default:
			line.State = "run when core needs it"
		}
		report.Integrations = append(report.Integrations, line)
	}

	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return
	}
	temporary := layout.IntegrationsFile() + ".tmp"
	if err := os.WriteFile(temporary, encoded, paths.FileMode); err != nil {
		return
	}
	_ = os.Rename(temporary, layout.IntegrationsFile())
}

// Read is the report back, or false when no session-watcher has written one.
func Read(layout paths.Layout) (Report, bool) {
	content, err := os.ReadFile(layout.IntegrationsFile())
	if err != nil {
		return Report{}, false
	}
	var report Report
	if err := json.Unmarshal(content, &report); err != nil {
		return Report{}, false
	}
	return report, true
}
