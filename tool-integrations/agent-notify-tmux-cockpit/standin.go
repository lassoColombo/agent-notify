package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/lassoColombo/agent-notify/session"
)

// The stand-in is the pane a borrowed agent is swapped with. It is an empty
// pane, one tmux made with no program in it (`split-window ''`): there is
// nothing in it to die, to be started again or to be told anything. On the
// stage it is what the tab shows while no agent is there, and the list writes
// into it what that is. Holding an agent's place it is never seen: a terminal
// that comes to that window finds the agent, which the server gives back
// before it draws (D-129).

// Showing is what the stand-in says on the stage: a title for its pane and a
// few lines for the middle of it.
type Showing struct {
	Title string
	Lines []string
}

// Frame is a stand-in of this size saying this, as the bytes a program in the
// pane would have printed: the title on the pane, the lines in the middle of
// it, the first one in the text's own colour and the rest dimmer, and no
// cursor.
func Frame(showing Showing, width, height int) string {
	var frame strings.Builder
	// one synchronized update, so a redraw never shows half of itself
	frame.WriteString("\x1b[?2026h\x1b[?25l\x1b[0m\x1b[2J")
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
