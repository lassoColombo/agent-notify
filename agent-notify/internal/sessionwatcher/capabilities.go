package sessionwatcher

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/lassoColombo/agent-notify/internal/config"
	"github.com/lassoColombo/agent-notify/internal/paths"
	"github.com/lassoColombo/agent-notify/session"
	"github.com/lassoColombo/agent-notify/tool"
)

// CapabilitiesTimeout bounds one `capabilities` call, which prints a constant.
// A var so a test can lengthen it: on a machine running a dozen packages'
// tests at once, a shell script that prints a constant can wait longer than
// this to be scheduled, and the test is not about the timeout.
var CapabilitiesTimeout = 2 * time.Second

// CapabilitiesByIntegration is what each integration answers, for a command
// that is not the session-watcher: read from the report, or asked when there
// is no report yet, so a focus still works on a machine where nothing has run.
func CapabilitiesByIntegration(layout paths.Layout, settings config.Config) map[string]session.Capabilities {
	if report, found := ReadReport(layout); found {
		answers := make(map[string]session.Capabilities, len(report.Integrations))
		for _, one := range report.Integrations {
			answers[one.Name] = one.Answers
		}
		return answers
	}
	answers, _ := askEveryIntegrationWhatItAnswers(settings)
	return answers
}

// askEveryIntegrationWhatItAnswers runs `capabilities` on all of them at once,
// under one shared timeout. The second map is why one did not answer.
func askEveryIntegrationWhatItAnswers(settings config.Config) (map[string]session.Capabilities, map[string]string) {
	runnable := settings.Runnable()
	answers := make(map[string]session.Capabilities, len(runnable))
	unanswered := map[string]string{}

	var asking sync.WaitGroup
	var writing sync.Mutex
	for _, name := range runnable {
		asking.Add(1)
		go func() {
			defer asking.Done()
			answered, err := ask(settings.Integration[name].Binary)
			writing.Lock()
			defer writing.Unlock()
			if err != nil {
				unanswered[name] = err.Error()
				return
			}
			answers[name] = answered
		}()
	}
	asking.Wait()
	return answers, unanswered
}

func (w *Watcher) askWhatEachIntegrationAnswers() {
	settings := w.opened.Settings
	answers, unanswered := askEveryIntegrationWhatItAnswers(settings)

	for _, name := range settings.Runnable() {
		if problem, failed := unanswered[name]; failed {
			w.logger.Warn("an integration did not say what it answers",
				"integration", name, "problem", problem,
				"until", "it is fixed and `agent-notify watcher reload` is run")
			continue
		}
		w.logger.Debug("asked what it answers", "integration", name,
			"methods", answers[name].Methods, "version", answers[name].Version)
	}

	w.mu.Lock()
	w.answers, w.unanswered = answers, unanswered
	w.mu.Unlock()
}

func ask(binary string) (session.Capabilities, error) {
	raw, err := tool.Ask(binary, session.CapabilitiesCommand, nil, CapabilitiesTimeout)
	if err != nil {
		return session.Capabilities{}, err
	}
	var answered session.Capabilities
	if err := json.Unmarshal(raw, &answered); err != nil {
		return session.Capabilities{}, fmt.Errorf("%s answered %s, which is not a set of capabilities",
			binary, tool.Summarise(raw))
	}
	return answered, nil
}
