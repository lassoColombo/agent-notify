package agentnotify

import (
	"fmt"
	"time"
)

// Duration is how long, written the way a person writes it: "8s", "168h",
// "30m". It is time.ParseDuration's spelling and the only one this system has —
// core's own file and every integration's table read the same, which is what
// D-62 wanted and could not have while the type was a bare time.Duration
// (D-78). An integration imports this rather than growing its own.
//
// It is a struct rather than a named time.Duration so that EVERY spelling
// reaches UnmarshalText. A named int64 lets go-toml decode a bare number
// natively, and that is how `announce = 8` comes to mean eight nanoseconds
// instead of the eight seconds whoever wrote it meant — the trap D-62 named and
// then kept. Here a bare number arrives as text and is refused for wanting a
// unit. The one that survives is 0, which time.ParseDuration accepts and which
// means the same thing however it is spelled.
//
// Text it cannot read is not an error. It is remembered, and Unreadable answers
// with it, so that one mistyped duration costs that one value and not the file
// it was written in (§A14).
type Duration struct {
	d   time.Duration
	bad string
}

// NewDuration is a duration a program chose rather than a person: a default.
func NewDuration(d time.Duration) Duration { return Duration{d: d} }

// UnmarshalText never fails, by design. See Unreadable.
func (d *Duration) UnmarshalText(text []byte) error {
	parsed, err := time.ParseDuration(string(text))
	if err != nil {
		d.bad = string(text)
		return nil
	}
	d.d = parsed
	return nil
}

// Duration is the value. It is zero when the text was unreadable, so a caller
// for which zero is itself a request — "stay passive", "keep nothing" — has to
// ask Unreadable before believing it.
func (d Duration) Duration() time.Duration { return d.d }

// Unreadable is the text that was not a duration, and "" when there was none.
func (d Duration) Unreadable() string { return d.bad }

// String is what doctor prints and what a complaint quotes.
func (d Duration) String() string {
	if d.bad != "" {
		return fmt.Sprintf("%q, which is not a duration", d.bad)
	}
	return d.d.String()
}
