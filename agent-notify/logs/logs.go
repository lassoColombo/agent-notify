// Package logs opens the one log file every agent-notify process writes to.
//
// One file, appended to by everybody: the session-watcher, record-agent-event,
// the commands, and every integration. A write under O_APPEND is atomic with
// its own offset and slog emits one write per record, so concurrent writers
// interleave whole lines and never tear one. That is what makes the file worth
// having — every line names the component that wrote it, so one session can be
// followed across a hook, the watcher and a display with grep.
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

// TheVariableThatSetsTheLevel is the one knob, and it is an environment
// variable rather than a configuration key because the log is opened before the
// configuration is read — deliberately, so that complaints about the
// configuration have somewhere to go.
//
// It costs nothing to reach every process: the session-watcher hands the
// integrations it starts the environment it was given, so setting this once
// before starting anything sets it for the whole tree.
const TheVariableThatSetsTheLevel = "AGENT_NOTIFY_LOG_LEVEL"

// Open returns a logger on the shared log file, tagged with the component
// writing through it, and the closer for the file behind it.
//
// This is the one an integration calls: it has no layout of its own and no way
// to build one, because where things live is core's knowledge and resolving it
// twice is how two processes come to disagree about it.
//
// The returned io.Closer is always non-nil and always safe to call.
func Open(component string) (*slog.Logger, io.Closer) {
	layout, err := paths.FromEnvironment()
	if err != nil {
		return slog.New(slog.DiscardHandler), discard{}
	}
	return OpenFile(layout.LogFile(), component)
}

// OpenFile is Open for a caller that already knows where everything is — the
// session-watcher and the commands, which are handed their layout rather than
// resolving it a second time (see core.OpenAt for why that matters).
func OpenFile(path, component string) (*slog.Logger, io.Closer) {
	file, err := File(path)
	if err != nil {
		return slog.New(slog.DiscardHandler), discard{}
	}
	wanted, unreadable := level()
	handler := slog.NewTextHandler(file, &slog.HandlerOptions{Level: wanted, ReplaceAttr: utcTimes})

	// The tag is not decoration: every process writes this file, so a line that
	// does not say who wrote it is a line that cannot be read.
	logger := slog.New(handler).With("component", component)
	if unreadable != "" {
		logger.Warn("that is not a level, so this is INFO",
			TheVariableThatSetsTheLevel, unreadable)
	}
	return logger, file
}

// File opens the log itself, for the one writer that does not go through a
// logger: the session-watcher's own stderr, pointed here by whoever spawns it,
// so that what no program can log for itself — a panic on a goroutine other
// than the one holding the recover, a runtime fatal error, a crash inside cgo —
// lands in the same place as everything else.
//
// An integration's stderr does not come here. Core runs one and reads what it
// printed back out of the pipe (see the tool package), which is the better
// answer anyway: it arrives attached to the call that caused it.
func File(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, paths.FileMode)
}

// level reads the one knob, and reports back anything it could not read rather
// than swallowing it: a typo in a variable you set precisely because you wanted
// to see more must not be the reason you see the same as before and cannot tell
// why. An unreadable value is INFO, never silence.
func level() (slog.Level, string) {
	text := os.Getenv(TheVariableThatSetsTheLevel)
	if text == "" {
		return slog.LevelInfo, ""
	}
	var wanted slog.Level
	if err := wanted.UnmarshalText([]byte(text)); err != nil {
		return slog.LevelInfo, text
	}
	return wanted, ""
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
