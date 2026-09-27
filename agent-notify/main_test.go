package main_test

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/lassoColombo/agent-notify/container"
	"github.com/lassoColombo/agent-notify/session"
)

// M6's "done when": a shell script pretending to be an agent produces records
// that list renders correctly, including one whose process it killed.
//
// Nothing here is a unit test. It builds the real binary, runs it from inside a
// process tree shaped like the real one, and reads what a person would read.

var (
	buildOnce sync.Once
	builtPath string
	buildErr  error
)

// binary builds agent-notify once for the whole test run.
func binary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("/tmp", "an-build")
		if err != nil {
			buildErr = err
			return
		}
		builtPath = filepath.Join(dir, "agent-notify")
		build := exec.Command("go", "build", "-o", builtPath, ".")
		// The toolchain is pinned by .tool-versions, and an inherited GOROOT
		// beats the pin — see the README.
		build.Env = append(os.Environ(), "GOROOT=", "GOPATH=")
		if output, err := build.CombinedOutput(); err != nil {
			buildErr = &buildFailure{err: err, output: string(output)}
		}
	})
	if buildErr != nil {
		t.Fatalf("building agent-notify: %v", buildErr)
	}
	return builtPath
}

type buildFailure struct {
	err    error
	output string
}

func (b *buildFailure) Error() string { return b.err.Error() + "\n" + b.output }

// pretendAgent is a process tree shaped like the real one:
//
//	fake-agent -> /bin/sh <script> -> agent-notify report-event
//
// fake-agent is a copy of this test binary re-entered in an "agent" role. Two
// approaches that look easier do not work, and both were tried:
//
//   - A symlink to /bin/sh named fake-agent. [verified 2026-09-17] The kernel
//     resolves the link before setting p_comm, so the process reports "bash" —
//     which is also what /bin/sh is on macOS.
//   - A copy of /bin/sh. macOS refuses to execute an unsigned copy of a system
//     binary. A copy of a Go binary is fine: its ad-hoc signature covers the
//     bytes, and the bytes are what was copied.
//
// A shell is still in the middle, which matters: it is the shape a real hook
// climbs, and climbing past it is the thing §A8.4 exists to do.
const (
	roleEnv    = "AGENT_NOTIFY_TEST_ROLE"
	scriptEnv  = "AGENT_NOTIFY_TEST_SCRIPT"
	binaryEnv  = "AGENT_NOTIFY_TEST_BINARY"
	sessionEnv = "AGENT_NOTIFY_TEST_SESSION"
)

func TestMain(m *testing.M) {
	// By name rather than by an environment variable, because a container is
	// spawned as a child of the hook and inherits the agent's whole environment
	// — including the variable that says "be the agent", which would make it
	// one.
	if filepath.Base(os.Args[0]) == "fake-container" {
		os.Exit(pretendToBeAContainer())
	}
	if os.Getenv(roleEnv) == "agent" {
		os.Exit(pretendToBeAnAgent())
	}
	code := m.Run()
	// The build is shared by every test in this package, so no single test can
	// own its cleanup with t.Cleanup — and without this, every run of the suite
	// leaves a copy of agent-notify in /tmp for ever.
	if builtPath != "" {
		os.RemoveAll(filepath.Dir(builtPath))
	}
	os.Exit(code)
}

// pretendToBeAContainer is a container-integration in six lines, through the
// real SDK.
//
// It answers `capture-environment` out of its own environment, which is the
// only way anything is captured since D-57 — and it is a child of the hook,
// which is the only process that can see an agent's environment at all.
func pretendToBeAContainer() int {
	return container.Main(container.Integration{
		Name: "paneish",
		Capture: func() (any, error) {
			return map[string]string{"FAKE_PANE": os.Getenv("FAKE_PANE")}, nil
		},
	})
}

