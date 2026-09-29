package hook

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/lassoColombo/agent-notify/internal/config"
	"github.com/lassoColombo/agent-notify/internal/paths"
	"github.com/lassoColombo/agent-notify/session"
)

// Main is an agent-integration's main. `named` are the program's own
// subcommands, `install` and `uninstall`, which a person runs and which may
// speak and fail; any other word on the command line is refused with the
// usage. Otherwise this is a hook: the payload on stdin is decoded into
// `payload`, handed to translate, and what comes back is recorded.
//
// A hook never exits non-zero and never writes to stdout. Claude and codex
// both read exit code 2 as "block this" and stdout as a verdict, and a
// notifier that stops an agent working is far worse than one that does not
// notify (R2). A payload that does not parse is not an error either: an
// agent sends a different shape per hook and adds fields between releases.
func Main(arguments []string, usage string, named map[string]func([]string) int,
	payload any, translate func() (session.Report, bool)) int {
	if len(arguments) > 0 {
		if run, known := named[arguments[0]]; known {
			return run(arguments[1:])
		}
		fmt.Fprint(os.Stderr, usage)
		return 1
	}
	_ = json.NewDecoder(os.Stdin).Decode(payload)
	if report, worth := translate(); worth {
		Record(report)
	}
	return 0
}

// WriteDropIn files an agent-integration's table in `conf.d`, which is where
// its `[agent.<name>]` table goes: the process name is the agent's, and the
// integration is what knows it (D-85). It says where it wrote.
func WriteDropIn(name, table string) (string, error) {
	layout, err := paths.FromEnvironment()
	if err != nil {
		return "", err
	}
	return config.WriteDropIn(layout, name, table)
}

// RemoveDropIn takes it away again.
func RemoveDropIn(name string) error {
	layout, err := paths.FromEnvironment()
	if err != nil {
		return err
	}
	return config.RemoveDropIn(layout, name)
}

// LastResponses reads a transcript backwards for the newest responses that
// cost something, oldest first.
//
// Backwards, because the newest are the ones core has not counted, and a
// transcript only grows: a session an hour in is fifty megabytes, and reading
// it in full to find the last few hundred bytes that changed is a cost that
// grows with every turn. `cost` reads one line and says whether it is a
// response with a price on it; it is called newest line first. A response an
// agent writes across several lines is counted once. An empty path, a file
// not written yet, or a line the read landed in the middle of is no
// responses and no error.
func LastResponses(path string, cost func(line []byte) (session.Spend, bool)) []session.Spend {
	if path == "" {
		return nil
	}
	var responses []session.Spend
	_ = ReadBackwards(path, oneReadWorthOfTranscript, asFarBackAsItIsWorthGoing,
		func(line []byte) bool {
			response, itCost := cost(line)
			if !itCost {
				return true
			}
			if newest := len(responses) - 1; newest >= 0 && responses[newest].Response == response.Response {
				return true
			}
			responses = append(responses, response)
			return len(responses) < enoughResponses
		})
	for i, j := 0, len(responses)-1; i < j; i, j = i+1, j-1 {
		responses[i], responses[j] = responses[j], responses[i]
	}
	return responses
}

const (
	// oneReadWorthOfTranscript is how much is read at a time, working
	// backwards from the end.
	oneReadWorthOfTranscript = 64 << 10
	// enoughResponses is how many the search is content to stop at. A hook
	// fires on every tool call, so one new response since the last read is
	// the ordinary case; eight is the margin for the hooks that do not fire.
	enoughResponses = 8
	// asFarBackAsItIsWorthGoing stops a search that is not finding any. A
	// tool result is a line too, and reading a file writes a megabyte of one,
	// so the newest response is often behind a result far larger than one
	// read; and the search cannot be unbounded on a path the agent waits on.
	asFarBackAsItIsWorthGoing = 4 << 20
)
