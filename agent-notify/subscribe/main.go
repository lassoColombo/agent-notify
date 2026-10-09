package subscribe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/lassoColombo/agent-notify/container"
	"github.com/lassoColombo/agent-notify/session"
)

// Commands is what an integration answers besides `capabilities` and
// `capture-environment`, which every one answers. Each function that is
// filled in is a method core may call; a nil one is not declared and never
// asked (D-81).
type Commands struct {
	// Render paints the view core hands over on stdin. Nil for a display that
	// owns its process.
	Render func(session.View) error
	// Interpret turns what Reads captured into coordinates. It runs in the
	// session-watcher and may ask the tool (D-27). Beside the capture comes
	// core's own: the process chain from the hook upward, nearest first, for
	// a container that places by process (§A8.4, D-93).
	Interpret func(captured json.RawMessage, ancestry []session.Ancestor) (any, error)
	// Focus brings one place to the front, validating the coordinates at the
	// moment of use (R17).
	Focus func(coordinates json.RawMessage) (container.Outcome, error)
	// Focused answers whether that place is in front, in three values (R27).
	Focused func(coordinates json.RawMessage) (container.Verdict, error)
	// Named are this program's own subcommands: install, check, and so on.
	Named map[string]func(arguments []string) int
	// Default runs when no subcommand matched. Nil prints the usage.
	Default func(arguments []string) int
}

func (c Commands) capabilities(i Integration) session.Capabilities {
	answer := session.Capabilities{WakeOn: i.WakeOn, WantEnded: i.WantEnded}
	for _, one := range []struct {
		method string
		filled bool
	}{
		{session.MethodInterpret, c.Interpret != nil},
		{session.MethodFocus, c.Focus != nil},
		{session.MethodFocused, c.Focused != nil},
		{session.MethodRender, c.Render != nil},
	} {
		if one.filled {
			answer.Methods = append(answer.Methods, one.method)
		}
	}
	return answer
}

// theVerbsCoreRuns is what a Named subcommand may not be called.
var theVerbsCoreRuns = []string{
	session.CapabilitiesCommand, session.CaptureCommand,
	session.MethodInterpret, session.MethodFocus, session.MethodFocused, session.MethodRender,
}

// Main dispatches one invocation and returns the exit code. The contract with
// core: one JSON object on stdout and exit 0, anything else meaning "I could
// not answer".
func Main(i Integration, commands Commands, arguments []string) int {
	for name := range commands.Named {
		if slices.Contains(theVerbsCoreRuns, name) {
			fmt.Fprintf(os.Stderr, "%s: a subcommand called %q would shadow the one core runs\n", i.Name, name)
			return 1
		}
	}
	if len(arguments) > 0 {
		if run, known := commands.Named[arguments[0]]; known {
			return run(arguments[1:])
		}
		answer, err := commands.answer(i, arguments[0], os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s %s: %v\n", i.Name, arguments[0], err)
			return 1
		}
		if answer != nil {
			encoded, err := json.Marshal(answer)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s %s: %v\n", i.Name, arguments[0], err)
				return 1
			}
			fmt.Println(string(encoded))
			return 0
		}
	}
	if commands.Default != nil {
		return commands.Default(arguments)
	}
	known := []string{session.CapabilitiesCommand, session.CaptureCommand}
	known = append(known, commands.capabilities(i).Methods...)
	for name := range commands.Named {
		known = append(known, name)
	}
	slices.Sort(known)
	fmt.Fprintf(os.Stderr, "%s takes one of: %v\n", i.Name, known)
	return 1
}

// answer is the subcommands core runs. A nil answer with no error means the
// word was not one of them.
func (c Commands) answer(i Integration, command string, stdin io.Reader) (any, error) {
	switch command {
	case session.CapabilitiesCommand:
		if err := session.ReasonTheseFieldsCannotBeWokenOn(i.WakeOn); err != nil {
			return nil, err
		}
		answer := c.capabilities(i)
		answer.Version = session.Version
		if answer.Methods == nil {
			answer.Methods = []string{}
		}
		return answer, nil
	case session.CaptureCommand:
		if i.Reads == nil {
			return map[string]any{}, nil
		}
		return i.Reads()
	case session.MethodRender:
		if c.Render == nil {
			return nil, nil
		}
		view, err := i.viewOnStdinOrTheStore(stdin)
		if err != nil {
			return nil, err
		}
		return map[string]any{}, c.Render(view)
	case session.MethodInterpret:
		if c.Interpret == nil {
			return nil, nil
		}
		given, err := read(stdin)
		if err != nil {
			return nil, err
		}
		// Core hands the record's captured_context narrowed to this
		// integration: its own entry, and the chain core walked (D-93).
		var captured session.CapturedContext
		if err := json.Unmarshal(given, &captured); err != nil {
			return nil, fmt.Errorf("what arrived on stdin is not a captured context: %w", err)
		}
		return c.Interpret(captured.By[i.Name], captured.Ancestry)
	case session.MethodFocus:
		if c.Focus == nil {
			return nil, nil
		}
		given, err := read(stdin)
		if err != nil {
			return nil, err
		}
		return c.Focus(given)
	case session.MethodFocused:
		if c.Focused == nil {
			return nil, nil
		}
		given, err := read(stdin)
		if err != nil {
			return nil, err
		}
		return c.Focused(given)
	}
	return nil, nil
}

// read takes the whole of stdin as JSON. Empty is `null`: a container asked
// about a session it never captured says so in its own words.
func read(input io.Reader) (json.RawMessage, error) {
	raw, err := io.ReadAll(input)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return json.RawMessage("null"), nil
	}
	if !json.Valid(raw) {
		return nil, fmt.Errorf("what arrived on stdin is not JSON")
	}
	return json.RawMessage(raw), nil
}

// viewOnStdinOrTheStore is the view core handed over, or, for a person running
// this by hand, whatever the store says now.
func (i Integration) viewOnStdinOrTheStore(stdin io.Reader) (session.View, error) {
	handed, err := io.ReadAll(stdin)
	if err != nil {
		return session.View{}, err
	}
	if len(bytes.TrimSpace(handed)) > 0 {
		var view session.View
		if err := json.Unmarshal(handed, &view); err != nil {
			return session.View{}, fmt.Errorf("what arrived on stdin is not a view: %w", err)
		}
		return view, nil
	}
	sessions, err := i.read(i.WantEnded)
	if err != nil {
		return session.View{}, err
	}
	return session.View{Sessions: sessions}, nil
}
