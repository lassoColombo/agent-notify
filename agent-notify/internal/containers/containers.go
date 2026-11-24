// Package containers runs the container-integrations: the half of §A11 that
// lives in core.
//
// Everything here is a subprocess with a timeout, run through [tool.Ask].
// A container is a program that gets asked things (D-38), and it is always a
// program: the second form, a container written entirely in
// configuration, was removed in D-57.
//
// Capture is deliberately not here. It is asked of any integration that needs to
// see inside the agent, container or not — a display painting a pane title needs
// to know which pane — so it belongs to no role: it is `Reads` on a
// subscribe.Integration, run by the hook (D-59).
package containers

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/lassoColombo/agent-notify/container"
	"github.com/lassoColombo/agent-notify/internal/config"
	"github.com/lassoColombo/agent-notify/internal/process"
	"github.com/lassoColombo/agent-notify/session"
	"github.com/lassoColombo/agent-notify/tool"
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
	// Methods is what this one said it answers, so that a verb it never
	// implemented costs a lookup rather than a process (§A10.3).
	Methods []string
}

// Configured is every container, outermost first.
//
// A container is an integration that answers `focus`, and it says so itself.
// “containers“ does not decide which programs are containers — it
// keeps the one job that is genuinely the user's, which is how the shells nest:
// tmux inside a window aerospace manages looks, from inside, exactly like
// tmux on its own, so nesting is not discoverable and never will be (§A11.2).
// With one container there is nothing to nest, so an absent order is that one
// (D-101); with two or more, the order has to be written.
//
// answers is what each one said to `capabilities`, handed over by whoever read
// it: the session-watcher has it in memory and a command reads the report. This
// package runs containers and does not decide what a container is.
func Configured(settings config.Config, answers map[string]session.Capabilities) ([]Container, []error) {
	var declared []string
	for _, name := range settings.Enabled() {
		if slices.Contains(answers[name].Methods, session.MethodFocus) {
			declared = append(declared, name)
		}
	}
	order := settings.Containers
	if len(order) == 0 {
		switch len(declared) {
		case 0:
			return nil, nil
		case 1:
			order = declared
		default:
			return nil, []error{fmt.Errorf(
				"%s all answer `focus`, and which is outside which is not discoverable: "+
					"write `containers`, outermost first", strings.Join(declared, " and "))}
		}
	}

	var found []Container
	var problems []error
	for _, name := range order {
		switch {
		case !slices.Contains(settings.OnPath(), name):
			problems = append(problems, fmt.Errorf(
				"`containers` names %q, and there is no %s on PATH", name, config.Program(name)))
			continue
		case !settings.Integrations[name].IsEnabled():
			continue
		case !slices.Contains(answers[name].Methods, session.MethodFocus):
			problems = append(problems, fmt.Errorf(
				"%s is in `containers` and does not answer `focus`, "+
					"so it cannot bring anything to the front", name))
			continue
		}
		found = append(found, Container{
			Name: name, Binary: config.Program(name), Methods: answers[name].Methods})
	}

	// A container that answers `focus` and is not in the order is a container
	// by its own account with nowhere in the nesting, so it is never walked.
	for _, name := range declared {
		if !slices.Contains(order, name) {
			problems = append(problems, fmt.Errorf(
				"%s answers `focus` and is not in `containers`, so nothing will ever "+
					"ask it to: add it, outermost first", name))
		}
	}
	return found, problems
}

// Interpret turns what was captured into coordinates. The container is handed
// the record's captured_context narrowed to itself: its own entry, since the
// others are opaque to it as to core (R7), and the chain core walked from the
// hook upward, which is how a container that places by process knows the
// process (§A8.4, D-93).
func Interpret(c Container, captured session.CapturedContext, timeout time.Duration) (json.RawMessage, error) {
	handed, err := json.Marshal(session.CapturedContext{
		CapturedAt: captured.CapturedAt,
		Ancestry:   captured.Ancestry,
		By:         map[string]json.RawMessage{c.Name: captured.By[c.Name]},
	})
	if err != nil {
		return nil, err
	}
	return tool.Ask(c.Binary, session.MethodInterpret, handed, timeout)
}

