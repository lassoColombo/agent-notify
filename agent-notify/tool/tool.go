// Package tool runs the external program an integration drives, under a
// timeout, and says what came of it.
//
// Every tool-integration has one of these: zellij, aerospace, sketchybar. The
// running of it is a dozen lines, four of which are easy to get wrong, and
// before this package there were five copies of them — one in core and one in
// each of four integrations — which had already drifted into disagreeing about
// what a failure is (D-68).
//
// What is easy to get wrong, and therefore what this does for you:
//
//   - **WaitDelay.** Without it the timeout is a lie. exec kills the process it
//     started, but Run does not return until the pipes handed to that process
//     are closed, and a grandchild inherits them. Measured in this project as a
//     200ms timeout that took 30 seconds.
//   - **The order of the checks.** A process the context killed reports an
//     ordinary exit error, so a caller that looks at the error before ctx.Err()
//     reports every timeout as "signal: killed" and nobody learns anything.
//   - **Telling "it is not there" from "it said no".** Both arrive as one
//     non-nil error and they mean opposite things: a broken installation
//     against an answer. Only one of the five copies drew the distinction, so
//     two containers reported a missing binary and a refused focus with the
//     same [container.Problem].
//   - **What a tool printed is not safe to pass on.** It is somebody else's
//     bytes on their way to a log, a status bar, and in one case back out to a
//     terminal. Summarise puts them through core's own cleaning (§A15) rather
//     than the hand-rolled ANSI strip the copies used, which knew about CSI and
//     let OSC — the sequence that can ask a terminal to do rather more than
//     change a colour — straight through.
//
// The three ways it can fail are types, so that a caller which acts
// differently on each can:
//
//	out, err := tool.Run(binary, 2*time.Second, "focus", "--window-id", "34")
//	var missing *tool.DidNotRun
//	if errors.As(err, &missing) {
//	    return container.Failed(container.NotRunning, err.Error()), nil
//	}
//
// There is no cancellation and no context argument. Every caller runs these on
// a path where the timeout is the bound that matters, and a second way to stop
// one is a second thing to get wrong.
package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/lassoColombo/agent-notify/session"
)

// FlushAfterKilling is how long a killed child is given to let go of the pipes
// before they are closed under it.
//
// Without it the timeout is a lie, and the way it lies is worth writing down:
// exec kills the process it started, but Run does not return until the pipes it
// handed that process are closed — and a grandchild inherits them. A container
// that shells out to something slow is therefore killed on time and waited for
// anyway. Measured: a 200ms timeout that took 30 seconds, which is exactly the
// failure R1 and R18 exist to prevent.
const FlushAfterKilling = 500 * time.Millisecond

// Output is everything one invocation produced.
//
// It is filled in whether the invocation succeeded or not, because a caller
// that wants to read a failure rather than propagate it needs to see what the
// tool actually said. sketchybar is the reason: it reports a bad message by
// complaining on stderr and exiting non-zero, and the complaint is the answer.
type Output struct {
	Stdout []byte
	Stderr []byte
}

// Run runs one program with these arguments and waits for it.
//
// A non-zero exit is an error rather than a field on Output, and deliberately:
// a caller that forgets to check a status code has a tool that failed and looks
// like it worked, which is a worse mistake than the one this package exists to
// prevent. A caller that wants the failure as data unwraps [SaidNo] and reads
// Output, which is an explicit act.
func Run(binary string, timeout time.Duration, args ...string) (Output, error) {
	return RunWithInput(binary, timeout, nil, args...)
}

// RunWithInput is Run with something on the program's stdin.
//
// One caller: core asking an integration's subcommand, which takes a JSON
// object in and gives one back. A tool driven from the command line takes its
// arguments and nothing else.
func RunWithInput(binary string, timeout time.Duration, input []byte, args ...string) (Output, error) {
	if binary == "" {
		return Output{}, errors.New("no binary to run")
	}
	if timeout <= 0 {
		// Not defaulted. A missing timeout is the unbounded wait R18 exists to
		// prevent, and quietly supplying one would hide the bug in whichever
		// caller forgot rather than showing it the first time it runs.
		return Output{}, fmt.Errorf("%s was given no timeout to run under", binary)
	}

	ctx, stop := context.WithTimeout(context.Background(), timeout)
	defer stop()

	run := exec.CommandContext(ctx, binary, args...)
	run.WaitDelay = FlushAfterKilling
	if len(input) > 0 {
		run.Stdin = bytes.NewReader(input)
	}
	var out, problems bytes.Buffer
	run.Stdout, run.Stderr = &out, &problems

	err := run.Run()
	produced := Output{Stdout: out.Bytes(), Stderr: problems.Bytes()}

	// ctx before err, and the order is the whole point: a process the context
	// killed comes back as an ordinary exit error, so asking err first reports
	// every timeout as "signal: killed".
	if ctx.Err() != nil {
		return produced, &TookTooLong{Binary: binary, Args: args, Timeout: timeout}
	}
	if err == nil {
		return produced, nil
	}
	var exited *exec.ExitError
	if errors.As(err, &exited) {
		return produced, &SaidNo{Binary: binary, Args: args,
			Code: exited.ExitCode(), Stderr: produced.Stderr}
	}
	return produced, &DidNotRun{Binary: binary, Err: err}
}

