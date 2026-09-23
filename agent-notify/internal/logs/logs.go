// Package logs opens the one log file both processes write to.
//
// It has no error return, and that is the whole point. record-agent-event runs
// on the hook path, where a diagnostic channel that can fail is a diagnostic
// channel that will eventually take the agent down with it (plan.md §A17 R2,
// R13). When the file cannot be opened, the logger discards — nothing is ever
// written to stdout, which the agent reads, and nothing is written to stderr,
// which some agents also read.
package logs

import (
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/lassoColombo/agent-notify/internal/paths"
)

// Open returns a logger tagged with the component writing through it, and the
// closer for the file behind it. The tag is not decoration: the session-watcher
// and record-agent-event share this file, so a line that does not say which one
// wrote it is a line that cannot be read.
//
// The returned io.Closer is always non-nil and always safe to call.
func Open(path, component string) (*slog.Logger, io.Closer) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, paths.FileMode)
	if err != nil {
		return slog.New(slog.DiscardHandler), discard{}
	}
	handler := slog.NewTextHandler(file, &slog.HandlerOptions{ReplaceAttr: utcTimes})
	return slog.New(handler).With("component", component), file
}

// utcTimes makes every timestamp in the file UTC. Stored and displayed times are
// UTC; only measured durations use a monotonic clock (plan.md §A17 R19). A log
// read on one machine about a process that ran on another is worth this line.
func utcTimes(_ []string, attr slog.Attr) slog.Attr {
	if attr.Key == slog.TimeKey && attr.Value.Kind() == slog.KindTime {
		attr.Value = slog.TimeValue(attr.Value.Time().UTC().Round(time.Millisecond))
	}
	return attr
}

// discard stands in for the file when there is no file. Returning a nil Closer
// instead would move one branch out of this package and into every caller.
type discard struct{}

func (discard) Close() error { return nil }
