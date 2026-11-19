package main

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// This file is pure: it splits what arrives on the view's terminal into what
// the view keeps for itself and what goes on to the agent, and spells the
// latter as tmux arguments.

// Kind is what a token is for.
type Kind int

const (
	// Keys go to the agent as typed.
	Keys Kind = iota
	// Wheel is a mouse wheel turn, Delta rows up (positive) or down.
	Wheel
	// ScrollMode is Alt v, which tmux.conf sends into the view instead of
	// putting the view's own pane in copy mode.
	ScrollMode
	// Paste is a bracketed paste, which goes to the agent through tmux's paste
	// so the agent sees one paste rather than a string of Enters.
	Paste
)

type Token struct {
	Kind  Kind
	Bytes []byte
	Delta int
}

const (
	pasteStart = "\x1b[200~"
	pasteEnd   = "\x1b[201~"
	mouseStart = "\x1b[<"
	altV       = "\x1bv"
)

// Tokenize splits input into tokens, and returns what is left over when the
// input stops part-way through a mouse report or a paste, to be read again
// with whatever comes next.
func Tokenize(input []byte) ([]Token, []byte) {
	var tokens []Token
	var keys []byte
	flush := func() {
		if len(keys) > 0 {
			tokens = append(tokens, Token{Kind: Keys, Bytes: keys})
			keys = nil
		}
	}
	for len(input) > 0 {
		switch {
		case bytes.HasPrefix(input, []byte(pasteStart)):
			end := bytes.Index(input, []byte(pasteEnd))
			if end < 0 {
				flush()
				return tokens, input
			}
			flush()
			tokens = append(tokens, Token{Kind: Paste, Bytes: input[len(pasteStart):end]})
			input = input[end+len(pasteEnd):]

		case bytes.HasPrefix(input, []byte(mouseStart)):
			end := bytes.IndexAny(input, "Mm")
			if end < 0 {
				flush()
				return tokens, input
			}
			flush()
			if delta, wheel := wheelOf(string(input[len(mouseStart):end])); wheel {
				tokens = append(tokens, Token{Kind: Wheel, Delta: delta})
			}
			input = input[end+1:]

		case bytes.HasPrefix(input, []byte(altV)):
			flush()
			tokens = append(tokens, Token{Kind: ScrollMode})
			input = input[len(altV):]

		case len(input) < len(mouseStart) && bytes.HasPrefix([]byte(mouseStart), input) && len(input) > 1:
			// "\x1b[" at the very end may be the start of a mouse report.
			flush()
			return tokens, input

		default:
			keys = append(keys, input[0])
			input = input[1:]
		}
	}
	flush()
	return tokens, nil
}

// wheelOf reads an SGR mouse report's button. 64 and 65 are the wheel; the
// low two bits say which way, and the modifier bits (4, 8, 16) are ignored.
func wheelOf(report string) (int, bool) {
	button, _, _ := strings.Cut(report, ";")
	code, err := strconv.Atoi(button)
	if err != nil || code&64 == 0 {
		return 0, false
	}
	switch code & 3 {
	case 0:
		return 3, true
	case 1:
		return -3, true
	}
	return 0, false
}

// SendKeys spells keys as tmux arguments for `pane`: runs of ASCII as hex, so
// that control characters and escape sequences arrive byte for byte, and
// anything else as literal text, since -H takes ASCII only. Each run is its
// own send-keys, joined with `;` into one invocation.
func SendKeys(pane string, keys []byte) []string {
	var arguments []string
	add := func(command ...string) {
		if len(arguments) > 0 {
			arguments = append(arguments, ";")
		}
		arguments = append(arguments, command...)
	}
	for len(keys) > 0 {
		if keys[0] < utf8.RuneSelf {
			end := 0
			for end < len(keys) && keys[end] < utf8.RuneSelf {
				end++
			}
			command := []string{"send-keys", "-t", pane, "-H"}
			for _, b := range keys[:end] {
				command = append(command, fmt.Sprintf("%02x", b))
			}
			add(command...)
			keys = keys[end:]
			continue
		}
		end := 0
		for end < len(keys) && keys[end] >= utf8.RuneSelf {
			end++
		}
		add("send-keys", "-t", pane, "-l", "--", string(keys[:end]))
		keys = keys[end:]
	}
	return arguments
}

// PasteInto is a paste as tmux arguments: through a buffer and paste-buffer
// -p, which brackets it only if the agent's pane asked for bracketed paste.
func PasteInto(pane string, text []byte) []string {
	return []string{
		"set-buffer", "-b", "agents-paste", "--", string(text), ";",
		"paste-buffer", "-p", "-d", "-b", "agents-paste", "-t", pane,
	}
}
