package agentnotify

import (
	"strings"
	"unicode/utf8"
)

// The bounds on stored text. These are not configurable: they are the reason
// the record stays about a kilobyte, and a record rides in every delta to every
// subscriber (plan.md §A13.1).
const (
	// MaxMessageBytes bounds what an agent last said. Generous enough for a
	// preview pane, small enough that a subscriber receiving one per session
	// notices nothing.
	MaxMessageBytes = 4096
	// MaxLineBytes bounds a name, a detail or a working directory — anything
	// that ends up in a tab title or a chip.
	MaxLineBytes = 256
)

// truncated marks text the bounds cut, so a display shows a cut rather than a
// sentence that mysteriously stops.
const truncated = "…"

// CleanMessage makes an agent's text safe to store, carry and render, and
// bounds it.
//
// Sanitising is not politeness. Text arrives from an agent's output and leaves
// through a status bar, a tab title and a notification — all of which are
// terminals or talk to one. An escape sequence that survives the trip can
// retitle a window, move a cursor, or in the OSC case ask the terminal to do
// considerably more than that. Core is the one place that sees all of this text,
// so core is where it is neutralised (§A15).
//
// Newlines survive, because a preview pane wants them. Flattening to one line
// is a rendering decision and belongs to whoever renders (R24).
func CleanMessage(text string) string {
	return bound(strip(text, true), MaxMessageBytes)
}

// CleanLine is CleanMessage for text that is never more than one line: a name,
// a detail, a working directory. A newline in a tab title is never what anybody
// meant.
func CleanLine(text string) string {
	return bound(strip(text, false), MaxLineBytes)
}

// strip removes escape sequences and control characters, and repairs invalid
// UTF-8. It works on runes rather than bytes so that the C1 range cannot be
// confused with the continuation bytes of a perfectly good multi-byte rune.
func strip(text string, keepNewlines bool) string {
	var out strings.Builder
	out.Grow(len(text))

	runes := []rune(strings.ToValidUTF8(text, "�"))
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case r == 0x1b:
			i = skipEscape(runes, i)
		case r == '\n' || r == '\r':
			if keepNewlines {
				// A lone CR, or the CR of a CRLF pair, becomes one newline.
				if r == '\r' && i+1 < len(runes) && runes[i+1] == '\n' {
					i++
				}
				out.WriteByte('\n')
			} else {
				out.WriteByte(' ')
			}
		case r == '\t':
			out.WriteByte(' ')
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f):
			// Every other C0 and C1 control, dropped.
		default:
			out.WriteRune(r)
		}
	}
	return strings.TrimSpace(out.String())
}

// skipEscape returns the index of the last rune belonging to the escape
// sequence starting at start, so that the caller's loop resumes after it.
func skipEscape(runes []rune, start int) int {
	i := start + 1
	if i >= len(runes) {
		return start
	}
	switch runes[i] {
	case '[':
		// CSI: parameters, then one final byte in 0x40-0x7e.
		for i++; i < len(runes); i++ {
			if runes[i] >= 0x40 && runes[i] <= 0x7e {
				return i
			}
		}
		return len(runes) - 1
	case ']':
		// OSC: runs until BEL or the two-rune string terminator ESC \.
		for i++; i < len(runes); i++ {
			if runes[i] == 0x07 {
				return i
			}
			if runes[i] == 0x1b && i+1 < len(runes) && runes[i+1] == '\\' {
				return i + 1
			}
		}
		return len(runes) - 1
	default:
		// Everything else is a two-rune sequence.
		return i
	}
}

// bound cuts text to at most limit bytes, on a rune boundary, and says that it
// cut. The marker counts against the limit: a bound that can be exceeded by
// bounding is not a bound.
func bound(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	keep := limit - len(truncated)
	if keep <= 0 {
		// No room for the marker itself. Slicing it would hand back half a
		// rune, which is worse than handing back nothing.
		return ""
	}
	for keep > 0 && !utf8.RuneStart(text[keep]) {
		keep--
	}
	return strings.TrimRight(text[:keep], " \n") + truncated
}