// pretendToBeAnAgent runs the session script, then stays alive taking further
// commands on stdin.
//
// Staying alive is not decoration: an agent that exited would prove nothing
// about liveness. Taking commands is what lets a test run a hook from *inside*
// the agent, which is where one runs and the only place the ancestry walk has
// anything to find (§A8.4).
func pretendToBeAnAgent() int {
	session := exec.Command("/bin/sh", os.Getenv(scriptEnv),
		os.Getenv(binaryEnv), os.Getenv(sessionEnv))
	session.Stdout, session.Stderr = os.Stdout, os.Stderr
	if err := session.Run(); err != nil {
		os.Stderr.WriteString("the session script failed: " + err.Error() + "\n")
		return 1
	}
	os.Stdout.WriteString("ready\n")

	waiting := bufio.NewScanner(os.Stdin)
	for waiting.Scan() {
		inside := exec.Command("/bin/sh", "-c", waiting.Text())
		inside.Stdout, inside.Stderr = os.Stdout, os.Stderr
		_ = inside.Run()
		os.Stdout.WriteString("done\n")
	}
	time.Sleep(10 * time.Minute)
	return 0
}

type pretendAgent struct {
	root    string
	binary  string
	session string
	process *exec.Cmd
	tell    io.WriteCloser
	said    chan string
}

func start(t *testing.T) *pretendAgent {
	t.Helper()
	return startWith(t, "")
}

// startWith is start with extra configuration, so that a test can make the
// sweep slow enough that only the exit watch could explain a fast answer.
func startWith(t *testing.T, extraConfig string) *pretendAgent {
	t.Helper()
	built := binary(t)

	root, err := os.MkdirTemp("/tmp", "an-m6")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })

	containerPath := filepath.Join(root, "fake-container")
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte(extraConfig+`
[agent.fake]
binary = "fake-agent"

[integration.paneish]
binary              = "`+containerPath+`"
capture-environment = true
`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	agentPath := filepath.Join(root, "fake-agent")
	copyExecutable(t, os.Args[0], agentPath)
	copyExecutable(t, os.Args[0], containerPath)

	script := filepath.Join(root, "session.sh")
	if err := os.WriteFile(script, []byte(`
set -e
AN="$1"
SESSION="$2"
"$AN" report-event --agent fake --session "$SESSION" --event session-started --no-message
"$AN" report-event --agent fake --session "$SESSION" --event user-sent-prompt --name "the-store" --message "refactor the store"
"$AN" report-event --agent fake --session "$SESSION" --event agent-progressed --detail running-tool --message "reading store.go"
"$AN" report-event --agent fake --session "$SESSION" --event blocked-on-human --detail permission-prompt --message "may I edit store.go?"
`), 0o700); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	pretend := &pretendAgent{root: root, binary: built, session: "session-one"}
	agent := exec.Command(agentPath)
	agent.Dir = root
	agent.Env = append(os.Environ(),
		"AGENT_NOTIFY_ROOT="+root,
		"FAKE_PANE=%7",
		roleEnv+"=agent",
		scriptEnv+"="+script,
		binaryEnv+"="+built,
		sessionEnv+"="+pretend.session,
	)
	// Its own process group, so that killing it takes the shell with it.
	agent.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	output, err := agent.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	tell, err := agent.StdinPipe()
	if err != nil {
		t.Fatalf("StdinPipe: %v", err)
	}
	var complaints strings.Builder
	agent.Stderr = &complaints
	if err := agent.Start(); err != nil {
		t.Fatalf("starting the pretend agent: %v", err)
	}
	pretend.process = agent
	pretend.tell = tell
	pretend.said = make(chan string, 64)
	t.Cleanup(func() {
		pretend.kill()
		// A hook starts a session-watcher when its poke finds nobody, so a
		// test that fires hooks leaves one running against its temporary root.
		_, _, _ = pretend.run(t, "watcher", "stop")
	})

	go func() {
		defer close(pretend.said)
		scanner := bufio.NewScanner(output)
		for scanner.Scan() {
			pretend.said <- strings.TrimSpace(scanner.Text())
		}
	}()

	pretend.await(t, "ready", &complaints)
	return pretend
}

