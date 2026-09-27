package session_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/lassoColombo/agent-notify/session"
)

// TestCleanMessageRemovesWhatATerminalWouldObey.
//
// This text leaves through status bars, tab titles and notifications, all of
// which are terminals or talk to one. An OSC sequence that survives the trip
// can retitle a window; a CSI sequence can move the cursor and overwrite what
// is around it. Core is the only place that sees all of this text.
func TestCleanMessageRemovesWhatATerminalWouldObey(t *testing.T) {
	cases := map[string]string{
		"\x1b[31mred\x1b[0m":                   "red",
		"\x1b]0;new window title\x07innocent":  "innocent",
		"\x1b]8;;http://example.com\x1b\\link": "link",
		"\x1b[2J\x1b[Hcleared your screen":     "cleared your screen",
		"bell\x07 and backspace\x08":           "bell and backspace",
		"a\x00b":                               "ab",
		"plain":                                "plain",
		"  padded  ":                           "padded",
		"emoji ✅ and accents é":                "emoji ✅ and accents é",
		"\x1bc":                                "",
		"tabs\tbecome\tspaces":                 "tabs become spaces",
		// A properly encoded C1 control is a valid rune, and dropped.
		"\u009b31m C1 introducer": "31m C1 introducer",
		// The bare byte 0x9b is not valid UTF-8 at all, so it is replaced before
		// the control check ever sees it — the same outcome a terminal in UTF-8
		// mode reaches.
		"\x9bbare byte": "\ufffdbare byte",
	}
	for input, want := range cases {
		if got := session.CleanMessage(input); got != want {
			t.Errorf("CleanMessage(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestCleanMessageKeepsNewlinesAndCleanLineDoesNot(t *testing.T) {
	if got := session.CleanMessage("one\ntwo"); got != "one\ntwo" {
		t.Errorf("CleanMessage dropped a newline a preview pane wants: %q", got)
	}
	if got := session.CleanMessage("one\r\ntwo\rthree"); got != "one\ntwo\nthree" {
		t.Errorf("CleanMessage(%q) = %q", "one\\r\\ntwo\\rthree", got)
	}
	if got := session.CleanLine("one\ntwo"); got != "one two" {
		t.Errorf("CleanLine kept a newline that would break a tab title: %q", got)
	}
}

func TestInvalidUTF8IsRepairedNotPropagated(t *testing.T) {
	got := session.CleanMessage("valid \xff\xfe bytes")
	if !strings.Contains(got, "valid") || !strings.Contains(got, "bytes") {
		t.Errorf("CleanMessage ate the good text too: %q", got)
	}
	if strings.ContainsRune(got, 0xfffd) != true {
		t.Errorf("CleanMessage(%q) = %q, want the bad bytes replaced", "valid \\xff\\xfe bytes", got)
	}
	for _, r := range got {
		if r == 0xfffd {
			continue
		}
		if r > 0x10ffff {
			t.Errorf("invalid rune survived: %q", got)
		}
	}
}

// TestBoundsAreBounds: the marker counts against the limit, because a bound
// that can be exceeded by bounding is not a bound.
func TestBoundsAreBounds(t *testing.T) {
	long := strings.Repeat("a", session.MaxMessageBytes*2)
	got := session.CleanMessage(long)
	if len(got) > session.MaxMessageBytes {
		t.Errorf("CleanMessage produced %d bytes, over the %d limit", len(got), session.MaxMessageBytes)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("a truncated message does not say it was truncated: ...%q", got[len(got)-10:])
	}

	line := session.CleanLine(strings.Repeat("b", session.MaxLineBytes*2))
	if len(line) > session.MaxLineBytes {
		t.Errorf("CleanLine produced %d bytes, over the %d limit", len(line), session.MaxLineBytes)
	}
}

// TestTruncationDoesNotSplitARune: a half-written rune is invalid UTF-8, which
// is exactly what CleanMessage exists to prevent.
func TestTruncationDoesNotSplitARune(t *testing.T) {
	for padding := range 8 {
		text := strings.Repeat("a", session.MaxMessageBytes-padding) + strings.Repeat("é", 20)
		got := session.CleanMessage(text)
		if len(got) > session.MaxMessageBytes {
			t.Fatalf("padding %d: %d bytes", padding, len(got))
		}
		if !utf8.ValidString(got) {
			t.Errorf("padding %d produced invalid UTF-8", padding)
		}
	}
}
