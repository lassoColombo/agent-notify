package sessionwatcher

import (
	"encoding/json"
	"slices"
	"sync"
	"time"

	"github.com/lassoColombo/agent-notify/internal/config"
	"github.com/lassoColombo/agent-notify/internal/paths"
	"github.com/lassoColombo/agent-notify/internal/subcommand"
	"github.com/lassoColombo/agent-notify/session"
)

// Asking every integration what it answers, which is the one thing core cannot
// learn any other way.
//
// It happens twice: when this process starts, and when somebody reloads the
// configuration. Never on a sweep — the answer is a property of the program on
// disk, and a program does not change its mind between ticks. A person who
// rebuilds an integration reloads, which is already the gesture for "I changed
// something out here" (§A9.4).

// capabilitiesTimeout bounds one `capabilities` call. It is generous for what
// it is — printing a constant — and short enough that a program that hangs
// costs one startup rather than the startup.
const capabilitiesTimeout = 2 * time.Second

// WhatAnIntegrationAnswers is one integration's reply to `capabilities`, or the
// reason there was no reply.
//
// The reason is kept rather than dropped because the two look identical from
// here and mean opposite things: an integration that answers no methods is
// doing nothing on purpose, and one that could not be asked is broken. Without
// Problem, a display whose binary was deleted reads exactly like a display that
// has nothing to offer, and core would quietly stop running it for ever.
type WhatAnIntegrationAnswers struct {
	session.Capabilities
	Problem string `json:"problem,omitempty"`
}

// everyIntegrationCoreCanRun is every integration with a binary core may run:
// enabled, and with something to run.
//
// `binary` is what says so. An integration that keeps a table for its settings
// and is never run by core does not name one, and is therefore not here.
func everyIntegrationCoreCanRun(settings config.Config) []string {
	var runnable []string
	for _, name := range everyConfiguredIntegration(settings) {
		if settings.Integration[name].Binary == "" {
			continue
		}
		runnable = append(runnable, name)
	}
	return runnable
}

// everyConfiguredIntegration is every enabled table, whether or not core can
// run what it describes. A table with no binary belongs to a client: something
// launchd starts, which connects on its own and keeps its settings here.
func everyConfiguredIntegration(settings config.Config) []string {
	var configured []string
	for name, integration := range settings.Integration {
		if !integration.IsEnabled() {
			continue
		}
		configured = append(configured, name)
	}
	slices.Sort(configured)
	return configured
}

// MethodsByIntegration is what each integration answers, for a command that is
// not the session-watcher.
//
// Read from the report, which is the whole reason the session-watcher writes
// one: a focus asked from a keybinding must not run every container to find out
// what a container is. When there is no report — no session-watcher has run
// since this machine booted — it asks, because the alternative is telling
// somebody that the containers they configured do not exist.
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

// TheIntegrationsToAsk is who gets run on the hook path.
//
// Nobody declares `capture-environment` and everybody is asked it, so what is
// left to know is which of these are real, answering programs: one that could
// not be asked cannot read an agent's environment either, and running it would
// fork something broken on the path an agent is waiting on.
//
// Nothing here asks anything. Where [MethodsByIntegration] falls back to
// running every integration, this falls back to the configuration, because R1
// is absolute about this path: a file read, or the file the user wrote, and
// never a process.
func TheIntegrationsToAsk(layout paths.Layout, settings config.Config) []string {
	report, found := ReadReport(layout)
	if !found {
		// No session-watcher has run since this machine booted. Everything with
		// a binary, which is what the report will say about them once one has.
		return everyIntegrationCoreCanRun(settings)
	}
	var answered []string
	for _, one := range report.Integrations {
		// A line with no binary is a client's table — settings for something
		// launchd starts and core never runs. It is in the report so that
		// doctor can see it, and it is not something to fork.
		if one.Binary != "" && one.Answers.Problem == "" {
			answered = append(answered, one.Name)
		}
	}
	slices.Sort(answered)
	return answered
}

// methodsByIntegration is the same thing for the session-watcher, which asked
// at startup and has the answers in hand.
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

// askEveryIntegrationWhatItAnswers runs `capabilities` on all of them at once.
//
// Concurrently, and under one shared timeout rather than one each, so that the
// cost of asking is the slowest answer and not the sum of them — the same shape
// the hook uses for `capture-environment`, for the same reason.
func askEveryIntegrationWhatItAnswers(settings config.Config) map[string]WhatAnIntegrationAnswers {
	runnable := everyIntegrationCoreCanRun(settings)
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

// askWhatEachIntegrationAnswers is the session-watcher doing it, and saying what it
// found out.
func (w *Watcher) askWhatEachIntegrationAnswers() {
	settings := w.opened.Settings
	answers := askEveryIntegrationWhatItAnswers(settings)

	for _, name := range everyIntegrationCoreCanRun(settings) {
		if answers[name].Problem == "" {
			w.logger.Debug("asked what it answers", "integration", name,
				"methods", answers[name].Methods, "version", answers[name].Version)
			continue
		}
		// Said out loud because everything downstream of it is silence: core
		// runs an integration for the methods it declared, so one that did not
		// declare any is one nothing will ever run again.
		w.logger.Warn("an integration did not say what it answers",
			"integration", name, "problem", answers[name].Problem,
			"until", "it is fixed and `agent-notify watcher reload` is run")
	}

	w.mu.Lock()
	w.answers = answers
	w.mu.Unlock()
}

// ask runs one `capabilities` and reads the answer.
func ask(binary string) WhatAnIntegrationAnswers {
	raw, err := subcommand.Ask(binary, session.CapabilitiesCommand, nil, capabilitiesTimeout)
	if err != nil {
		return WhatAnIntegrationAnswers{Problem: err.Error()}
	}
	var answered WhatAnIntegrationAnswers
	if err := json.Unmarshal(raw, &answered.Capabilities); err != nil {
		return WhatAnIntegrationAnswers{
			Problem: binary + " answered " + subcommand.Summarise(raw) +
				", which is not a set of capabilities"}
	}
	return answered
}
