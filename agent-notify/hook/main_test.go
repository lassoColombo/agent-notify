package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lassoColombo/agent-notify/session"
)

type pretendPayload struct {
	SessionID string `json:"session_id"`
}

// onStdin puts this on the process's stdin, which is where a hook's payload
// arrives and the only place Main reads it from.
func onStdin(t *testing.T, content string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stdin")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	was := os.Stdin
	os.Stdin = file
	t.Cleanup(func() { os.Stdin = was; file.Close() })
}

// onStderr collects what the process says to stderr.
func onStderr(t *testing.T) func() string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stderr")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	was := os.Stderr
	os.Stderr = file
	t.Cleanup(func() { os.Stderr = was; file.Close() })
	return func() string {
		said, _ := os.ReadFile(path)
		return string(said)
	}
}

// TestMainDecodesAFreshPayloadAndHandsItOver is the shape D-97 gave Main:
// the adapter writes one translation of one payload and nothing else — no
// variable to declare, no pointer to pass, no closure reaching back for it.
func TestMainDecodesAFreshPayloadAndHandsItOver(t *testing.T) {
	onStdin(t, `{"session_id":"alpha","a_field_this_build_has_never_seen":true}`)
	var handed pretendPayload
	code := Main(nil, "usage\n", nil, func(payload pretendPayload) (session.Report, bool) {
		handed = payload
		return session.Report{}, false
	})
	if code != 0 || handed.SessionID != "alpha" {
		t.Errorf("exit %d, handed %+v; want 0 and the payload on stdin", code, handed)
	}
}

// TestAPayloadThatDoesNotParseIsStillTranslated, from the zero value: an
// agent sends a different shape per hook and adds fields between releases,
// and a hook never fails over it (R2).
func TestAPayloadThatDoesNotParseIsStillTranslated(t *testing.T) {
	onStdin(t, `this is not json`)
	translated := false
	code := Main(nil, "usage\n", nil, func(payload pretendPayload) (session.Report, bool) {
		translated = true
		return session.Report{}, false
	})
	if code != 0 || !translated {
		t.Errorf("exit %d, translated %v; want 0 and the translation run", code, translated)
	}
}

// TestAWordOnTheCommandLineIsASubcommandOrTheUsage: `install` is a person's
// command and may exit how it likes; anything else is refused with the usage
// and nothing is read from stdin.
func TestAWordOnTheCommandLineIsASubcommandOrTheUsage(t *testing.T) {
	var given []string
	named := map[string]func([]string) int{"install": func(arguments []string) int {
		given = arguments
		return 3
	}}
	translate := func(pretendPayload) (session.Report, bool) {
		t.Error("a word on the command line is not a hook, and the translation ran")
		return session.Report{}, false
	}

	if code := Main([]string{"install", "--print"}, "usage\n", named, translate); code != 3 || len(given) != 1 || given[0] != "--print" {
		t.Errorf("install exited %d with %v; want its own 3 and its own arguments", code, given)
	}

	said := onStderr(t)
	if code := Main([]string{"instal"}, "usage\n", named, translate); code != 1 || !strings.Contains(said(), "usage") {
		t.Errorf("an unknown word exited %d saying %q; want 1 and the usage", code, said())
	}
}