// await waits for one of the pretend agent's markers.
func (p *pretendAgent) await(t *testing.T, marker string, complaints *strings.Builder) {
	t.Helper()
	for {
		select {
		case line, open := <-p.said:
			if !open {
				t.Fatalf("the pretend agent stopped before saying %q: %s", marker, complaints.String())
			}
			if line == marker {
				return
			}
		case <-time.After(30 * time.Second):
			t.Fatalf("the pretend agent never said %q", marker)
		}
	}
}

// inside runs a command as a child of the pretend agent, which is the only way
// to exercise anything that resolves the session it is running in.
func (p *pretendAgent) inside(t *testing.T, command string) {
	t.Helper()
	if _, err := io.WriteString(p.tell, command+"\n"); err != nil {
		t.Fatalf("telling the pretend agent to run %q: %v", command, err)
	}
	p.await(t, "done", &strings.Builder{})
}

// copyExecutable copies the bytes and the mode. An ad-hoc code signature covers
// the bytes, so the copy runs.
func copyExecutable(t *testing.T, from, to string) {
	t.Helper()
	content, err := os.ReadFile(from)
	if err != nil {
		t.Fatalf("reading %s: %v", from, err)
	}
	if err := os.WriteFile(to, content, 0o700); err != nil {
		t.Fatalf("writing %s: %v", to, err)
	}
}

func (p *pretendAgent) kill() {
	if p.process == nil || p.process.Process == nil {
		return
	}
	_ = syscall.Kill(-p.process.Process.Pid, syscall.SIGKILL)
	_, _ = p.process.Process.Wait()
}

// run invokes the built binary against this test's root, the way a person would.
func (p *pretendAgent) run(t *testing.T, arguments ...string) (stdout, stderr string, code int) {
	t.Helper()
	command := exec.Command(p.binary, arguments...)
	command.Env = append(os.Environ(), "AGENT_NOTIFY_ROOT="+p.root)
	var out, errs strings.Builder
	command.Stdout, command.Stderr = &out, &errs
	if err := command.Run(); err != nil {
		var exit *exec.ExitError
		if !errorAs(err, &exit) {
			t.Fatalf("running %v: %v", arguments, err)
		}
		code = exit.ExitCode()
	}
	return out.String(), errs.String(), code
}

func errorAs(err error, target **exec.ExitError) bool {
	exit, ok := err.(*exec.ExitError)
	if ok {
		*target = exit
	}
	return ok
}

func TestAPretendAgentProducesRecordsListCanRender(t *testing.T) {
	pretend := start(t)

	stdout, stderr, code := pretend.run(t, "list")
	if code != 0 {
		t.Fatalf("list exited %d\n%s", code, stderr)
	}
	t.Logf("list:\n%s", stdout)

	for _, want := range []string{"the-store", "blocked-on-you/permission-prompt", "may I edit store.go?"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("list does not show %q:\n%s", want, stdout)
		}
	}

	records := pretend.records(t)
	if len(records) != 1 {
		t.Fatalf("list --json returned %d records, want 1", len(records))
	}
	record := records[0]

	if record.Kernel != session.BlockedOnYou {
		t.Errorf("kernel = %q, want blocked-on-you", record.Kernel)
	}
	if record.Rank != session.RankBlockedOnYou {
		t.Errorf("rank = %d, want %d", record.Rank, session.RankBlockedOnYou)
	}
	if record.Name != "the-store" {
		t.Errorf("name = %q — the name the event carried was not kept", record.Name)
	}
	if record.Sequence != 4 {
		t.Errorf("sequence = %d, want 4: one per event", record.Sequence)
	}
	if record.Key.Agent != "fake" || record.Key.SessionID != pretend.session {
		t.Errorf("key = %+v", record.Key)
	}

	// The ancestry walk found the process wearing the configured binary name.
	if record.Process.PID != pretend.process.Process.Pid {
		t.Errorf("process.pid = %d, want the pretend agent's %d",
			record.Process.PID, pretend.process.Process.Pid)
	}
	if record.Process.StartedAt.IsZero() {
		t.Error("process.started_at is empty, so the pid means nothing")
	}
	if record.Process.BootID == "" {
		t.Error("process.boot_id is empty, so a reboot would leave this as a ghost")
	}

	// The container's `capture-environment` ran as a child of the hook, which
	// is the only place an agent's environment can be read from.
	captured, ok := record.CapturedContext.By["paneish"]
	if !ok {
		t.Fatalf("nothing was captured for the configured container: %+v", record.CapturedContext)
	}
	var wanted map[string]string
	if err := json.Unmarshal(captured, &wanted); err != nil {
		t.Fatalf("the capture is not JSON: %v", err)
	}
	if wanted["FAKE_PANE"] != "%7" {
		t.Errorf("captured %v, want FAKE_PANE=%%7", wanted)
	}
	if len(record.CapturedContext.Ancestry) < 2 {
		t.Errorf("the ancestry is %d deep, want at least the hook and its agent: %+v",
			len(record.CapturedContext.Ancestry), record.CapturedContext.Ancestry)
	}

	// StateSince is the block, not the naming that happened before it.
	if !record.StateSince.Equal(record.UpdatedAt) {
		t.Errorf("state_since %v and updated_at %v: the last write was the block itself",
			record.StateSince, record.UpdatedAt)
	}
}

