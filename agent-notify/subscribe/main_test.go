package subscribe_test

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/lassoColombo/agent-notify/session"
	"github.com/lassoColombo/agent-notify/subscribe"
)

// TestNothingToPlaceIsPrintedAsNull: a container with nothing to place a
// session by answers nil, and core reads `null` on stdout with exit 0 as that
// answer. Before D-135 the nil fell through to the usage and exited 1, which
// core read as a container that failed.
func TestNothingToPlaceIsPrintedAsNull(t *testing.T) {
	handed, _ := json.Marshal(session.CapturedContext{By: map[string]json.RawMessage{"placer": json.RawMessage(`{}`)}})
	code, printed := mainWith(t, string(handed), subscribe.Integration{Name: "placer"}, subscribe.Commands{
		Interpret: func(json.RawMessage, []session.Ancestor) (any, error) { return nil, nil },
	}, session.MethodInterpret)
	if code != 0 || strings.TrimSpace(printed) != "null" {
		t.Errorf("exit %d, printed %q; want exit 0 and null", code, printed)
	}
}

// mainWith runs Main with stdin given and stdout read back.
func mainWith(t *testing.T, stdin string, i subscribe.Integration, commands subscribe.Commands,
	arguments ...string) (int, string) {
	t.Helper()
	in, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := in.WriteString(stdin); err != nil {
		t.Fatal(err)
	}
	if _, err := in.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	reading, writing, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	wasIn, wasOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = in, writing
	defer func() { os.Stdin, os.Stdout = wasIn, wasOut }()

	code := subscribe.Main(i, commands, arguments)
	writing.Close()
	printed, _ := io.ReadAll(reading)
	return code, string(printed)
}
