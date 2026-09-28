package container

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/lassoColombo/agent-notify/session"
)

// What a container declares is derived from which functions it filled in, and
// these say so — because the alternative, a list written down beside the
// functions, is a list that goes stale the first time somebody adds a verb.

func TestAContainerDeclaresTheVerbsItImplemented(t *testing.T) {
	all := Integration{
		Name:      "zellij-container",
		Interpret: func(json.RawMessage) (any, error) { return nil, nil },
		Focus:     func(json.RawMessage) (Outcome, error) { return Done(), nil },
		Focused:   func(json.RawMessage) (Verdict, error) { return Verdict{}, nil },
	}

	want := []string{session.MethodInterpret, session.MethodFocus, session.MethodFocused}
	if got := all.capabilities().Methods; !slices.Equal(got, want) {
		t.Errorf("declared %v, want %v", got, want)
	}
}

func TestAContainerThatCannotSayWhatIsInFrontDoesNotDeclareFocused(t *testing.T) {
	// It would still ANSWER — with "cannot say", which is a real answer (R27) —
	// and that is exactly why it must not be asked: the fork is spent to be
	// told nothing, and the nothing is indistinguishable from a tool that
	// genuinely could not see.
	placer := Integration{
		Name:      "aerospace-container",
		Interpret: func(json.RawMessage) (any, error) { return nil, nil },
		Focus:     func(json.RawMessage) (Outcome, error) { return Done(), nil },
	}

	declared := placer.capabilities()
	if declared.Answers(session.MethodFocused) {
		t.Error("declared focused without a Focused")
	}
	if !declared.Answers(session.MethodFocus) {
		t.Error("did not declare focus")
	}
}

func TestCaptureIsNotAMethodAnybodyDeclares(t *testing.T) {
	// Every integration is asked it and none of them declares it: an
	// integration that reads nothing answers an empty object. A container with
	// a Capture and nothing else therefore declares nothing at all.
	reader := Integration{Name: "reads-only", Capture: func() (any, error) { return nil, nil }}

	if methods := reader.capabilities().Methods; len(methods) != 0 {
		t.Errorf("declared %v, want nothing", methods)
	}
}
