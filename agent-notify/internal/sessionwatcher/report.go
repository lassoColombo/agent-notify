package sessionwatcher

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
	Name    string                   `json:"name"`
	Answers WhatAnIntegrationAnswers `json:"answers"`
	// Binary is what core runs; a table without one is a client's settings.
	Binary string `json:"binary,omitempty"`
	State  string `json:"state"`
}

// Report is what the session-watcher knows about the integrations, written as
// a file so that `doctor` and a focus off a keybinding can read it without
// asking a process anything.
type Report struct {
	PID          int           `json:"pid"`
	Written      string        `json:"written"`
	Integrations []Integration `json:"integrations"`
}

func (w *Watcher) writeReport(settings config.Config) {
	report := Report{PID: os.Getpid(), Written: time.Now().UTC().Format(time.RFC3339)}

	runnable := settings.Runnable()
	for _, name := range settings.Enabled() {
		line := Integration{Name: name, State: "yours to start; core never runs it"}
		if slices.Contains(runnable, name) {
			line.Binary = settings.Integration[name].Binary
			w.mu.Lock()
			line.Answers = w.answers[name]
			w.mu.Unlock()
			line.State = "run when core needs it"
			if slices.Contains(line.Answers.Methods, session.MethodRender) {
				line.State = "drawn when something it watches moves"
			}
		}
		report.Integrations = append(report.Integrations, line)
	}

	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return
	}
	temporary := w.opened.Layout.IntegrationsFile() + ".tmp"
	if err := os.WriteFile(temporary, encoded, paths.FileMode); err != nil {
		return
	}
	_ = os.Rename(temporary, w.opened.Layout.IntegrationsFile())
}

func ReadReport(layout paths.Layout) (Report, bool) {
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