// TestARenameDoesNotResetTheClock is §A7.4.2 in the real binary: a name is a
// label and not a state, so it may arrive on an event that asserts nothing and
// "waiting 12m" has to survive it.
func TestARenameDoesNotResetTheClock(t *testing.T) {
	pretend := start(t)

	before := pretend.records(t)[0]
	time.Sleep(20 * time.Millisecond)
	pretend.inside(t, `"`+pretend.binary+`" report-event --agent fake --session `+
		pretend.session+` --event context-changed --name renamed-on-purpose --no-message`)

	after := pretend.records(t)[0]
	if after.Name != "renamed-on-purpose" {
		t.Errorf("name = %q", after.Name)
	}
	if !after.StateSince.Equal(before.StateSince) {
		t.Errorf("state_since moved from %v to %v", before.StateSince, after.StateSince)
	}
	if after.Sequence != before.Sequence+1 {
		t.Errorf("sequence = %d, want %d", after.Sequence, before.Sequence+1)
	}
	if after.Kernel != before.Kernel {
		t.Errorf("kernel moved from %q to %q", before.Kernel, after.Kernel)
	}
}

// TestAnEventCarryingNoNameLeavesTheNameAlone is the rule that replaces
// --if-unnamed: a hook that does not know the name must not erase the one a
// hook that did know it established. Every hook after the first carries it
// anyway, so the only way this shows up is an agent that has no name yet.
func TestAnEventCarryingNoNameLeavesTheNameAlone(t *testing.T) {
	pretend := start(t)
	pretend.inside(t, `"`+pretend.binary+`" report-event --agent fake --session `+
		pretend.session+` --event agent-progressed --no-message`)
	if got := pretend.records(t)[0].Name; got != "the-store" {
		t.Errorf("name = %q, want the name it already had", got)
	}
}

// TestAFlagAfterTheSessionIsStillAFlag is an accommodation for how a person
// writes it: `focus-session <session> --quiet`, and Go's flag package stops at
// the first argument that is not a flag. Without the partition this exits 2
// with a usage message, having done nothing — which reads as "that session
// does not exist".
func TestAFlagAfterTheSessionIsStillAFlag(t *testing.T) {
	pretend := start(t)
	stdout, stderr, code := pretend.run(t, "focus-session", pretend.session, "--quiet")
	if code == 2 || strings.Contains(stderr, "focus-session <session>") {
		t.Fatalf("the flag after the session was read as a second session:\n%s%s", stdout, stderr)
	}
}

