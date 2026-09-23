// Package container is what a container-integration imports.
//
// A container answers two questions about one session — where does it live, and
// bring it to the front — and it answers them by being *run*, not by staying
// connected (plan.md D-38). A display is told things and therefore holds a
// socket; a container is asked things and therefore has subcommands.
//
// An author writes up to four functions and a two-line main:
//
//	func main() {
//	    os.Exit(container.Main(container.Integration{
//	        Name:      "zellij-container",
//	        Capture:   capture,    // runs inside the agent: read, do not ask
//	        Interpret: interpret,  // runs in the session-watcher: ask the tool
//	        Focus:     focus,
//	        Focused:   focused,
//	    }))
//	}
//
// The split between Capture and Interpret is the one thing to understand, and
// it is not stylistic (D-27). Capture runs as a child of the hook, inside the
// agent's process tree, because the agent's environment exists nowhere else —
// and it must therefore never talk to anything, never open a socket and never
// wait. Interpret runs later, in the session-watcher, where blocking is allowed
// and where asking zellij which tab holds pane 7 costs the agent nothing.
//
// Only Capture is shared with the rest of the world, which is why it lives in
// [capture] and is merely wired up here (D-59). It is not a container's question
// at all: a display needs it too, and the three functions below are the ones
// that make this a container.
package container

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/lassoColombo/agent-notify/capture"
)

// The subcommands a container answers. They are named after what they do rather
// than after the record fields they end up in, because an author implementing
// one should not have to know the record's shape.
//
// The fourth, `capture-environment`, is [capture.Command]: it is answered by
// integrations that are not containers as well, so it is not declared here.
const (
	InterpretCommand = "interpret-environment"
	FocusCommand     = "focus"
	FocusedCommand   = "focused"
)

// Integration is what an author fills in. Every function is optional: a
// container that can place a session but not focus it is a legitimate thing to
// ship, and core asks only for what is there.
type Integration struct {
	// Name is what this calls itself — in its config table, in
	// `[container] order`, and as the key of its own section of a record.
	Name string

	// Capture reads the agent's environment. It runs on the hook path, as a
	// child of the agent, under `capture-timeout`, and the rule is absolute:
	// read local state and return. Never ask the tool anything here.
	//
	// It is declared here for a container's convenience — a container usually
	// needs all four — but it is [capture.Reads] and Main dispatches it to
	// [capture.Main], so a container and a display answer this one subcommand
	// through the same code (D-59).
	Capture capture.Reads

	// Interpret turns what Capture returned into coordinates. It runs in the
	// session-watcher under `interpret-timeout` and is allowed to block.
	Interpret func(captured json.RawMessage) (any, error)

	// Focus brings one place to the front. It is given what Interpret
	// returned, and the coordinates are validated at the moment of use rather
	// than trusted — panes and windows die without telling anybody (R17).
	Focus func(coordinates json.RawMessage) (Outcome, error)

	// Focused answers whether that place is the one in front, in three values,
	// because "nobody can tell" is the state of every fresh install (R27).
	Focused func(coordinates json.RawMessage) (Verdict, error)
}

// Problem is why a focus did not happen, as a word rather than a sentence, so
// that a caller can act differently on each: "the pane is gone" should offer to
// prune the record and "zellij is not running" should not (§A11.3).
type Problem string

const (
	// PlaceIsGone — the pane, window or tab this session was in no longer
	// exists. The session may well still be alive somewhere else.
	PlaceIsGone Problem = "place-is-gone"
	// NotRunning — the tool itself is not there to be asked.
	NotRunning Problem = "not-running"
	// NeverPlaced — this container has no coordinates for this session, which
	// is the ordinary case for a session that started outside it.
	NeverPlaced Problem = "never-placed"
	// NoContainer — nothing is configured to place anything. It is deliberately
	// not the same word as NeverPlaced: those two feel identical and are not,
	// and a fresh install is the first one (§A11.7).
	NoContainer Problem = "no-container-configured"
	// Unreachable — the container was run and could not answer: it crashed, it
	// timed out, or the binary named in the config is not there. Core
	// synthesises this one; no integration ever reports it about itself.
	Unreachable Problem = "container-unreachable"
	// Refused — the tool was asked, understood, and would not.
	Refused Problem = "refused"
	// Ambiguous — this container can see several places the session could be
	// and nothing to choose between them. It is not "gone" and it is not "never
	// placed": the evidence points at more than one door, and opening a door at
	// random puts somebody in front of a window that is not theirs while
	// looking exactly like success (§A11.3).
	Ambiguous Problem = "ambiguous"
)

