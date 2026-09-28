// Package container is the vocabulary a container-integration answers in: a
// typed outcome for a focus and a three-valued answer for whether a session is
// in front. The functions themselves are filled into [subscribe.Commands].
package container

// Problem is why a focus did not happen, as a word rather than a sentence, so
// that a caller can act differently on each (§A11.3).
type Problem string

const (
	// PlaceIsGone — the pane, window or tab no longer exists.
	PlaceIsGone Problem = "place-is-gone"
	// NotRunning — the tool itself is not there to be asked.
	NotRunning Problem = "not-running"
	// NeverPlaced — this container has no coordinates for this session.
	NeverPlaced Problem = "never-placed"
	// NoContainer — nothing is configured to place anything, which is every
	// fresh install (§A11.7).
	NoContainer Problem = "no-container-configured"
	// Unreachable — the container was run and could not answer. Core
	// synthesises this one.
	Unreachable Problem = "container-unreachable"
	// Refused — the tool was asked, understood, and would not.
	Refused Problem = "refused"
	// Ambiguous — several places the session could be, and nothing to choose
	// between them.
	Ambiguous Problem = "ambiguous"
)

// Outcome is what came of a focus. An empty Problem with OK false is read as
// Refused.
type Outcome struct {
	OK      bool    `json:"ok"`
	Problem Problem `json:"problem,omitempty"`
	Detail  string  `json:"detail,omitempty"`
}

func Done() Outcome { return Outcome{OK: true} }

func Failed(problem Problem, detail string) Outcome {
	return Outcome{Problem: problem, Detail: detail}
}

// Answer is a three-valued yes (R27).
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
