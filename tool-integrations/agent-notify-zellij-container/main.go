// Command agent-notify-zellij-container places and focuses zellij sessions for
// [agent-notify].
//
// It is a container-integration: a program that gets *run* when something needs
// an answer, not a daemon that stays connected (plan.md D-38). Four
// subcommands, each one JSON object in and one JSON object out.
//
// Its sibling, agent-notify-zellij-display, paints titles and can do nothing
// else. They are separate on purpose: a display writes text, a container moves
// your cursor, and wanting the first is not the same decision as allowing the
// second (D-37).
package main

import (
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/lassoColombo/agent-notify/container"
)

// Settings is `[integration.zellij-container.settings]`, and nothing else in
// the file is ours. An undeclared key is refused by name.
type Settings struct {
	// Zellij is where zellij is, as an absolute path. Required, and named
	// after the tool rather than called `binary` because the table above it has
	// a `binary` of its own — this program's — and install prints both
	// together (D-67).
	Zellij string `toml:"zellij"`
}

// zellijTimeout bounds one zellij invocation. It has a name and a defined
// behaviour on expiry, which is what R18 asks for: the subcommand answers "I
// could not", core reads that as a container that did not run, and nothing
// waits.
//
// It is not configuration. Nobody editing a config file knows better than this
// how long `zellij action` should be allowed to take, and a person who set it
// wrong would be debugging a focus that fails for a reason the file does not
// mention.
const zellijTimeout = 2 * time.Second

func main() {
	if len(os.Args) > 1 && os.Args[1] == "install" {
		os.Exit(install(os.Args[2:]))
	}

	// Resolved on first use rather than before dispatch, and the subcommand
	// that does not use it is the reason: `capture-environment` runs as a
	// child of the hook, on the path the agent is blocked on, and it reads two
	// environment variables and returns (R1, R3). Resolving eagerly made it
	// pay for a config read, a LookPath and up to four stats on every hook
	// event — and fail outright on a machine where zellij is somewhere this
	// does not look, for a capture that never needed zellij at all.
	//
	// Nothing is lost by waiting. This program is run per question, so
	// "startup" and "the moment of use" are the same instant, and a
	// misconfiguration is still one error message — it just goes to whoever
	// asked a question that needed zellij. Its sibling containers already
	// work this way: aerospace defers the same lookup, and the display
	// answers capture before reading anything.
	zellij := sync.OnceValues(resolve)

	os.Exit(container.Main(container.Integration{
		Name:    Name,
		Capture: Capture,
		Interpret: func(captured json.RawMessage) (any, error) {
			found, err := zellij()
			if err != nil {
				return nil, err
			}
			return Interpret(found, captured)
		},
		Focus: func(coordinates json.RawMessage) (container.Outcome, error) {
			found, err := zellij()
			if err != nil {
				// Typed, rather than an exit code core would have to read
				// meaning into: zellij not being findable is the tool not
				// being there to ask (§A11.3).
				return container.Failed(container.NotRunning, err.Error()), nil
			}
			return Focus(found, coordinates)
		},
		Focused: func(coordinates json.RawMessage) (container.Verdict, error) {
			found, err := zellij()
			if err != nil {
				return container.Verdict{Answer: container.CannotTell, Detail: err.Error()}, nil
			}
			return Focused(found, coordinates)
		},
	}))
}

// resolve reads this integration's own section of agent-notify's config.
func resolve() (Zellij, error) {
	var settings Settings
	if err := me.Settings(&settings); err != nil {
		return Zellij{}, err
	}

	binary, err := TheZellijToRun(settings.Zellij)
	if err != nil {
		return Zellij{}, err
	}
	return Zellij{Binary: binary, Timeout: zellijTimeout}, nil
}