// Outcome is what came of a focus.
type Outcome struct {
	OK bool `json:"ok"`
	// Problem is set when OK is false. An empty one is read as Refused rather
	// than as success: a container that says "no" without saying why has still
	// said no.
	Problem Problem `json:"problem,omitempty"`
	Detail  string  `json:"detail,omitempty"`
}

// Done is the outcome of a focus that worked.
func Done() Outcome { return Outcome{OK: true} }

// Failed is the outcome of one that did not, named.
func Failed(problem Problem, detail string) Outcome {
	return Outcome{Problem: problem, Detail: detail}
}

// Answer is a three-valued yes. There is no bool anywhere in this file for the
// reason R27 gives: the third answer is part of the design, and a bool forces
// whoever receives it to guess which of the two it meant.
type Answer string

const (
	Yes        Answer = "yes"
	No         Answer = "no"
	CannotTell Answer = "cannot-tell"
)

// Verdict is what `focused` prints.
type Verdict struct {
	Answer Answer `json:"answer"`
	Detail string `json:"detail,omitempty"`
}

// Main dispatches one subcommand and returns the exit code.
//
// The contract with core, in one paragraph: the answer is one JSON object on
// stdout and the exit code is 0; anything else means "I could not answer", and
// stderr is the reason. Core never parses stderr and never reads meaning into a
// particular non-zero code — a container that crashes, hangs or was deleted all
// arrive as the same thing, which is the only honest way to treat a program
// that did not run.
func Main(integration Integration) int {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "%s <%s|%s|%s|%s>\n", integration.Name,
			capture.Command, InterpretCommand, FocusCommand, FocusedCommand)
		return 1
	}

	// Capture is the one subcommand this package does not answer itself, and it
	// is dispatched before anything else for the reason the whole split exists:
	// it runs on the path the agent waits on and must do nothing but read.
	if os.Args[1] == capture.Command {
		return capture.Main(integration.Name, integration.Capture)
	}

	answer, err := integration.answer(os.Args[1], os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s %s: %v\n", integration.Name, os.Args[1], err)
		return 1
	}
	encoded, err := json.Marshal(answer)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s %s: %v\n", integration.Name, os.Args[1], err)
		return 1
	}
	fmt.Println(string(encoded))
	return 0
}

var errUnimplemented = errors.New("this container does not answer that")

func (i Integration) answer(command string, input io.Reader) (any, error) {
	switch command {
	case InterpretCommand:
		if i.Interpret == nil {
			return nil, errUnimplemented
		}
		given, err := read(input)
		if err != nil {
			return nil, err
		}
		return i.Interpret(given)

	case FocusCommand:
		if i.Focus == nil {
			return nil, errUnimplemented
		}
		given, err := read(input)
		if err != nil {
			return nil, err
		}
		return i.Focus(given)

	case FocusedCommand:
		if i.Focused == nil {
			// Not knowing is an answer, and it is the one every container that
			// has never heard of this question should give (R27).
			return Verdict{Answer: CannotTell, Detail: "this container cannot say"}, nil
		}
		given, err := read(input)
		if err != nil {
			return nil, err
		}
		return i.Focused(given)
	}
	return nil, fmt.Errorf("%q is not a command", command)
}

// read takes the whole of stdin as JSON. An empty stdin is `null` rather than
// an error: `capture-environment` is given nothing, and a container asked to
// interpret a session it never captured should say so in its own words rather
// than fail to start.
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