// Focus brings one container's place to the front: through the window given,
// when a container outside has just brought one forward for it (D-141).
func Focus(c Container, coordinates json.RawMessage, window *container.Window, timeout time.Duration) container.Outcome {
	if len(coordinates) == 0 || string(coordinates) == "null" {
		return container.Failed(container.NeverPlaced, c.Name+" has no coordinates for this session")
	}
	input, err := json.Marshal(container.FocusInput{Coordinates: coordinates, Window: window})
	if err != nil {
		return container.Failed(container.Unreachable, err.Error())
	}
	return outcomeOf(c, session.MethodFocus, input, timeout)
}

// WindowsShowing asks the container inside which windows already show its
// place, the one to prefer first.
func WindowsShowing(c Container, coordinates json.RawMessage, timeout time.Duration) ([]container.Window, error) {
	raw, err := tool.Ask(c.Binary, session.MethodWindowsShowing, coordinates, timeout)
	if err != nil {
		return nil, err
	}
	var windows []container.Window
	if err := json.Unmarshal(raw, &windows); err != nil {
		return nil, fmt.Errorf("%s answered something that is not a list of windows: %s", c.Name, tool.Summarise(raw))
	}
	return windows, nil
}

// FocusWindow asks a container outside to bring forward a window a container
// inside named.
func FocusWindow(c Container, window container.Window, ancestry []session.Ancestor, timeout time.Duration) container.Outcome {
	input, err := json.Marshal(container.FocusWindowInput{Window: window, Ancestry: ancestry})
	if err != nil {
		return container.Failed(container.Unreachable, err.Error())
	}
	return outcomeOf(c, session.MethodFocusWindow, input, timeout)
}

