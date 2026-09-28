package subscribe

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	"github.com/lassoColombo/agent-notify/session"
)

// A display that has to own its own process reads `agent-notify tail --json`.
//
// Most displays do not own one. Core runs them: it hands a view to `render` and
// they exit, which is the one interface everything else here has. A menu bar
// cannot live like that — an NSStatusItem dies with the process that made it —
// so it stops being something core runs and becomes something launchd starts,
// which §A10.2 already allows: an unsolicited connection is first-class, and
// the CLI is a client rather than a second implementation of anything.
//
// It goes through the CLI rather than linking [Run] because of what that buys
// once nothing else does: the socket, the handshake, the resync and the
// overflow rules stop being a published SDK and become core talking to itself.
// A display in any language is then a program that reads lines of JSON.

// howLongToWaitBeforeStartingTailAgain bounds the retry loop when `tail` will
// not stay up — no session-watcher yet, or a core that is being reinstalled
// underneath us. It doubles to this and no further.
const howLongToWaitBeforeStartingTailAgain = 2 * time.Second

// RunThroughTheCLI runs `agent-notify tail --json` and calls OnChange with every
// view it prints, until the context is cancelled.
//
// It restarts tail whenever it stops, for the reason [Run] reconnects: a
// display outlives the session-watcher, and a person who restarts one does not
// expect to restart the other.
func RunThroughTheCLI(ctx context.Context, integration Integration) error {
	if integration.OnChange == nil {
		return fmt.Errorf("%s has nothing to do: OnChange is nil", integration.Name)
	}
	// Checked here, before anything runs, for the reason D-70 gives: this SDK
	// was compiled from the same source as the record, so it knows exactly
	// which names its author can use.
	if err := session.ReasonTheseFieldsCannotBeWokenOn(integration.WakeOn); err != nil {
		return fmt.Errorf("%s: %w", integration.Name, err)
	}
	logger := integration.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	core, err := integration.CoreBinary()
	if err != nil {
		return fmt.Errorf("%s cannot find agent-notify to watch through: %w", integration.Name, err)
	}

	wait := 50 * time.Millisecond
	for ctx.Err() == nil {
		if err := readEveryView(ctx, core, integration, logger); err != nil && ctx.Err() == nil {
			logger.Info("tail stopped", "problem", err.Error(), "restarting in", wait.String())
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
		if wait < howLongToWaitBeforeStartingTailAgain {
			wait *= 2
		}
	}
	return nil
}

// readEveryView runs tail once and hands over each line it prints.
func readEveryView(
	ctx context.Context, core string, integration Integration, logger *slog.Logger,
) error {
	arguments := []string{"tail", "--json"}
	if len(integration.WakeOn) > 0 {
		arguments = append(arguments, "--wake-on", strings.Join(integration.WakeOn, ","))
	}
	if integration.WantEnded {
		arguments = append(arguments, "--all")
	}

	tail := exec.CommandContext(ctx, core, arguments...)
	// Without this the context cancelling is a suggestion: Wait does not return
	// until the pipes are closed, and nothing closes a pipe a killed child's
	// own children still hold.
	tail.WaitDelay = time.Second
	printed, err := tail.StdoutPipe()
	if err != nil {
		return err
	}
	if err := tail.Start(); err != nil {
		return err
	}
	logger.Debug("watching through the CLI", "agent-notify", core, "arguments", arguments)

	lines := bufio.NewScanner(printed)
	// A view carrying every session and every message can be large, and the
	// default 64KB would silently end the scan rather than truncate a line.
	lines.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for lines.Scan() {
		var view session.View
		if err := json.Unmarshal(lines.Bytes(), &view); err != nil {
			// One unreadable line says nothing about the next (R13).
			logger.Warn("a view could not be read", "problem", err.Error())
			continue
		}
		if err := integration.OnChange(view); err != nil {
			logger.Warn("rendering", "problem", err.Error())
		}
	}
	if err := lines.Err(); err != nil {
		_ = tail.Wait()
		return err
	}
	return tail.Wait()
}
