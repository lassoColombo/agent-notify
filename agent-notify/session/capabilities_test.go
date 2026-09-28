package session_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lassoColombo/agent-notify/session"
)

// The handshake is a published interface — core runs somebody else's program
// and reads what it prints — so these tests are about the bytes on stdout and
// not only about the Go value that produced them.

func TestTheHandshakeIsOneLineOfJSONWithTheseKeys(t *testing.T) {
	var out bytes.Buffer
	code := session.Capabilities{
		Methods:   []string{session.MethodRender},
		WakeOn:    []string{"kernel"},
		WantEnded: true,
	}.Answer(&out)

	if code != 0 {
		t.Fatalf("answering capabilities exited %d", code)
	}
	if lines := strings.Count(out.String(), "\n"); lines != 1 {
		t.Errorf("the answer is %d lines, and core reads one", lines)
	}

	var keys map[string]json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &keys); err != nil {
		t.Fatalf("what was printed is not a JSON object: %v", err)
	}
	for _, key := range []string{"version", "methods", "wake_on", "want_ended"} {
		if _, present := keys[key]; !present {
			t.Errorf("%q is missing from the answer", key)
		}
	}
}

func TestTheVersionIsStampedAndNotTakenFromTheCaller(t *testing.T) {
	// An author who fills this in by hand fills it in wrong: it is the core
	// they linked, which only the linked core knows.
	var out bytes.Buffer
	session.Capabilities{Version: "whatever I felt like"}.Answer(&out)

	var answered session.Capabilities
	if err := json.Unmarshal(out.Bytes(), &answered); err != nil {
		t.Fatal(err)
	}
	if answered.Version != session.Version {
		t.Errorf("answered version %q, want %q", answered.Version, session.Version)
	}
}

func TestAnsweringNoMethodsIsAnEmptyListAndNotNull(t *testing.T) {
	// "None" is an answer and core acts on it. `null` reads as a program that
	// did not understand the question, which is a different thing entirely.
	var out bytes.Buffer
	session.Capabilities{}.Answer(&out)

	if got := strings.TrimSpace(out.String()); !strings.Contains(got, `"methods":[]`) {
		t.Errorf("answered %s, want an empty methods list", got)
	}
}
