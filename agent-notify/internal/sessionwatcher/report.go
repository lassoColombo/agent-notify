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

// What the session-watcher knows about the integrations, on disk.
//
// This file used to be the supervisor: children, a grace period, consecutive
// failures, a count of attempts before giving up, and the pid matching that
// worked out which connection belonged to which child. All of it is gone with
// the last long-lived integration, and what is left is the part that was never
// about supervising — writing down what core knows, so that something else can
// read it.
//
// Three things read it now. `doctor` shows it. A focus asked from a keybinding
// finds out which programs are containers without running them. And the hook
// finds out who is worth asking for an environment, on the path an agent is
// waiting on, where a process is the one thing it must not spend (R1).

// Integration is one line of the report.
type Integration struct {
	Name string `json:"name"`
	// Answers is what this one said when it was asked what it answers.
	Answers WhatAnIntegrationAnswers `json:"answers"`
	// Binary is what core runs, and its absence is a whole fact: a table
	// without one belongs to something launchd starts and core never runs.
	Binary string `json:"binary,omitempty"`
	// State is what core can say about it as a process, which for most of them
	// is "nothing, until somebody needs it".
	State string `json:"state"`
	PID   int    `json:"pid,omitempty"`
	Since string `json:"since,omitempty"`
}

// Report is what the session-watcher knows about its integrations.
//
// It is a FILE rather than a socket message, and that is a diagnostic decision
// rather than a lazy one: doctor's hardest job is a session-watcher that holds
// the lock and does not answer (§A9.2), and a report you have to ask for over
// the socket is exactly the report you cannot get in that case. This one is
// also readable with `cat`.
type Report struct {
	PID          int           `json:"pid"`
	Written      string        `json:"written"`
	Integrations []Integration `json:"integrations"`
}

// writeReport puts what core knows on disk.
func (w *Watcher) writeReport(settings config.Config) {
	report := Report{PID: os.Getpid(), Written: time.Now().UTC().Format(time.RFC3339)}
	listening := w.subs.Listening()

	seen := map[string]bool{}
	for _, name := range everyIntegrationCoreCanRun(settings) {
		seen[name] = true
		line := Integration{Name: name, Binary: settings.Integration[name].Binary}

		w.mu.Lock()
		line.Answers = w.answers[name]
		w.mu.Unlock()

		if slices.Contains(line.Answers.Methods, session.MethodRender) {
			line.State = "drawn when something it watches moves"
		} else {
			line.State = "run when core needs it"
		}
		report.Integrations = append(report.Integrations, line)
	}

	// Tables with no binary: a client's settings, for something launchd starts
	// and core never runs. They are here so that a display which is configured
	// and not running is visible as itself rather than as nothing at all — the
	// alternative is a menu bar that disappears from doctor the moment it stops.
	for _, name := range everyConfiguredIntegration(settings) {
		if seen[name] {
			continue
		}
		seen[name] = true
		report.Integrations = append(report.Integrations, Integration{
			Name: name, State: "yours to start; core never runs it"})
	}

	// Anything connected. Core started none of it, a disconnection is its own
	// business, and it may come and go as it likes — but a person running
	// doctor should still see it, and seeing it is how they know the launch
	// agent they loaded is doing something.
	for _, one := range listening {
		if seen[one.Name] {
			// Configured as well as connected: say so on the line it already
			// has rather than printing it twice.
			for i := range report.Integrations {
				if report.Integrations[i].Name == one.Name {
					report.Integrations[i].State = "connected"
					report.Integrations[i].PID = one.PID
					report.Integrations[i].Since = one.Since.UTC().Format(time.RFC3339)
				}
			}
			continue
		}
		seen[one.Name] = true
		report.Integrations = append(report.Integrations, Integration{
			Name: one.Name, State: "connected, and in no table here", PID: one.PID,
			Since: one.Since.UTC().Format(time.RFC3339),
		})
	}

	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return
	}
	_ = writeAtomically(w.opened.Layout.IntegrationsFile(), encoded)
}

// writeAtomically is the same tmp-and-rename the store uses, in miniature: a
// reader must never see half a report.
func writeAtomically(path string, content []byte) error {
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, content, paths.FileMode); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

// ReadReport is how everything else reads it, from another process entirely.
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
