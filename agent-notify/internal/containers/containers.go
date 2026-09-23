// Package containers runs the container-integrations: the half of §A11 that
// lives in core.
//
// Everything here is a subprocess with a timeout, run through
// [internal/subcommand]. A container is a program that gets asked things (D-38),
// and it is always a program: the second form, a container written entirely in
// configuration, was removed in D-57.
//
// Capture is deliberately not here. It is asked of any integration that needs to
// see inside the agent, container or not — a display painting a pane title needs
// to know which pane — so it belongs to no role and lives in the public
// [capture] package, run by the hook (D-59).
package containers

import (
	"encoding/json"
	"fmt"
	"time"

	agentnotify "github.com/lassoColombo/agent-notify"
	"github.com/lassoColombo/agent-notify/container"
	"github.com/lassoColombo/agent-notify/internal/config"
	"github.com/lassoColombo/agent-notify/internal/subcommand"
)

// focusTimeout bounds one step of a focus, and one `focused` query. On expiry
// the step is a typed failure naming the container that did not answer, and the
// layers inside it are never attempted (§A11.2).
//
// The two functions it is passed to take it as an argument rather than reading
// it here, so that a test can hand them a short one without a configuration
// file having to exist to say so.
const focusTimeout = 5 * time.Second

// A Container is one configured container, resolved out of the file.
//
// A container is always a program. It was once possible to write one entirely
// in configuration — a list of variables to capture and one command template to
// run — and that is gone (D-57): it put knowledge the integration already held
// into a file the user edits, and it could never answer `focused` at all.
type Container struct {
	Name   string
	Binary string
}

// Configured is every container in `[container] order`, outermost first.
//
// Order is configuration rather than discovery because nesting is not
// discoverable: zellij inside a window aerospace manages looks, from inside,
// exactly like zellij on its own (§A11.2). A name in the order with no table
// behind it is skipped rather than guessed at — and reported, because it is
// almost always a typo in the one place that cannot be typo-checked.
func Configured(settings config.Config) ([]Container, []error) {
	var found []Container
	var problems []error
	for _, name := range settings.Container.Order {
		integration, present := settings.Integration[name]
		switch {
		case !present:
			problems = append(problems, fmt.Errorf(
				"[container] order names %q, which has no [integration.%s] table", name, name))
			continue
		case !integration.IsEnabled():
			continue
		case integration.Binary == "":
			problems = append(problems, fmt.Errorf(
				"[integration.%s] is in [container] order but has no binary to run", name))
			continue
		}
		found = append(found, Container{Name: name, Binary: integration.Binary})
	}
	return found, problems
}

// Interpret turns a captured blob into coordinates.
func Interpret(c Container, captured json.RawMessage, timeout time.Duration) (json.RawMessage, error) {
	return subcommand.Ask(c.Binary, container.InterpretCommand, captured, timeout)
}

// Focus brings one container's place to the front.
func Focus(c Container, coordinates json.RawMessage, timeout time.Duration) container.Outcome {
	if len(coordinates) == 0 || string(coordinates) == "null" {
		return container.Failed(container.NeverPlaced, c.Name+" has no coordinates for this session")
	}

	raw, err := subcommand.Ask(c.Binary, container.FocusCommand, coordinates, timeout)
	if err != nil {
		return container.Failed(container.Unreachable, err.Error())
	}
	var outcome container.Outcome
	if err := json.Unmarshal(raw, &outcome); err != nil {
		return container.Failed(container.Unreachable,
			fmt.Sprintf("%s answered something that is not an outcome: %s", c.Name, subcommand.Summarise(raw)))
	}
	if !outcome.OK && outcome.Problem == "" {
		// A container that says no without saying why has still said no.
		outcome.Problem = container.Refused
	}
	return outcome
}

// Focused asks one container whether its place is the one in front.
func Focused(c Container, coordinates json.RawMessage, timeout time.Duration) container.Verdict {
	if len(coordinates) == 0 || string(coordinates) == "null" {
		return container.Verdict{Answer: container.CannotTell,
			Detail: c.Name + " has no coordinates for this session"}
	}
	raw, err := subcommand.Ask(c.Binary, container.FocusedCommand, coordinates, timeout)
	if err != nil {
		return container.Verdict{Answer: container.CannotTell, Detail: err.Error()}
	}
	var verdict container.Verdict
	if err := json.Unmarshal(raw, &verdict); err != nil {
		return container.Verdict{Answer: container.CannotTell,
			Detail: fmt.Sprintf("%s answered %s", c.Name, subcommand.Summarise(raw))}
	}
	switch verdict.Answer {
	case container.Yes, container.No, container.CannotTell:
		return verdict
	}
	return container.Verdict{Answer: container.CannotTell,
		Detail: fmt.Sprintf("%s answered %q, which is not an answer", c.Name, verdict.Answer)}
}

