package main

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"
)

// frameEvery is how often the view rereads the agent's screen. One tmux call a
// frame reads both what is selected and what that pane shows.
const frameEvery = 100 * time.Millisecond

// The terminal modes the view needs from its own pane: the alternate screen,
// SGR mouse reports for the wheel, and bracketed paste, so that a paste can be
// told from typing and handed on as one.
const (
	setUp    = "\x1b[?1049h\x1b[?25l\x1b[?1000h\x1b[?1006h\x1b[?2004h"
	tearDown = "\x1b[?2004l\x1b[?1006l\x1b[?1000l\x1b[?25h\x1b[?1049l"
)

// mirror is the view's state between frames.
type mirror struct {
	tmux Tmux
	self string

	selected      string
	width, height int

	// What the agent's pane showed last, so that scrolling redraws without
	// waiting for the next read.
	lines  []string
	cursor Cursor

	// scrolling is Alt v's mode, entered also by the wheel: the whole of the
	// pane's history is read, and keys move through it instead of going to
	// the agent. scroll is how many rows above the bottom the view is.
	scrolling bool
	scroll    int

	drawn string
}

func runView() error {
	t, self, err := here()
	if err != nil {
		return err
	}
	in := int(os.Stdin.Fd())
	before, err := term.MakeRaw(in)
	if err != nil {
		return err
	}
	defer term.Restore(in, before)
	os.Stdout.WriteString(setUp)
	defer os.Stdout.WriteString(tearDown)

	typed := make(chan []byte)
	go func() {
		buffer := make([]byte, 4096)
		for {
			n, err := os.Stdin.Read(buffer)
			if err != nil {
				close(typed)
				return
			}
			typed <- bytes.Clone(buffer[:n])
		}
	}()

	m := &mirror{tmux: t, self: self}
	frames := time.NewTicker(frameEvery)
	defer frames.Stop()
	m.read()
	m.draw()
	var pending []byte
	for {
		select {
		case input, open := <-typed:
			if !open {
				return nil
			}
			var tokens []Token
			tokens, pending = Tokenize(append(pending, input...))
			for _, token := range tokens {
				m.handle(token)
			}
			m.draw()
		case <-frames.C:
			m.read()
			m.draw()
		}
	}
}

// read is one frame's tmux call: this pane's size and the selection, and,
// when something is selected, where its cursor is and what its screen shows
// (all of its history too, while scrolling).
func (m *mirror) read() {
	arguments := []string{"display-message", "-p", "-t", m.self,
		"#{" + optionSelected + "}" + separator + "#{pane_width}" + separator + "#{pane_height}"}
	if m.selected != "" {
		arguments = append(arguments, ";", "display-message", "-p", "-t", m.selected,
			"#{pane_id}"+separator+"#{cursor_x}"+separator+"#{cursor_y}"+separator+
				"#{cursor_flag}"+separator+"#{history_size}",
			";", "capture-pane", "-p", "-e", "-t", m.selected)
		if m.scrolling {
			arguments = append(arguments, "-S", "-")
		}
	}
	out, err := m.tmux.run(arguments...)
	if err != nil && m.selected != "" {
		// The selected pane is gone: forget it, and learn what is selected
		// now from the first line, which tmux printed before it failed.
		m.lines, m.selected = nil, ""
	}
	first, rest, _ := strings.Cut(out, "\n")
	fields := strings.Split(first, separator)
	if len(fields) != 3 {
		return
	}
	m.width, _ = strconv.Atoi(fields[1])
	m.height, _ = strconv.Atoi(fields[2])
	if fields[0] != m.selected {
		m.selected = fields[0]
		m.lines, m.scrolling, m.scroll = nil, false, 0
		if m.selected != "" {
			m.read()
		}
		return
	}
	if m.selected == "" || err != nil {
		return
	}

	about, screen, _ := strings.Cut(rest, "\n")
	facts := strings.Split(about, separator)
	if len(facts) != 5 || facts[0] != m.selected {
		return
	}
	x, _ := strconv.Atoi(facts[1])
	y, _ := strconv.Atoi(facts[2])
	history, _ := strconv.Atoi(facts[4])
	if m.scrolling {
		y += history
	}
	m.cursor = Cursor{X: x, Y: y, Visible: facts[3] == "1"}
	m.lines = Lines(screen)
}

