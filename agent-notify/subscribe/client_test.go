package subscribe_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lassoColombo/agent-notify/session"
	"github.com/lassoColombo/agent-notify/subscribe"
)

// A display that owns its process reads `agent-notify tail --json`, so what is
// under test is a program's stdout and not a Go call: these stand a script up
// in place of the CLI and read what comes back out of it.

// pretendToBeTheCLI writes a script that answers `tail` with these lines, and
// points the configuration at it.
func pretendToBeTheCLI(t *testing.T, lines ...string) string {
	t.Helper()
	root := t.TempDir()
	core := filepath.Join(root, "agent-notify")

	var printing strings.Builder
	for _, line := range lines {
		fmt.Fprintf(&printing, "printf '%%s\\n' '%s'\n", line)
	}
	script := "#!/bin/sh\necho \"$@\" >> " + filepath.Join(root, "asked") + "\n" + printing.String()
	if err := os.WriteFile(core, []byte(script), 0o700); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "config.toml"),
		[]byte(fmt.Sprintf("agent-notify-binary = %q\n", core)), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return root
}

func TestEveryViewTailPrintsReachesTheDisplay(t *testing.T) {
	root := pretendToBeTheCLI(t,
		`{"sessions":[{"key":{"host":"mac","agent":"claude","session_id":"a"},"kernel":"working"}]}`,
		`{"sessions":[],"changed":[{"record":{"key":{"host":"mac","agent":"claude","session_id":"a"},"kernel":"finished-a-turn"},"previous_kernel":"working"}]}`)

	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	var seen []session.View
	var holding sync.Mutex
	go func() {
		_ = subscribe.RunThroughTheCLI(ctx, subscribe.Integration{
			Name: "bar", Root: root,
			OnChange: func(view session.View) error {
				holding.Lock()
				defer holding.Unlock()
				seen = append(seen, view)
				return nil
			},
		})
	}()

	waitUntil(t, "both views to arrive", func() bool {
		holding.Lock()
		defer holding.Unlock()
		return len(seen) >= 2
	})
	stop()

	holding.Lock()
	defer holding.Unlock()
	if len(seen[0].Changed) != 0 || len(seen[0].Sessions) != 1 {
		t.Errorf("the first view is %+v, want the world arriving with its one session", seen[0])
	}
	// The whole view, not only what changed: a bar draws every row, and the
	// ordering is a property of the set rather than of any one change.
	if len(seen[1].Changed) != 1 {
		t.Errorf("the second view is %+v, want its one change", seen[1])
	}
	if got := seen[1].Changed[0].PreviousKernel; got != session.Working {
		t.Errorf("previous kernel = %q, and a notifier reads nothing else", got)
	}
}

func TestWhatItWakesForIsWhatTailIsAskedFor(t *testing.T) {
	root := pretendToBeTheCLI(t, `{"sessions":[]}`)

	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	arrived := make(chan struct{}, 1)
	go func() {
		_ = subscribe.RunThroughTheCLI(ctx, subscribe.Integration{
			Name: "bar", Root: root,
			WakeOn:    []string{"kernel", "rank"},
			WantEnded: true,
			OnChange: func(session.View) error {
				select {
				case arrived <- struct{}{}:
				default:
				}
				return nil
			},
		})
	}()

	select {
	case <-arrived:
	case <-time.After(10 * time.Second):
		t.Fatal("no view ever arrived")
	}
	stop()

	asked, err := os.ReadFile(filepath.Join(root, "asked"))
	if err != nil {
		t.Fatalf("the CLI was never run: %v", err)
	}
	for _, want := range []string{"tail", "--json", "--wake-on kernel,rank", "--all"} {
		if !strings.Contains(string(asked), want) {
			t.Errorf("tail was asked %q, which is missing %q", strings.TrimSpace(string(asked)), want)
		}
	}
}

// TestTailIsStartedAgainWhenItStops: a display outlives the session-watcher,
// and a person who restarts one does not expect to restart the other.
func TestTailIsStartedAgainWhenItStops(t *testing.T) {
	// The script prints one view and exits, so every run is a restart.
	root := pretendToBeTheCLI(t, `{"sessions":[]}`)

	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	var runs int
	var holding sync.Mutex
	go func() {
		_ = subscribe.RunThroughTheCLI(ctx, subscribe.Integration{
			Name: "bar", Root: root,
			OnChange: func(session.View) error {
				holding.Lock()
				defer holding.Unlock()
				runs++
				return nil
			},
		})
	}()

	waitUntil(t, "tail to be started again", func() bool {
		holding.Lock()
		defer holding.Unlock()
		return runs >= 3
	})
}

// TestAWakeOnNameThatIsNotAFieldIsRefusedBeforeAnythingRuns is D-70 on this
// path too: a name that matches no field matches nothing for ever, and the
// display would simply never be woken.
func TestAWakeOnNameThatIsNotAFieldIsRefusedBeforeAnythingRuns(t *testing.T) {
	root := pretendToBeTheCLI(t, `{"sessions":[]}`)

	err := subscribe.RunThroughTheCLI(context.Background(), subscribe.Integration{
		Name: "bar", Root: root,
		WakeOn:   []string{"kernal"},
		OnChange: func(session.View) error { return nil },
	})
	if err == nil || !strings.Contains(err.Error(), "kernal") {
		t.Errorf("err = %v, want a refusal naming the misspelling", err)
	}
}

func waitUntil(t *testing.T, what string, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("waited for %s and it never happened", what)
}
