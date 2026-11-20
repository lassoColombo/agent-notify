package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"

	"github.com/lassoColombo/agent-notify/session"
)

// The stand-in is the pane a borrowed agent is swapped with. On the stage it
// is what the tab shows while no agent is there; in an agent's window it holds
// the agent's place, and says where the agent went. It decides nothing: the
// list tells it what to draw.
//
// It is also what makes a list that died harmless. The link to the list closes
// when the list is gone, whatever it died of, and a stand-in that sees that
// gives back the pane whose place it holds and then closes itself.

// The terminal modes a stand-in needs from its pane: the alternate screen, no
// cursor, and focus reports, which is how it knows somebody arrived.
const (
	setUp    = "\x1b[?1049h\x1b[?25l\x1b[?1004h"
	tearDown = "\x1b[?1004l\x1b[?25h\x1b[?1049l"
	focusIn  = "\x1b[I"
)

func runStandIn(arguments []string) error {
	if len(arguments) != 1 {
		return fmt.Errorf("stand-in takes the list's socket, and is started by the list")
	}
	settings, err := Read()
	if err != nil {
		return err
	}
	socket, self, err := here()
	if err != nil {
		return err
	}
	tmux := Tmux{Binary: settings.Binary, Socket: socket}

	list, err := net.Dial("unix", arguments[0])
	if err != nil {
		return leave(tmux, self)
	}
	defer list.Close()

	in := int(os.Stdin.Fd())
	before, err := term.MakeRaw(in)
	if err != nil {
		return err
	}
	restore := func() {
		os.Stdout.WriteString(tearDown)
		_ = term.Restore(in, before)
	}
	os.Stdout.WriteString(setUp)

	told := make(chan Showing)
	go func() {
		defer close(told)
		lines := bufio.NewScanner(list)
		for lines.Scan() {
			var showing Showing
			if json.Unmarshal(lines.Bytes(), &showing) == nil {
				told <- showing
			}
		}
	}()
	typed := make(chan []byte)
	go func() {
		defer close(typed)
		buffer := make([]byte, 256)
		for {
			n, err := os.Stdin.Read(buffer)
			if err != nil {
				return
			}
			typed <- bytes.Clone(buffer[:n])
		}
	}()
	resized := make(chan os.Signal, 1)
	signal.Notify(resized, syscall.SIGWINCH)

	var showing Showing
	for {
		select {
		case next, open := <-told:
			if !open {
				restore()
				return leave(tmux, self)
			}
			showing = next
			draw(showing)
		case input, open := <-typed:
			if !open {
				restore()
				return nil
			}
			if bytes.Contains(input, []byte(focusIn)) {
				fmt.Fprintln(list, gotFocus)
			}
			if bytes.ContainsAny(input, "\r\n") {
				fmt.Fprintln(list, pressedEnter)
			}
		case <-resized:
			draw(showing)
		}
	}
}

// leave is what a stand-in does when there is no list: the pane whose place
// it holds goes back there, and the stand-in closes its own pane, which
// `remain-on-exit` would otherwise keep.
func leave(tmux Tmux, self string) error {
	world, err := tmux.Observe()
	if err != nil {
		return err
	}
	for _, pane := range world.Borrowed() {
		if pane.StandIn == self {
			if _, err := tmux.run(giveBack(pane)...); err != nil {
				return err
			}
		}
	}
	_, err = tmux.run("kill-pane", "-t", self)
	return err
}

// draw paints what the list said: the title on the pane, the lines in the
// middle of it, the first one in the text's own colour and the rest dimmer.
func draw(showing Showing) {
	width, height, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || width <= 0 || height <= 0 {
		return
	}
	os.Stdout.WriteString(Frame(showing, width, height))
}

// Frame is the pure half of draw.
func Frame(showing Showing, width, height int) string {
	var frame strings.Builder
	// one synchronized update, so a redraw never shows half of itself
	frame.WriteString("\x1b[?2026h\x1b[0m\x1b[2J")
	frame.WriteString(ansi.SetWindowTitle(session.CleanLine(showing.Title)))
	top := max(0, (height-len(showing.Lines))/2)
	for i, line := range showing.Lines {
		if top+i >= height {
			break
		}
		line = ansi.Truncate(session.CleanLine(line), width, "…")
		colour := "\x1b[38;2;140;136;166m" // subtle
		if i == 0 {
			colour = "\x1b[38;2;224;222;244m" // text
		}
		column := max(0, (width-ansi.StringWidth(line))/2)
		fmt.Fprintf(&frame, "\x1b[%d;%dH%s%s\x1b[0m", top+i+1, column+1, colour, line)
	}
	frame.WriteString("\x1b[?2026l")
	return frame.String()
}
