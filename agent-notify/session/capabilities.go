package session

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
)

// The handshake: what core asks a tool-integration about itself, before it
// knows anything else about it.
//
// It exists because of one fact that has to be known too early for any of the
// existing channels. The connect handshake (§A10.3) is how an integration
// declares itself, and it is only reachable by something that has already
// decided to connect — so it can carry everything except whether connecting is
// what this program does at all. What decided that instead was a program's
// ABSENCE from `[container] order`: a property of the integration read out of
// the user's file, which is the thing §A10.3 and D-66 both exist to prevent.
//
// So: a subcommand, answered without connecting, before anything else is asked.
// It is not a key in the config for the same reason D-57 took the list of
// captured variables out of the config — a second copy of what the source
// already knows is a copy that can disagree with it, silently.

// CapabilitiesCommand is the subcommand every tool-integration answers.
const CapabilitiesCommand = "capabilities"

// The methods an integration can claim. They ARE the subcommand names: what a
// program says it answers is what core will run.
//
// `capture-environment` is deliberately absent. Every integration core can run
// is asked it and none of them declares it: one that reads nothing answers an
// empty object, which core stores as nothing at all. A bit in the config saying
// who to ask was only ever there because the hook has no socket (D-39), and a
// hook that asks everybody needs no such bit.
const (
	MethodInterpret = "interpret-environment"
	MethodFocus     = "focus"
	MethodFocused   = "focused"
	MethodRender    = "render"
)

// Capabilities is everything core asks an integration about itself.
//
// It is methods rather than roles, and the difference is the whole point. A
// role is a category a person infers and then writes down — in the table, in
// `[container] order`, in a `roles` field — and each copy is somewhere it can
// be wrong. A method is what core is about to run. "Is a container" becomes
// "answers `focus`", which is not an opinion.
type Capabilities struct {
	// Version is the core this was built against. Announced and logged, never
	// negotiated on: everything here is built and released together, so a
	// version that does not match is a deployment somebody half finished
	// rather than a protocol to arbitrate (D-77). It is stamped by [Answer]
	// rather than written by an author, because it is a fact about the linked
	// library and not something anybody should be able to get wrong.
	Version string `json:"version"`

	// Methods is what is worth calling, and not what will not error. A
	// container that implements `focused` but can never answer should leave it
	// out: core takes the list as the set of questions there is any point
	// asking, so a method that is listed and useless costs a fork per question
	// and answers nothing.
	Methods []string `json:"methods"`

	// WakeOn names the record fields this integration cares about, and means
	// what it always meant (R23) — but it decides more now. It used to gate a
	// write to a socket somebody was already listening on; it gates a fork.
	// Empty means everything.
	WakeOn []string `json:"wake_on,omitempty"`

	// WantEnded asks for ended sessions in what core hands over. A bar says no
	// and a picker says yes (D-26), and a display that paints something it did
	// not create says yes so that it can give the pane back: the ended record
	// is the only thing that remembers which pane it was.
	WantEnded bool `json:"want_ended,omitempty"`
}

// Answers reports whether this integration said it answers that method.
//
// Asking is free and calling is a fork, which is the reason this exists: an
// unimplemented verb used to cost a process and come back indistinguishable
// from a real "I cannot tell".
func (c Capabilities) Answers(method string) bool { return slices.Contains(c.Methods, method) }

// Answer writes these capabilities as the answer to `capabilities` and returns
// the exit code, so that the case in every integration's main is one line.
//
// The version is stamped here, from the linked library, rather than taken from
// the value passed in.
func (c Capabilities) Answer(to io.Writer) int {
	c.Version = Version
	if c.Methods == nil {
		// An integration that answers no methods says so with an empty list
		// rather than with `null`: core reads this, and "none" is an answer
		// while "nothing" looks like a program that did not understand.
		c.Methods = []string{}
	}
	encoded, err := json.Marshal(c)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", CapabilitiesCommand, err)
		return 1
	}
	fmt.Fprintln(to, string(encoded))
	return 0
}
