package sessionstore_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/lassoColombo/agent-notify/internal/config"
	"github.com/lassoColombo/agent-notify/internal/paths"
	"github.com/lassoColombo/agent-notify/internal/sessionstore"
	"github.com/lassoColombo/agent-notify/session"
)

// The store is written by several short-lived processes at once — two hooks of
// one session firing together, or eight agents working at once — so what has to
// be proven is proven between processes, not between goroutines. A mutex in one
// address space would pass a test that says nothing about the system as it runs.
//
// TestMain gives this binary two extra jobs, chosen by environment, so that
// spawning a writer costs one exec and no build step.
const (
	roleEnv   = "AGENT_NOTIFY_TEST_ROLE"
	writesEnv = "AGENT_NOTIFY_TEST_WRITES"
	labelEnv  = "AGENT_NOTIFY_TEST_LABEL"
	stopEnv   = "AGENT_NOTIFY_TEST_STOP"
)

var contended = session.Key{Host: "mac", Agent: "claude", SessionID: "contended"}

func TestMain(m *testing.M) {
	switch os.Getenv(roleEnv) {
	case "writer":
		os.Exit(writer())
	case "reader":
		os.Exit(reader())
	default:
		os.Exit(m.Run())
	}
}

// openFromEnvironment is what a real short-lived writer does: resolve the
// layout from the environment and open the store. Nothing is handed in.
func openFromEnvironment() (*sessionstore.SessionStore, error) {
	layout, err := paths.FromEnvironment()
	if err != nil {
		return nil, err
	}
	return sessionstore.Open(layout, config.Defaults())
}

func writer() int {
	opened, err := openFromEnvironment()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	writes, _ := strconv.Atoi(os.Getenv(writesEnv))
	label := os.Getenv(labelEnv)

	for i := range writes {
		text := label + " " + strconv.Itoa(i)
		if _, err := opened.Apply(session.Report{
			Key: contended, Event: session.AgentProgressed, Message: &text,
		}, time.Now().UTC()); err != nil {
			fmt.Fprintf(os.Stderr, "%s write %d: %v\n", label, i, err)
			return 1
		}
	}
	return 0
}

// reader hammers the read path while the writers run and fails on anything a
// reader must never see: a record that does not parse, a sequence that went
// backwards, or a record belonging to another session.
func reader() int {
	opened, err := openFromEnvironment()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	stop := os.Getenv(stopEnv)

	var highest uint64
	var reads int
	for {
		if _, err := os.Stat(stop); err == nil {
			fmt.Fprintf(os.Stderr, "reader: %d clean reads, highest sequence %d\n", reads, highest)
			return 0
		}
		record, found, err := opened.Read(contended)
		if err != nil {
			fmt.Fprintf(os.Stderr, "torn read after %d reads: %v\n", reads, err)
			return 1
		}
		if !found {
			continue
		}
		reads++
		if record.Sequence < highest {
			fmt.Fprintf(os.Stderr, "sequence went backwards: %d after %d\n", record.Sequence, highest)
			return 1
		}
		if record.Key != contended {
			fmt.Fprintf(os.Stderr, "read a record for the wrong session: %v\n", record.Key)
			return 1
		}
		highest = record.Sequence
	}
}

