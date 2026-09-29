package session

// The handshake: what core asks a tool-integration about itself, before it
// runs anything else. A subcommand rather than a key in the config, because a
// second copy of what the source already knows is a copy that can disagree
// with it, silently (D-57, D-81).

// CapabilitiesCommand is the subcommand every tool-integration answers.
const CapabilitiesCommand = "capabilities"

// CaptureCommand is the subcommand core runs on every integration it can run
// at all, from inside the agent (D-27). Nobody declares it; one that reads
// nothing answers an empty object.
const CaptureCommand = "capture-environment"

// The methods an integration can claim. They ARE the subcommand names: what a
// program says it answers is what core will run.
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
	// rather than a protocol to arbitrate (D-77). The SDK stamps it from the
	// linked library, so nobody can get it wrong.
	Version string `json:"version"`

	// Methods is what is worth calling, and not what will not error. A
	// container that implements `focused` but can never answer should leave it
	// out: core takes the list as the set of questions there is any point
	// asking, so a method that is listed and useless costs a fork per question
	// and answers nothing.
	Methods []string `json:"methods"`

	// WakeOn names the record fields this integration cares about (R23); it
	// gates a render. Empty means everything but the stamps.
	WakeOn []string `json:"wake_on,omitempty"`

	// WantEnded asks for ended sessions in what core hands over. A bar says no
	// and a picker says yes (D-26), and a display that paints something it did
	// not create says yes so that it can give the pane back: the ended record
	// is the only thing that remembers which pane it was.
	WantEnded bool `json:"want_ended,omitempty"`
}