// handle is one token: the view's own when it is about scrolling, the agent's
// otherwise.
func (m *mirror) handle(token Token) {
	switch token.Kind {
	case ScrollMode:
		m.scrolling = !m.scrolling
		m.scroll = 0
		m.read()
	case Wheel:
		if !m.scrolling {
			m.scrolling = true
			m.read()
		}
		m.scrollBy(token.Delta)
	case Paste:
		m.leaveScroll()
		if m.selected != "" {
			_, _ = m.tmux.run(PasteInto(m.selected, token.Bytes)...)
		}
	case Keys:
		keys := token.Bytes
		if m.scrolling {
			keys = m.scrollKeys(keys)
		}
		if len(keys) > 0 && m.selected != "" {
			_, _ = m.tmux.run(SendKeys(m.selected, keys)...)
		}
	}
}

// scrollKeys moves through the history with the keys of tmux.conf's scroll
// mode, and returns whatever is left once a key that is not one of them
// arrives: that key leaves scrolling and goes to the agent, as typing into a
// scrolled-back terminal would.
func (m *mirror) scrollKeys(keys []byte) []byte {
	half := max(m.height/2, 1)
	moves := []struct {
		key string
		by  int
	}{
		{"\x1b[A", 1}, {"\x1b[B", -1}, {"\x1b[5~", m.height}, {"\x1b[6~", -m.height},
		{"k", 1}, {"j", -1}, {"K", half}, {"J", -half}, {"u", m.height}, {"d", -m.height},
		{"g", 1 << 30}, {"G", -(1 << 30)},
	}
next:
	for len(keys) > 0 {
		for _, move := range moves {
			if bytes.HasPrefix(keys, []byte(move.key)) {
				m.scrollBy(move.by)
				keys = keys[len(move.key):]
				if !m.scrolling {
					return keys
				}
				continue next
			}
		}
		if keys[0] == 'q' || (keys[0] == 0x1b && len(keys) == 1) {
			m.leaveScroll()
			return nil
		}
		m.leaveScroll()
		return keys
	}
	return nil
}

func (m *mirror) scrollBy(by int) {
	m.scroll = max(0, m.scroll+by)
	if m.scroll == 0 && by < 0 {
		m.leaveScroll()
	}
}

func (m *mirror) leaveScroll() {
	if m.scrolling {
		m.scrolling, m.scroll = false, 0
		m.read()
	}
}

// draw writes the frame, if it is not the one already on screen, as one
// synchronized update so that a redraw never shows half of itself.
func (m *mirror) draw() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	var frame strings.Builder
	frame.WriteString("\x1b[?2026h\x1b[?25l")
	row := func(i int, text string) {
		fmt.Fprintf(&frame, "\x1b[%d;1H\x1b[0m\x1b[2K%s\x1b[0m", i+1, text)
	}

	switch {
	case m.selected == "":
		for i := range m.height {
			row(i, "")
		}
		row(m.height/2, "\x1b[38;2;140;136;166m  select an agent on the left")
	default:
		top := 0
		if m.scrolling {
			top = 1
		}
		rows, at, scroll := Frame(m.lines, m.cursor, m.width, m.height-top, m.scroll)
		m.scroll = scroll
		if m.scrolling {
			row(0, fmt.Sprintf("\x1b[1;38;2;25;23;36;48;2;196;167;231m SCROLL \x1b[0m"+
				"\x1b[38;2;140;136;166m ↑%d · j/k J/K d/u g/G · Esc back", m.scroll))
		}
		for i, text := range rows {
			row(i+top, text)
		}
		if at.Visible && !m.scrolling {
			fmt.Fprintf(&frame, "\x1b[%d;%dH\x1b[?25h", at.Y+top+1, at.X+1)
		}
	}
	frame.WriteString("\x1b[?2026l")
	if frame.String() == m.drawn {
		return
	}
	m.drawn = frame.String()
	os.Stdout.WriteString(m.drawn)
}
