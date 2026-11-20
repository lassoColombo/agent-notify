package main

import (
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"

	"github.com/lassoColombo/agent-notify/session"
)

// Command is one tmux command, as data rather than as an effect.
type Command struct {
	Args []string
	Why  string
}

// Plan is what one server's panes should say, minus what they say now. It is
// pure: it reads the options as they were found rather than remembering what
// it last wrote, so it is right after a restart, and it never plans a write
// that would change nothing (R22).
//
// A pane is an agent's when a live record captured it, in this server — the
// socket and the pid both, because a server started again on the same socket
// hands the same pane ids out again. Two records claiming one pane is an
// agent restarted in it before core noticed the first one end, and the more
// urgent of the two is the one shown.
//
// A pane that carries our options and is nobody's is one of ours left over —
// an agent that ended, a record that is gone — and is given back by unsetting
// them. Nobody else writes them, so there is nothing to recover but that.
func Plan(socket string, records []session.Record, server Server, glyphs session.Palette) []Command {
	// Sorted here rather than trusted from the caller: which of two claims
	// wins must not depend on the order they were handed over in.
	ordered := slices.Clone(records)
	session.ByUrgency(ordered)

	want := map[string]Mark{}
	for _, record := range ordered {
		place, found := PlaceOf(record)
		if !found || place.Socket != socket || place.Server != server.PID || !record.Kernel.Live() {
			continue
		}
		if _, exists := server.Panes[place.Pane]; !exists {
			// The pane is gone and the session is not. Whether a session is
			// alive is core's to decide, from the agent's process; a display
			// that concluded anything from a pane would disagree with it (R4).
			continue
		}
		if _, taken := want[place.Pane]; taken {
			continue
		}
		want[place.Pane] = Mark{
			State: string(record.Kernel),
			Glyph: glyphs.For(record),
			Name:  record.DisplayName(),
		}
	}

	panes := make([]string, 0, len(server.Panes))
	for pane := range server.Panes {
		panes = append(panes, pane)
	}
	slices.SortFunc(panes, func(a, b string) int { return number(a) - number(b) })

	var commands []Command
	for _, pane := range panes {
		is, should := server.Panes[pane].values(), want[pane].values()
		for i, option := range options {
			switch {
			case should[i] == is[i]:
			case should[i] == "":
				commands = append(commands, Command{
					Args: []string{"set-option", "-p", "-u", "-t", pane, option},
					Why:  fmt.Sprintf("%s holds no live agent", pane),
				})
			default:
				commands = append(commands, Command{
					Args: []string{"set-option", "-p", "-t", pane, option, argument(should[i])},
					Why:  fmt.Sprintf("%s holds %s, %s", pane, want[pane].Name, want[pane].State),
				})
			}
		}
	}
	return commands
}

// argument is a value as tmux must be handed it. tmux reads an argument that
// ends in `;` as the end of a command, and `\;` as a `;` that is not
// [measured on 3.8]. A name is the agent's, and can end in anything.
func argument(value string) string {
	if strings.HasSuffix(value, ";") {
		return value[:len(value)-1] + `\;`
	}
	return value
}

// chain joins commands into one invocation.
func chain(commands []Command) []string {
	var args []string
	for i, command := range commands {
		if i > 0 {
			args = append(args, ";")
		}
		args = append(args, command.Args...)
	}
	return args
}

// number orders pane ids by their number, so that %10 comes after %9.
func number(pane string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(pane, "%"))
	return n
}

// Group sorts records into the tmux servers they live in, so that each one is
// read and written once however many agents it holds.
func Group(records []session.Record) map[string][]session.Record {
	grouped := map[string][]session.Record{}
	for _, record := range records {
		if place, found := PlaceOf(record); found {
			grouped[place.Socket] = append(grouped[place.Socket], record)
		}
	}
	return grouped
}

// Display is the render function and what it needs.
type Display struct {
	Tmux   Tmux
	Glyphs session.Palette
	Logger *slog.Logger
}

// Render is called with the current state, never with a transition (R22).
func (d *Display) Render(view session.View) error {
	for socket, records := range Group(view.Sessions) {
		d.paint(socket, records)
	}
	return nil
}

// paint is one server: one look, and every write it plans in one invocation.
// If tmux refuses that, it is because something moved between the two — a
// pane closed, and the command naming it stopped the rest — so it looks once
// more and tries once more. Past that, the next render starts from a fresh
// look anyway (R13).
func (d *Display) paint(socket string, records []session.Record) {
	for attempt := 1; ; attempt++ {
		server, err := d.Tmux.Look(socket)
		if err != nil {
			// A failed look is what guarantees nothing is written to a server
			// that is not there (R13).
			d.Logger.Debug("skipped a tmux server", "socket", socket, "why", err.Error())
			return
		}
		commands := Plan(socket, records, server, d.Glyphs)
		if len(commands) == 0 {
			return
		}
		err = d.Tmux.Do(socket, chain(commands))
		if err == nil {
			for _, command := range commands {
				d.Logger.Debug("painted", "socket", socket, "what", command.Why)
			}
			return
		}
		if attempt == 2 {
			d.Logger.Warn("tmux refused the marks", "socket", socket, "problem", err.Error())
			return
		}
	}
}
