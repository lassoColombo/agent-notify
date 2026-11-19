package main

import (
	"slices"
	"testing"
)

func TestTypingIsKeys(t *testing.T) {
	tokens, rest := Tokenize([]byte("hi\r\x1b[A"))
	if len(tokens) != 1 || tokens[0].Kind != Keys || string(tokens[0].Bytes) != "hi\r\x1b[A" || rest != nil {
		t.Errorf("tokens = %+v, rest = %q", tokens, rest)
	}
}

func TestTheWheelIsTheViews(t *testing.T) {
	tokens, _ := Tokenize([]byte("a\x1b[<64;10;5Mb\x1b[<65;10;5M\x1b[<0;3;3M"))
	kinds := []Kind{}
	for _, token := range tokens {
		kinds = append(kinds, token.Kind)
	}
	if !slices.Equal(kinds, []Kind{Keys, Wheel, Keys, Wheel}) {
		t.Fatalf("kinds = %v; a click is dropped, the wheel kept, typing either side kept", kinds)
	}
	if tokens[1].Delta != 3 || tokens[3].Delta != -3 {
		t.Errorf("deltas = %d, %d; want up 3 and down 3", tokens[1].Delta, tokens[3].Delta)
	}
}

func TestAltVIsScrollMode(t *testing.T) {
	tokens, _ := Tokenize([]byte("\x1bv"))
	if len(tokens) != 1 || tokens[0].Kind != ScrollMode {
		t.Errorf("tokens = %+v", tokens)
	}
}

func TestAPasteIsOne(t *testing.T) {
	tokens, _ := Tokenize([]byte("\x1b[200~line one\rline two\x1b[201~x"))
	if len(tokens) != 2 || tokens[0].Kind != Paste || string(tokens[0].Bytes) != "line one\rline two" {
		t.Errorf("tokens = %+v", tokens)
	}
}

// TestAHalfReadSequenceWaitsForTheRest: a mouse report or a paste can be split
// across two reads, and half of one must not be typed into the agent.
func TestAHalfReadSequenceWaitsForTheRest(t *testing.T) {
	for _, half := range []string{"\x1b[<64;1", "\x1b[200~abc", "\x1b["} {
		tokens, rest := Tokenize([]byte("x" + half))
		if len(tokens) != 1 || string(tokens[0].Bytes) != "x" || string(rest) != half {
			t.Errorf("%q: tokens = %+v, rest = %q", half, tokens, rest)
		}
	}
}

func TestKeysAreSpelledByteForByte(t *testing.T) {
	got := SendKeys("%3", []byte("a\x1b[Aé!"))
	want := []string{
		"send-keys", "-t", "%3", "-H", "61", "1b", "5b", "41", ";",
		"send-keys", "-t", "%3", "-l", "--", "é", ";",
		"send-keys", "-t", "%3", "-H", "21",
	}
	if !slices.Equal(got, want) {
		t.Errorf("SendKeys =\n %q\nwant\n %q", got, want)
	}
}