// TestAKilledAgentIsRenderedAsEnded is the other half of M6's "done when", and
// the reason the whole liveness layer exists: nothing will ever fire a hook for
// this session again.
//
// Since M8 there may or may not be a session-watcher running by the time this
// looks, and both outcomes are correct: with one, the record has been filed
// into ended/ and a plain `list` rightly stops showing it (D-26); without one,
// `list` judges it dead as it reads and renders it as ended. What must never
// happen is its still reading `blocked-on-you`.
func TestAKilledAgentIsRenderedAsEnded(t *testing.T) {
	pretend := start(t)
	if got := pretend.records(t)[0].Kernel; got != session.BlockedOnYou {
		t.Fatalf("kernel = %q before the kill", got)
	}

	pretend.kill()

	stdout, _, code := pretend.run(t, "list", "--all")
	if code != 0 {
		t.Fatalf("list exited %d", code)
	}
	t.Logf("list --all after the kill:\n%s", stdout)

	records := pretend.recordsAll(t)
	if len(records) != 1 {
		t.Fatalf("list --all returned %d records, want 1", len(records))
	}
	record := records[0]
	if record.Kernel != session.Ended {
		t.Errorf("kernel = %q, want ended: its process is gone", record.Kernel)
	}
	if record.Detail != "process-gone" {
		t.Errorf("detail = %q, want process-gone", record.Detail)
	}
	if !strings.Contains(stdout, "ended/process-gone") {
		t.Errorf("list does not show it as ended:\n%s", stdout)
	}

	// A plain list shows what is live, and nothing that has ended is.
	plain, _, _ := pretend.run(t, "list")
	if strings.Contains(plain, "the-store") {
		t.Errorf("a session that has ended is still on the default list:\n%s", plain)
	}
}

// TestTheHookNeverFailsAndNeverSpeaks is R2, checked against the real binary
// rather than against the intention.
func TestTheHookNeverFailsAndNeverSpeaks(t *testing.T) {
	pretend := start(t)

	attempts := [][]string{
		{"report-event"},
		{"report-event", "--agent", "fake"},
		{"report-event", "--agent", "fake", "--session", "x", "--event", "not-an-event"},
		{"report-event", "--agent", "fake", "--session", "", "--event", "turn-finished"},
		{"report-event", "--nonsense"},
	}
	for _, attempt := range attempts {
		stdout, stderr, code := pretend.run(t, attempt...)
		if code != 0 {
			t.Errorf("%v exited %d — an agent reads that as an instruction (R2)", attempt, code)
		}
		if stdout != "" {
			t.Errorf("%v wrote to stdout, which Claude reads into the session: %q", attempt, stdout)
		}
		if stderr != "" {
			t.Errorf("%v wrote to stderr: %q", attempt, stderr)
		}
	}

	// The complaints went to the log, which is where R2 says they go.
	log, err := os.ReadFile(filepath.Join(pretend.root, "state", "agent-notify.log"))
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	if !strings.Contains(string(log), "component=record-agent-event") {
		t.Errorf("nothing from record-agent-event reached the log:\n%s", log)
	}
	for _, want := range []string{"are all required", "not-an-event"} {
		if !strings.Contains(string(log), want) {
			t.Errorf("the log does not explain %q:\n%s", want, log)
		}
	}
}

func (p *pretendAgent) recordsAll(t *testing.T) []session.Record {
	t.Helper()
	return p.listed(t, "list", "--json", "--all")
}

func (p *pretendAgent) records(t *testing.T) []session.Record {
	t.Helper()
	return p.listed(t, "list", "--json")
}

func (p *pretendAgent) listed(t *testing.T, arguments ...string) []session.Record {
	t.Helper()
	stdout, stderr, code := p.run(t, arguments...)
	if code != 0 {
		t.Fatalf("list --json exited %d: %s", code, stderr)
	}
	var records []session.Record
	if err := json.Unmarshal([]byte(stdout), &records); err != nil {
		t.Fatalf("list --json is not JSON: %v\n%s", err, stdout)
	}
	return records
}
