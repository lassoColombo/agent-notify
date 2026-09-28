package capture_test

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/lassoColombo/agent-notify/capture"
)

// whatItPrinted runs something with stdout redirected, because the contract
// this package keeps is about what core reads and not about what the function
// returned.
func whatItPrinted(t *testing.T, run func() int) (string, int) {
	t.Helper()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	was := os.Stdout
	os.Stdout = write
	code := run()
	os.Stdout = was
	write.Close()

	printed, err := io.ReadAll(read)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(printed)), code
}

func TestReadingNothingIsAnAnswerAndNotAFailure(t *testing.T) {
	// Every integration is asked this one now, so an integration with nothing
	// to read is the ordinary case rather than a misconfiguration. It answers
	// `{}`, which core stores as no entry at all — and exits 0, so that the log
	// is not filled with a complaint about a program working as intended.
	printed, code := whatItPrinted(t, func() int { return capture.Main("macos-bar", nil) })

	if code != 0 {
		t.Errorf("exited %d, want 0", code)
	}
	if printed != "{}" {
		t.Errorf("printed %q, want {}", printed)
	}
}

func TestWhatAnIntegrationReadsIsPrintedVerbatim(t *testing.T) {
	printed, code := whatItPrinted(t, func() int {
		return capture.Main("zellij-display", func() (any, error) {
			return map[string]string{"ZELLIJ_PANE_ID": "7"}, nil
		})
	})

	if code != 0 {
		t.Errorf("exited %d, want 0", code)
	}
	if printed != `{"ZELLIJ_PANE_ID":"7"}` {
		t.Errorf("printed %q", printed)
	}
}

func TestAnIntegrationThatFailsSaysSoWithACode(t *testing.T) {
	// Failing stays a failure: the empty answer above is for having nothing to
	// say, not for being unable to say it.
	printed, code := whatItPrinted(t, func() int {
		return capture.Main("zellij-display", func() (any, error) { return nil, os.ErrNotExist })
	})

	if code == 0 {
		t.Error("a capture that failed exited 0")
	}
	if printed != "" {
		t.Errorf("printed %q on stdout, and core would read it as an answer", printed)
	}
}
