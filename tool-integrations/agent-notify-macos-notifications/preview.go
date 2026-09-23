package main

import (
	"fmt"
	"strings"

	agentnotify "github.com/lassoColombo/agent-notify"
)

// Preview is how much of an agent's last message a notification carries.
//
// It used to be about the menu — a line under each row and a tooltip behind it
// — and both were bad in the same way: a fragment of a sentence, in a place
// nobody chose to look. What an agent said belongs in the notification, which
// arrives when it is said, and this is now how much of it goes in.
type Preview struct {
	Lines int
	Width int
}

func (p Preview) sane() string {
	switch {
	case p.Lines < 1:
		return "lines must be at least 1"
	case p.Width < 16:
		return "width must be at least 16 characters"
	}
	return ""
}

// on reports whether there is a preview at all. A zero Preview is a legitimate
// thing to ask for — rows with nothing behind them — and it is also what a
// caller that never set one has, so it has to be safe rather than clever.
func (p Preview) on() bool { return p.Lines > 0 && p.Width > 0 }

// Said is the agent's last message alone, wrapped and capped — no state line in
// front of it. It is what a notification's body is made of, where the state has
// already been said in the subtitle and saying it twice is noise.
func (p Preview) Said(record agentnotify.Record) string {
	if !p.on() {
		return ""
	}
	body := wrap(agentnotify.CleanMessage(record.Message), p.Width)
	if len(body) > p.Lines {
		body = body[:p.Lines]
		body[p.Lines-1] = truncate(body[p.Lines-1], max(p.Width-1, 1)) + "…"
	}
	return strings.Join(body, "\n")
}

// State is the (kernel, detail) pair as a person would say it. It is here
// rather than in render.go because a notification's subtitle wants exactly the
// same words a tooltip's first line does.
func State(record agentnotify.Record) string {
	return strings.ReplaceAll(record.State(), "-", " ")
}

// wrap breaks text into lines of at most width runes, on spaces where it can.
//
// Newlines the agent wrote are kept, because they are the difference between a
// list and a paragraph, and a blank line is kept too — it is the only paragraph
// break there is room for.
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
			// the whole tooltip sideways.
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
	// Leading and trailing blank lines are the agent's formatting, not content.
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// truncate cuts text to width runes. It counts runes rather than bytes because
// an agent writes prose, and a message cut mid-codepoint is a message with a
// replacement character in it.
func truncate(text string, width int) string {
	runes := []rune(text)
	if len(runes) <= width {
		return text
	}
	if width <= 0 {
		return ""
	}
	return string(runes[:width])
}

// plural is the difference between "and 1 more" and "and 2 more", which is the
// kind of thing that looks like nothing and reads like a bug.
func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