// outcomeOf runs one of the two verbs that answer an outcome.
func outcomeOf(c Container, method string, input []byte, timeout time.Duration) container.Outcome {
	raw, err := tool.Ask(c.Binary, method, input, timeout)
	if err != nil {
		return container.Failed(container.Unreachable, err.Error())
	}
	var outcome container.Outcome
	if err := json.Unmarshal(raw, &outcome); err != nil {
		return container.Failed(container.Unreachable,
			fmt.Sprintf("%s answered something that is not an outcome: %s", c.Name, tool.Summarise(raw)))
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
	raw, err := tool.Ask(c.Binary, session.MethodFocused, coordinates, timeout)
	if err != nil {
		return container.Verdict{Answer: container.CannotTell, Detail: err.Error()}
	}
	var verdict container.Verdict
	if err := json.Unmarshal(raw, &verdict); err != nil {
		return container.Verdict{Answer: container.CannotTell,
			Detail: fmt.Sprintf("%s answered %s", c.Name, tool.Summarise(raw))}
	}
	switch verdict.Answer {
	case container.Yes, container.No, container.CannotTell:
		return verdict
	}
	return container.Verdict{Answer: container.CannotTell,
		Detail: fmt.Sprintf("%s answered %q, which is not an answer", c.Name, verdict.Answer)}
}

// Coordinates is what a record holds for one container, or nothing.
func Coordinates(record session.Record, name string) json.RawMessage {
	return record.DerivedContext[name]
}

// A Step is one layer of a focus, and what came of it.
type Step struct {
	Container string
	Outcome   container.Outcome
	// Skipped means this layer had nothing to say about this session, which is
	// not the same as having failed.
	Skipped bool
	// Window is, for a step that brought a window forward, the inside
	// container's name for it.
	Window string
}

// FocusSession walks the configured containers outermost first and brings the
// session to the front (§A11.2).
//
// Each step must succeed before the next is attempted, because the inner one is
// meaningless without the outer: focusing a tmux pane in a window on a
// workspace you are not looking at leaves you looking at nothing.
//
// A layer with no coordinates for this session is SKIPPED rather than failed,
// and that refinement is load-bearing. Partial placement is the ordinary case —
// you started tmux inside a terminal window that no window manager was
// recording — and treating it as failure would mean that anybody whose outer
// layer is unconfigured can never focus anything. Only when NO layer placed this
// session is there nothing to do, and that has its own name (§A11.7).
//
// "No coordinates" arrives in two spellings and both are skipped: no section in
// the record at all, and a container that answers NeverPlaced when asked. The
// second one exists because interpretation is not a yes-or-no — aerospace
// interprets every session it is given and can place only the ones it can see a
// window for (D-45).
//
// A skipped layer that can bring forward a window it is shown is not done with
// (D-141). An agent in tmux gives aerospace nothing to find its window by, but
// tmux knows which terminals already show the agent's session, and the title
// tmux gave each one's window. So before the layer inside acts, it is asked
// which windows show its place, and the skipped layers outside to bring one
// forward; the first that comes forward is the one the layer inside then shows
// its place through. When none does, the layer inside acts as it would have
// alone. A terminal is moved only once its window is in front, which is what
// D-123 asked of every focus.
func FocusSession(
	settings config.Config, answers map[string]session.Capabilities, record session.Record,
	prober process.ReadsProcessFacts,
) ([]Step, container.Outcome) {
	configured, problems := Configured(settings, answers)
	if len(configured) == 0 {
		detail := "nothing is listed in `containers`"
		if len(problems) > 0 {
			detail = problems[0].Error()
		}
		return nil, container.Failed(container.NoContainer, detail)
	}

	var steps []Step
	acted := false
	// outside is the skipped layers, so far, that can bring forward a window
	// a layer inside names.
	var outside []Container
	skip := func(one Container, outcome container.Outcome) {
		steps = append(steps, Step{Container: one.Name, Skipped: true, Outcome: outcome})
		if slices.Contains(one.Methods, session.MethodFocusWindow) {
			outside = append(outside, one)
		}
	}
	for _, one := range configured {
		coordinates := Coordinates(record, one.Name)
		if len(coordinates) == 0 {
			skip(one, container.Failed(container.NeverPlaced, one.Name+" has no coordinates for this session"))
			continue
		}
		var window *container.Window
		if len(outside) > 0 && slices.Contains(one.Methods, session.MethodWindowsShowing) {
			var raised []Step
			window, raised = throughAWindow(outside, one, coordinates, prober)
			steps = append(steps, raised...)
		}
		outcome := Focus(one, coordinates, window, focusTimeout)
		if outcome.Problem == container.NeverPlaced {
			// The same thing the branch above says, said by the container
			// itself rather than by the absence of a section. A layer that
			// looked and found nothing of its own here is a layer with nothing
			// to do, and stopping the walk on it would mean that installing an
			// outer container breaks focusing for every session that started
			// before it — the inner pane, which is right there and would have
			// worked, never gets asked.
			skip(one, outcome)
			continue
		}
		steps = append(steps, Step{Container: one.Name, Outcome: outcome})
		if !outcome.OK {
			return steps, outcome
		}
		acted = true
		outside = nil
	}

	if !acted {
		return steps, container.Failed(container.NeverPlaced,
			"no container ever placed this session — it started somewhere none of them was watching")
	}
	return steps, container.Done()
}

// throughAWindow asks the layer inside which windows already show its place,
// and the layers outside to bring one forward, in the order the layer inside
// prefers them. It returns the window that came forward, if one did, and a
// step for each one asked. A window that does not come forward is a step
// skipped and not a focus failed: the layer inside acts anyway.
func throughAWindow(
	outside []Container, inside Container, coordinates json.RawMessage, prober process.ReadsProcessFacts,
) (*container.Window, []Step) {
	windows, err := WindowsShowing(inside, coordinates, focusTimeout)
	if err != nil {
		return nil, []Step{{Container: inside.Name, Skipped: true,
			Outcome: container.Failed(container.Unreachable, err.Error())}}
	}
	var steps []Step
	for _, window := range windows {
		ancestry := process.Ancestry(window.PID, prober)
		for _, manager := range outside {
			outcome := FocusWindow(manager, window, ancestry, focusTimeout)
			steps = append(steps, Step{Container: manager.Name, Outcome: outcome, Skipped: !outcome.OK, Window: window.ID})
			if outcome.OK {
				return &window, steps
			}
		}
	}
	return nil, steps
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
func IsFocused(
	settings config.Config, answers map[string]session.Capabilities, record session.Record,
) container.Verdict {
	configured, _ := Configured(settings, answers)

	// Only the ones that answer `focused` are in the vote. A container that
	// never implemented it used to be asked anyway and to reply "cannot tell",
	// which unanimity turns into a permanent "cannot tell" for every session on
	// the machine — a whole layer voting nothing and vetoing everything.
	canTell := make([]Container, 0, len(configured))
	for _, one := range configured {
		if slices.Contains(one.Methods, session.MethodFocused) {
			canTell = append(canTell, one)
		}
	}
	if len(canTell) == 0 {
		return container.Verdict{Answer: container.CannotTell,
			Detail: "no container can say what is in front"}
	}

	agreed := 0
	var unsure string
	for _, one := range canTell {
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
	if agreed == len(canTell) {
		return container.Verdict{Answer: container.Yes}
	}
	return container.Verdict{Answer: container.CannotTell, Detail: unsure}
}
