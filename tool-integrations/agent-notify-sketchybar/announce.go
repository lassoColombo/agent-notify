package main

import (
	"fmt"
	"strings"
	"unicode"

	agentnotify "github.com/lassoColombo/agent-notify"
)

// Preview is how much of an agent's last message the chip shows.
//
// Lines and Depth are two different numbers, and conflating them is the bug
// this shape exists to prevent: every line is WRITTEN to the bar and only the
// first Lines of them are DRAWN, so a scroll moves nothing but `drawing`. If
// only the visible ones were written, scrolling would reveal blank rows.
type Preview struct {
	Lines int
	Depth int
	Width int
	// Text is what the agent's words are drawn in. The name on top is drawn in
	// the state's own colour instead, so that a preview says which semaphore it
	// belongs to without a second glance at the chip above it.
	Text string
}

func (p Preview) sane() string {
	switch {
	case p.Lines < 2:
		return "lines must be at least 2: one for the name and one to read"
	case p.Depth < p.Lines:
		return fmt.Sprintf("depth = %d is less than lines = %d, so the first screenful "+
			"would not fit in what is written", p.Depth, p.Lines)
	case p.Width < 16:
		return "width must be at least 16 characters"
	}
	return ""
}

// on reports whether there is a preview at all. A zero Preview is a legitimate
// thing to ask for — counters and rows and nothing under them — and it is also
// what a caller that never set one has, so it has to be safe rather than clever.
func (p Preview) on() bool { return p.Depth > 1 && p.Lines > 1 && p.Width > 0 }

// window is how many body lines are on screen at once. Slot 0 is the session's
// name and never scrolls, so the body gets one fewer.
func (p Preview) window() int { return p.Lines - 1 }

// Lines is the agent's last message, wrapped, with the session's name on top.
//
// The name is slot 0 and never scrolls, because the preview sits below every
// row of the chip and which agent you are reading would otherwise be a guess.
//
// A message longer than the depth is cut rather than allowed to run past what
// was written: the last line says so, because a message that simply stops
// reads as an agent that stopped.
func (p Preview) Of(record agentnotify.Record) []string {
	name := record.DisplayName()
	if detail := record.Detail; detail != "" {
		name += "  " + detail
	}
	lines := []string{truncate(name, p.Width)}

	body := wrap(record.Message, p.Width)
	if room := p.Depth - 1; len(body) > room {
		body = body[:room]
		if room > 0 {
			body[room-1] = truncate(body[room-1], max(p.Width-1, 1)) + "…"
		}
	}
	if len(body) == 0 {
		body = []string{"(nothing said yet)"}
	}
	return append(lines, body...)
}

// wrap breaks text into lines of at most width runes, on spaces where it can.
//
// Newlines the agent wrote are kept, because they are the difference between a
// list and a paragraph, and a blank line is kept too — it is the only paragraph
// break a bar can show.
func wrap(text string, width int) []string {
	var lines []string
	for _, paragraph := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}
		line := ""
		for _, word := range words {
			switch {
			case line == "":
				line = word
			case len([]rune(line))+1+len([]rune(word)) <= width:
				line += " " + word
			default:
				lines = append(lines, line)
				line = word
			}
			// A single word longer than the line gets cut rather than pushing
			// the whole preview sideways.
			for len([]rune(line)) > width {
				runes := []rune(line)
				lines = append(lines, string(runes[:width]))
				line = string(runes[width:])
			}
		}
		if line != "" {
			lines = append(lines, line)
		}
	}
	// Trailing blank lines are the agent's formatting, not content.
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// quotable makes text safe to sit inside single quotes in a generated shell
// command, which is where a hover's preview has to live.
//
// [verified 2026-09-18, against the real daemon] inside single quotes sh
// expands nothing: `$(touch /tmp/x)`, backticks, `$HOME`, `~` and `;` all
// arrive at sketchybar as the characters they are, and nothing runs. So there
// is exactly one character that matters — the closing quote — and one class of
// character that breaks the command rather than escaping it: the controls, of
// which a newline is the one an agent writes constantly.
//
// The apostrophe becomes a typographic one. That is a visible change to an
// agent's words and it is the right trade: the alternative spellings are to
// drop the character, which makes "don't" into "dont", or to end and reopen the
// quoting, which puts a shell's syntax inside a loop that must never get it
// wrong.
//
// This is applied where the quote is written and NOWHERE else. A row's label is
// argv and never sees a shell; escaping it there would show an agent writing
// `it’s` when it wrote `it's`.
func quotable(text string) string {
	var out strings.Builder
	out.Grow(len(text))
	for _, r := range text {
		switch {
		case r == '\'':
			out.WriteRune('’')
		case r == '\n' || r == '\t' || unicode.IsControl(r):
			out.WriteRune(' ')
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}
