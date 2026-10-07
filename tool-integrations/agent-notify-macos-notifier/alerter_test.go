package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/lassoColombo/agent-notify/subscribe"
)

// theHoldToRun is how a test asks this binary to be the detached half instead
// of a test binary. `hold` is a process in production — that is the whole
// point of it — and the rule it keeps is about what a SEPARATE process does
// with a word on a pipe, so it is tested as one.
const theHoldToRun = "AGENT_NOTIFY_TEST_HOLD"

func TestMain(m *testing.M) {
	if os.Getenv(theHoldToRun) != "" {
		os.Exit(hold(os.Args[1:]))
	}
	os.Exit(m.Run())
}

// stubAlerterOnPATH puts something called alerter on PATH, and answers with the
// directory. It is what lets this suite say the same thing on a machine that
// has never installed one — the thing three of sketchybar's tests did not do,
// and why a full sweep was red for a year (D-79).
func stubAlerterOnPATH(t *testing.T) string {
	t.Helper()
	return filepath.Dir(scriptCalled(t, "alerter", "exit 0"))
}

// scriptCalled writes an executable shell script and answers with its path.
func scriptCalled(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

// TestOneBannerPerSession, replaced rather than stacked (D-54). `--group` is
// the session's key and nothing else, because that is also what reaps the
// alerter holding the previous banner.
func TestTheArgumentsAreOneBannerPerSession(t *testing.T) {
	notice := Notice{Key: "a/b/c", Title: "lenny", Subtitle: "blocked on you", Body: "Shall I?"}
	got := theArgumentsFor(notice, "/tmp/love.png", Sound{Plays: true})

	for _, want := range [][2]string{
		{"--group", "a/b/c"},
		{"--title", "lenny"},
		{"--subtitle", "blocked on you"},
		{"--message", "Shall I?"},
		// The same file twice: the picture beside the text and the badge on it.
		{"--content-image", "/tmp/love.png"},
		{"--app-icon", "/tmp/love.png"},
		{"--sound", "default"},
	} {
		if at := slices.Index(got, want[0]); at < 0 || at+1 >= len(got) || got[at+1] != want[1] {
			t.Errorf("%v does not say %s %s", got, want[0], want[1])
		}
	}
}

// TestNothingIsSaidAboutASoundThatIsNotPlaying. alerter has no way to spell
// silence, so silence is the flag being absent.
func TestSilenceIsTheFlagBeingAbsent(t *testing.T) {
	got := theArgumentsFor(Notice{Key: "k"}, "", Sound{Plays: false})
	if slices.Contains(got, "--sound") {
		t.Errorf("%v asks for a sound when the config said not to", got)
	}
	if slices.Contains(got, "--content-image") {
		t.Errorf("%v attaches a picture there is no colour for", got)
	}
	named := theArgumentsFor(Notice{Key: "k"}, "", Sound{Plays: true, Name: "Submarine.aiff"})
	if at := slices.Index(named, "--sound"); at < 0 || named[at+1] != "Submarine.aiff" {
		t.Errorf("%v does not ask for the sound the config named", named)
	}
}

// TestOnlyATapFocusesTheSession, which is the one rule in this file that
// matters: alerter says one of four words and three of them are somebody
// DECLINING to be interrupted. Acting on those would take you to a session you
// had just dismissed, which is worse than doing nothing.
func TestOnlyATapFocusesTheSession(t *testing.T) {
	for _, one := range []struct {
		said    string
		focuses bool
	}{
		{tapped, true},
		{"@CLOSED", false},
		{"@TIMEOUT", false},
		{"@ACTIONCLICKED", false},
		{"", false},
	} {
		t.Run(one.said, func(t *testing.T) {
			root := t.TempDir()
			// Core, as far as the detached half is concerned: something that
			// records having been asked to focus anything.
			asked := filepath.Join(root, "asked")
			core := scriptCalled(t, "agent-notify", "printf '%s' \"$*\" > "+asked)
			writeConfig(t, root, "agent-notify-binary = "+quoted(core)+"\n")

			alerter := scriptCalled(t, "alerter", "printf '%s' "+quoted(one.said))
			held := exec.Command(os.Args[0], "the/session", alerter, "--group", "the/session")
			held.Env = append(os.Environ(), "AGENT_NOTIFY_ROOT="+root, theHoldToRun+"=yes")
			if output, err := held.CombinedOutput(); err != nil {
				t.Fatalf("hold: %v\n%s", err, output)
			}

			ran, err := os.ReadFile(asked)
			switch {
			case one.focuses && err != nil:
				t.Fatalf("%s did not focus anything: %v", one.said, err)
			case one.focuses && !(strings.Contains(string(ran), "focus-session") &&
				strings.Contains(string(ran), "the/session")):
				t.Errorf("%s ran %q, want focus-session on the banner's session", one.said, ran)
			case !one.focuses && err == nil:
				t.Errorf("%s focused %q, and nobody asked to go anywhere", one.said, ran)
			}
		})
	}
}

// TestHoldSaysWhatItNeeds rather than indexing past the end of its arguments.
func TestHoldRefusesTooFewArguments(t *testing.T) {
	if code := hold([]string{"only-a-session"}); code == 0 {
		t.Errorf("hold with no alerter exited %d, want a refusal", code)
	}
}

// TestAlerterIsCheckedRatherThanTrusted: a brew upgrade that moved it is a
// display that silently stops interrupting anybody.
func TestAnAlerterThatIsNotThereIsNamed(t *testing.T) {
	if _, err := Alerter("/nowhere/alerter"); err == nil ||
		!strings.Contains(err.Error(), "/nowhere/alerter") {
		t.Errorf("err = %v, want the written path named", err)
	}
	t.Setenv("PATH", stubAlerterOnPATH(t))
	found, err := Alerter("")
	if err != nil || filepath.Base(found) != alerterProgram {
		t.Errorf("Alerter(\"\") = %q, %v; want the one on PATH", found, err)
	}
}

// writeConfig puts a config file under a root of this test's own, so nothing
// here can reach the real one (D-69).
func writeConfig(t *testing.T, root, body string) {
	t.Helper()
	path, err := subscribe.Integration{Name: Name, Root: root}.ConfigFile()
	if err != nil {
		t.Fatalf("ConfigFile: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func quoted(value string) string { return strconv.Quote(value) }
