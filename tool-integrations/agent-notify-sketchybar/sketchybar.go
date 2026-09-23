package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lassoColombo/agent-notify/tool"
)

// Sketchybar is the only thing here that runs anything.
type Sketchybar struct {
	Binary  string
	Timeout time.Duration
}

// Do performs one render.
//
// It returns the complaints sketchybar made rather than an error, because
// sketchybar does not fail in the usual way. Asked for an item that is not
// there it says so and carries on with the rest of the batch — and the exit
// code, measured on v2.24.0, reports only whether the LAST message in the batch
// succeeded. `--set missing --set real` exits 0; `--set real --set missing`
// exits 1; neither says anything about the hundred messages in between.
//
// So the exit code is per-message and unusable as a verdict on a render, and
// the complaints are the truth. The severity is sketchybar's own rather than a
// guess at one: it marks informational lines `[?]` and problems `[!]`. "Item
// already exists" is the first kind and arrives on every render after the
// first, by design.
//
// An error comes back only when sketchybar did not run at all — it is missing,
// or it hung — because that is the only case where nothing can be said about
// what happened.
func (s Sketchybar) Do(arguments []string) ([]string, error) {
	// The running is [tool.Run] — the WaitDelay, the check order, and the
	// difference between "it is not there" and "it said no" all live there
	// (D-68). This program is the reason that last one is a type: it is the
	// one caller that has to read a refusal rather than pass it on.
	out, err := tool.Run(s.Binary, s.Timeout, arguments...)
	said := complaints(string(out.Stderr))

	var refused *tool.SaidNo
	switch {
	case err != nil && !errors.As(err, &refused):
		// It timed out, or it did not run at all: not installed, not
		// executable, no such directory. tool.Run has already said which and
		// named the binary, and there is nothing to add.
		return nil, err
	case err != nil && len(said) == 0:
		// It ran, it failed, and it did not blame a message. The exit code
		// otherwise only reports the last message, so a failure with nothing
		// marked `[!]` is sketchybar refusing wholesale — which is what
		// "'env USER' not set! abort" looks like, and what a display would
		// otherwise do in complete silence for ever (D-43).
		return nil, fmt.Errorf("sketchybar refused: %s", tool.Summarise(out.Stderr))
	}
	return said, nil
}

// Seen reports whether a pointer has reached this display since the
// announcement carrying this key was raised.
//
// A --query is the only way to read anything back off the bar. It is asked at
// most once a paint and only while an announcement is live, so the ordinary
// paint pays nothing for it.
//
// The mark is the announcement's own key, which every hover script this display
// writes overwrites with the word `seen`. Anything that is not the key means a
// pointer has been here since — including a query that failed, which reads as
// not seen: a chip that closes a moment early is a smaller wrong than one that
// stays open until somebody happens to walk a pointer across the bar.
func (s Sketchybar) Seen(item, key string) bool {
	out, err := tool.Run(s.Binary, s.Timeout, "--query", item)
	if err != nil {
		return false
	}
	var answer struct {
		Icon struct {
			Value string `json:"value"`
		} `json:"icon"`
	}
	if err := json.Unmarshal(out.Stdout, &answer); err != nil {
		return false
	}
	return answer.Icon.Value != key
}

func complaints(output string) []string {
	var real []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[!]") {
			real = append(real, line)
		}
	}
	return real
}