// Coordinates is what a record holds for one container, or nothing.
func Coordinates(record agentnotify.Record, name string) json.RawMessage {
	return record.DerivedContext[name]
}

// A Step is one layer of a focus, and what came of it.
type Step struct {
	Container string
	Outcome   container.Outcome
	// Skipped means this layer had nothing to say about this session, which is
	// not the same as having failed.
	Skipped bool
}

// FocusSession walks the configured containers outermost first and brings the
// session to the front (§A11.2).
//
// Each step must succeed before the next is attempted, because the inner one is
// meaningless without the outer: focusing a zellij pane in a window on a
// workspace you are not looking at leaves you looking at nothing.
//
// A layer with no coordinates for this session is SKIPPED rather than failed,
// and that refinement is load-bearing. Partial placement is the ordinary case —
// you started zellij inside a terminal window that no window manager was
// recording — and treating it as failure would mean that anybody whose outer
// layer is unconfigured can never focus anything. Only when NO layer placed this
// session is there nothing to do, and that has its own name (§A11.7).
//
// "No coordinates" arrives in two spellings and both are skipped: no section in
// the record at all, and a container that answers NeverPlaced when asked. The
// second one exists because interpretation is not a yes-or-no — aerospace
// interprets every session it is given and can place only the ones it can see a
// window for (D-45).
func FocusSession(settings config.Config, record agentnotify.Record) ([]Step, container.Outcome) {
	configured, problems := Configured(settings)
	if len(configured) == 0 {
		detail := "nothing is listed in [container] order"
		if len(problems) > 0 {
			detail = problems[0].Error()
		}
		return nil, container.Failed(container.NoContainer, detail)
	}

	var steps []Step
	acted := false
	for _, one := range configured {
		coordinates := Coordinates(record, one.Name)
		if len(coordinates) == 0 {
			steps = append(steps, Step{Container: one.Name, Skipped: true,
				Outcome: container.Failed(container.NeverPlaced,
					one.Name+" has no coordinates for this session")})
			continue
		}
		outcome := Focus(one, coordinates, focusTimeout)
		if outcome.Problem == container.NeverPlaced {
			// The same thing the branch above says, said by the container
			// itself rather than by the absence of a section. A layer that
			// looked and found nothing of its own here is a layer with nothing
			// to do, and stopping the walk on it would mean that installing an
			// outer container breaks focusing for every session that started
			// before it — the inner pane, which is right there and would have
			// worked, never gets asked.
			steps = append(steps, Step{Container: one.Name, Skipped: true, Outcome: outcome})
			continue
		}
		steps = append(steps, Step{Container: one.Name, Outcome: outcome})
		if !outcome.OK {
			return steps, outcome
		}
		acted = true
	}

	if !acted {
		return steps, container.Failed(container.NeverPlaced,
			"no container ever placed this session — it started somewhere none of them was watching")
	}
	return steps, container.Done()
}

// IsFocused asks every configured container whether its place is in front, and
// combines the answers the only way that is honest (§A11.6).
//
// A session is in front only if every layer agrees. One layer saying no is
// decisive — you are demonstrably looking at something else. Everything short of
// unanimous yes is "cannot tell", and the notifier's rule for that third answer
// is to notify anyway: silence would mean a user with no containers never
// receives a notification at all, which is worse than an occasional redundant
// one (R27, §A11.7).
func IsFocused(settings config.Config, record agentnotify.Record) container.Verdict {
	configured, _ := Configured(settings)
	if len(configured) == 0 {
		return container.Verdict{Answer: container.CannotTell,
			Detail: "no container is configured, so nothing can say what is in front"}
	}

	agreed := 0
	var unsure string
	for _, one := range configured {
		verdict := Focused(one, Coordinates(record, one.Name), focusTimeout)
		switch verdict.Answer {
		case container.No:
			return container.Verdict{Answer: container.No,
				Detail: one.Name + ": " + verdict.Detail}
		case container.Yes:
			agreed++
		default:
			if unsure == "" {
				unsure = one.Name + ": " + verdict.Detail
			}
		}
	}
	if agreed == len(configured) {
		return container.Verdict{Answer: container.Yes}
	}
	return container.Verdict{Answer: container.CannotTell, Detail: unsure}
}
