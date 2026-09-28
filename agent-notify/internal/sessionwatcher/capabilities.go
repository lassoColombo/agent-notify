package sessionwatcher

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/lassoColombo/agent-notify/internal/config"
	"github.com/lassoColombo/agent-notify/internal/paths"
	"github.com/lassoColombo/agent-notify/internal/subcommand"
	"github.com/lassoColombo/agent-notify/session"
	"github.com/lassoColombo/agent-notify/tool"
)

// capabilitiesTimeout bounds one `capabilities` call, which prints a constant.
const capabilitiesTimeout = 2 * time.Second

// WhatAnIntegrationAnswers is one integration's reply to `capabilities`, or
// the reason there was none. The reason is kept because a program that answers
// no methods and one that could not be asked look identical and mean opposite
// things.
type WhatAnIntegrationAnswers struct {
	session.Capabilities
	Problem string `json:"problem,omitempty"`
}

// MethodsByIntegration is what each integration answers, for a command that
// is not the session-watcher: read from the report, or asked when there is no
// report yet, so a focus still works on a machine where nothing has run.
func MethodsByIntegration(layout paths.Layout, settings config.Config) map[string][]string {
	if report, found := ReadReport(layout); found {
		methods := make(map[string][]string, len(report.Integrations))
		for _, one := range report.Integrations {
			methods[one.Name] = one.Answers.Methods
		}
		return methods
	}
	return methodsIn(askEveryIntegrationWhatItAnswers(settings))
}

func (w *Watcher) methodsByIntegration() map[string][]string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return methodsIn(w.answers)
}

func methodsIn(answers map[string]WhatAnIntegrationAnswers) map[string][]string {
	methods := make(map[string][]string, len(answers))
	for name, answered := range answers {
		methods[name] = answered.Methods
	}
	return methods
}

// askEveryIntegrationWhatItAnswers runs `capabilities` on all of them at once,
// under one shared timeout.
func askEveryIntegrationWhatItAnswers(settings config.Config) map[string]WhatAnIntegrationAnswers {
	runnable := settings.Runnable()
	answers := make(map[string]WhatAnIntegrationAnswers, len(runnable))

	var asking sync.WaitGroup
	var writing sync.Mutex
	for _, name := range runnable {
		asking.Add(1)
		go func() {
			defer asking.Done()
			answered := ask(settings.Integration[name].Binary)
			writing.Lock()
			answers[name] = answered
			writing.Unlock()
		}()
	}
	asking.Wait()
	return answers
}

func (w *Watcher) askWhatEachIntegrationAnswers() {
	settings := w.opened.Settings
	answers := askEveryIntegrationWhatItAnswers(settings)

	for _, name := range settings.Runnable() {
		if answers[name].Problem == "" {
			w.logger.Debug("asked what it answers", "integration", name,
				"methods", answers[name].Methods, "version", answers[name].Version)
			continue
		}
		w.logger.Warn("an integration did not say what it answers",
			"integration", name, "problem", answers[name].Problem,
			"until", "it is fixed and `agent-notify watcher reload` is run")
	}

	w.mu.Lock()
	w.answers = answers
	w.mu.Unlock()
}

func ask(binary string) WhatAnIntegrationAnswers {
	raw, err := subcommand.Ask(binary, session.CapabilitiesCommand, nil, capabilitiesTimeout)
	if err != nil {
		return WhatAnIntegrationAnswers{Problem: err.Error()}
	}
	var answered WhatAnIntegrationAnswers
	if err := json.Unmarshal(raw, &answered.Capabilities); err != nil {
		return WhatAnIntegrationAnswers{
			Problem: binary + " answered " + tool.Summarise(raw) +
				", which is not a set of capabilities"}
	}
	return answered
}