// TestConcurrentWriterProcesses is M4's "done when".
//
// Six processes each write twenty-five events to one session, while a seventh
// reads without pause. If any two writers read the same record and both wrote,
// the final sequence falls short — so the count is an exact proof of no lost
// update rather than an approximate one.
func TestConcurrentWriterProcesses(t *testing.T) {
	const (
		writers        = 6
		writesPerChild = 25
	)

	root, err := os.MkdirTemp("/tmp", "an-race")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	t.Setenv(paths.TheVariableThatNamesTheRoot, root)
	stop := root + "/stop"

	opened, err := openFromEnvironment()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	watching := child("reader", "", 0, stop)
	var watched bytes.Buffer
	watching.Stdout, watching.Stderr = &watched, &watched
	if err := watching.Start(); err != nil {
		t.Fatalf("starting the reader: %v", err)
	}

	var running sync.WaitGroup
	failures := make(chan string, writers)
	for i := range writers {
		running.Add(1)
		go func() {
			defer running.Done()
			output, err := child("writer", "w"+strconv.Itoa(i), writesPerChild, stop).CombinedOutput()
			if err != nil {
				failures <- fmt.Sprintf("writer %d: %v\n%s", i, err, output)
			}
		}()
	}
	running.Wait()
	close(failures)
	for failure := range failures {
		t.Error(failure)
	}

	if err := os.WriteFile(stop, nil, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := watching.Wait(); err != nil {
		t.Errorf("the reader saw something a reader must never see: %v\n%s", err, watched.String())
	}
	t.Log(watched.String())

	record, found, err := opened.Read(contended)
	if err != nil || !found {
		t.Fatalf("Read: %v, found %v", err, found)
	}
	if want := uint64(writers * writesPerChild); record.Sequence != want {
		t.Errorf("sequence = %d, want %d — %d writes went missing, which means two processes "+
			"read the same record and one overwrote the other",
			record.Sequence, want, want-record.Sequence)
	}
	if record.Kernel != session.Working {
		t.Errorf("kernel = %q, want working", record.Kernel)
	}

	// The history file is written under the same lock and faced the same
	// contention; out-of-order entries would mean a writer wrote history from
	// a record it no longer held.
	history, err := opened.History(contended)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if want := config.Defaults().HistoryMessages; len(history.Messages) != want {
		t.Errorf("history kept %d messages, want the bound of %d", len(history.Messages), want)
	}
	for i := 1; i < len(history.Messages); i++ {
		if history.Messages[i].Sequence <= history.Messages[i-1].Sequence {
			t.Errorf("history is out of order at %d: sequence %d after %d",
				i, history.Messages[i].Sequence, history.Messages[i-1].Sequence)
		}
	}
}

// TestLockGivesUpRatherThanWaitingForever is R1 on the hook path: a writer that
// cannot have the lock abandons the write and lets the agent get on with it.
// Losing one event is cheap, because the next one re-establishes the state;
// hanging the agent is not.
func TestLockGivesUpRatherThanWaitingForever(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "an-busy")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	t.Setenv(paths.TheVariableThatNamesTheRoot, root)

	layout, err := paths.FromEnvironment()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	settings := config.Defaults()
	shortenTheLockWait(t, 50*time.Millisecond)
	opened, err := sessionstore.Open(layout, settings)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := opened.Apply(session.Report{Key: key, Event: session.SessionStarted}, when); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// The lock has to be held from another process: flock is owned by an open
	// file description, so a second flock from this process would simply
	// succeed and prove nothing.
	blocker := exec.Command(os.Args[0], "-test.run=TestHoldTheLock")
	blocker.Env = append(os.Environ(), "AGENT_NOTIFY_TEST_HOLD="+layout.LockFile(key.String()))
	if err := blocker.Start(); err != nil {
		t.Fatalf("starting the lock holder: %v", err)
	}
	t.Cleanup(func() { _ = blocker.Process.Kill() })

	// Retry until the holder actually has it, then time the attempt that fails.
	deadline := time.Now().Add(3 * time.Second)
	for {
		start := time.Now()
		_, err := opened.Apply(session.Report{Key: key, Event: session.AgentProgressed}, when)
		waited := time.Since(start)
		if err != nil {
			if waited > time.Second {
				t.Errorf("gave up after %v, want about the 50ms it was told to wait", waited)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the lock was never held: the test proved nothing")
		}
		// Let go long enough for the holder to get in. Retried back to back,
		// this side holds the lock nearly all the time, and a holder polling
		// every 20ms can miss it for seconds: on Linux it did, one run in two.
		time.Sleep(10 * time.Millisecond)
	}
}

// TestHoldTheLock is not a test. It is the lock holder the test above spawns,
// and it does nothing at all unless its environment says so.
func TestHoldTheLock(t *testing.T) {
	path := os.Getenv("AGENT_NOTIFY_TEST_HOLD")
	if path == "" {
		t.Skip("spawned only by TestLockGivesUpRatherThanWaitingForever")
	}
	layout, err := paths.FromEnvironment()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	settings := config.Defaults()
	shortenTheLockWait(t, 5*time.Second)
	opened, err := sessionstore.Open(layout, settings)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// Hold it by sitting inside an Update for longer than the other side is
	// willing to wait.
	if _, err := opened.Update(key, when, func(previous session.Record) session.Record {
		time.Sleep(3 * time.Second)
		return previous.Clone()
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
}

// child builds a re-execution of this binary in one of TestMain's roles.
func child(role, label string, writes int, stop string) *exec.Cmd {
	command := exec.Command(os.Args[0])
	command.Env = append(os.Environ(),
		roleEnv+"="+role,
		labelEnv+"="+label,
		writesEnv+"="+strconv.Itoa(writes),
		stopEnv+"="+stop,
	)
	return command
}

// shortenTheLockWait moves sessionstore.LockPatience for the length of one
// test. The wait is the thing these two tests are about, and two seconds of it
// is two seconds nobody needs to watch.
func shortenTheLockWait(t *testing.T, to time.Duration) {
	t.Helper()
	was := sessionstore.LockPatience
	sessionstore.LockPatience = to
	t.Cleanup(func() { sessionstore.LockPatience = was })
}