// DidNotRun is the program failing to start at all: it is not there, it is not
// executable, the directory it was named in does not exist.
//
// It is its own type because it and [SaidNo] mean opposite things — a broken
// installation against an answer — and a container acting on them owes the
// caller container.NotRunning for one and container.Refused for the other.
// Nothing can be said about what the program would have done.
type DidNotRun struct {
	Binary string
	Err    error
}

// Error names the path in full, which is the one case where the path is the
// news: what went wrong is that this exact string is not a program.
func (d *DidNotRun) Error() string { return fmt.Sprintf("cannot run %s: %v", d.Binary, d.Err) }

func (d *DidNotRun) Unwrap() error { return d.Err }

// TookTooLong is the program running past its timeout and being given up on.
//
// It is not [SaidNo]: nothing was decided, and a caller that treats it as a
// refusal is recording a verdict the tool never gave.
type TookTooLong struct {
	Binary  string
	Args    []string
	Timeout time.Duration
}

func (t *TookTooLong) Error() string {
	return fmt.Sprintf("%s took longer than %s and was given up on", asked(t.Binary, t.Args), t.Timeout)
}

// SaidNo is the program running, finishing, and exiting non-zero.
//
// Stderr is carried rather than summarised so that a caller which understands
// the tool's own vocabulary can read it — sketchybar marks its complaints `[!]`
// and the marking is what makes a batch legible.
type SaidNo struct {
	Binary string
	Args   []string
	Code   int
	Stderr []byte
}

func (s *SaidNo) Error() string {
	return fmt.Sprintf("%s exited %d: %s", asked(s.Binary, s.Args), s.Code, Summarise(s.Stderr))
}

// asked is how an invocation is named in an error: the tool's name and what it
// was asked, without the directory it lives in.
//
// The directory is not news once the program has run — it ran, so it is where
// the configuration said. [DidNotRun] is the one case where the path IS the
// news, and that one prints it in full.
func asked(binary string, args []string) string {
	return strings.TrimSpace(filepath.Base(binary) + " " + strings.Join(args, " "))
}

// Summarise turns whatever a program printed into one short line fit for an
// error message or a log.
//
// The FIRST non-empty line, because a tool that failed says why first and then
// prints something large: zellij answers a missing session with a coloured list
// of every session that does exist.
//
// Cleaned through core's own [session.CleanMessage] and
// [session.CleanLine] rather than a local strip. These are somebody else's
// bytes heading for a log file, a status bar, and in sketchybar's case back out
// to a terminal — so the escape handling has to be the careful one, which
// understands OSC as well as CSI and repairs invalid UTF-8 rather than passing
// half a rune along (§A15). The three copies this replaces did none of that.
//
// "nothing" rather than "" when a program failed in silence, because an error
// ending in a colon and a space reads as a bug in the error.
func Summarise(output []byte) string {
	for _, line := range strings.Split(session.CleanMessage(string(output)), "\n") {
		if cleaned := session.CleanLine(line); cleaned != "" {
			return cleaned
		}
	}
	return "nothing"
}

// Ask runs one of an integration's subcommands and returns the JSON object it
// printed. Every way it can fail arrives as one error: not there, crashed,
// hung, or printed something that is not JSON (D-38).
func Ask(binary, command string, input []byte, timeout time.Duration) ([]byte, error) {
	out, err := RunWithInput(binary, timeout, input, command)
	if err != nil {
		return nil, err
	}
	answer := bytes.TrimSpace(out.Stdout)
	if !json.Valid(answer) {
		return nil, fmt.Errorf("%s %s answered %s, which is not JSON", binary, command, Summarise(answer))
	}
	return answer, nil
}
