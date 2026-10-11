// Package handshake is `capabilities`: the one question core asks an
// integration before it runs it, and the report of what each one answered
// (D-81). The session-watcher asks at startup and on reload and writes the
// report; `doctor` and a focus off a keypress read the report rather than
// asking a process anything, and ask only when there is none to read.
package handshake

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

// Timeout bounds one `capabilities` call, which prints a constant. A var so a
// test can lengthen it: on a machine running a dozen packages' tests at once,
// a shell script that prints a constant can wait longer than this to be
// scheduled, and the test is not about the timeout.
var Timeout = 2 * time.Second

// ByIntegration is what each integration answers, for a command that is not
// the session-watcher: read from the report, or asked when there is no report
// yet, so a focus still works on a machine where nothing has run.
func ByIntegration(layout paths.Layout, settings config.Config) map[string]session.Capabilities {
	if report, found := Read(layout); found {
		answers := make(map[string]session.Capabilities, len(report.Integrations))
		for _, one := range report.Integrations {
			answers[one.Name] = one.Answers
		}
		return answers
	}
	answers, _ := Ask(settings)
	return answers
}

// Ask runs `capabilities` on every enabled integration at once, under one
// shared timeout. The second map is why one did not answer, which includes
// a program that is not on PATH (D-101).
func Ask(settings config.Config) (map[string]session.Capabilities, map[string]string) {
	enabled := settings.Enabled()
	answers := make(map[string]session.Capabilities, len(enabled))
	unanswered := map[string]string{}

	var asking sync.WaitGroup
	var writing sync.Mutex
	for _, name := range enabled {
		asking.Add(1)
		go func() {
			defer asking.Done()
			answered, err := ask(config.Program(name))
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

func ask(binary string) (session.Capabilities, error) {
	raw, err := tool.Ask(binary, session.CapabilitiesCommand, nil, Timeout)
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
