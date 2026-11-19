package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/lassoColombo/agent-notify/session"
	"github.com/lassoColombo/agent-notify/subscribe"
)

// Rosé Pine, as the picker draws it: the four state hues mark a state and
// nothing else, names are the neutral ramp so they read the same whatever the
// agent is doing, and the selected row sits on overlay with a bright rose bar.
var (
	base02 = lipgloss.Color("#26233a") // overlay — the selected row
	base03 = lipgloss.Color("#6e6a86") // muted — counts, rules
	base04 = lipgloss.Color("#8c88a6") // subtle — where, age
	base05 = lipgloss.Color("#e0def4") // text — names
	base08 = lipgloss.Color("#eb6f92") // love — needs you
	base09 = lipgloss.Color("#f6c177") // gold — broke
	base0C = lipgloss.Color("#9ccfd8") // foam — finished a turn
	base13 = lipgloss.Color("#fff0ee") // bright rose — the pointer
	base14 = lipgloss.Color("#609fbb") // bright pine — working
)

var hues = map[session.Kernel]lipgloss.Style{
	session.BlockedOnYou:  lipgloss.NewStyle().Foreground(base08),
	session.Broke:         lipgloss.NewStyle().Foreground(base09),
	session.FinishedATurn: lipgloss.NewStyle().Foreground(base0C),
	session.Working:       lipgloss.NewStyle().Foreground(base14),
	session.Idle:          lipgloss.NewStyle().Foreground(base04),
}

// glyphs are agent-notify-tmux's, so a state looks the same on a pane frame
// and here.
var glyphs = map[session.Kernel]string{
	session.BlockedOnYou:  "",
	session.Broke:         "",
	session.FinishedATurn: "",
	session.Working:       "",
	session.Idle:          "",
}

func hueOf(kernel session.Kernel) lipgloss.Style {
	if hue, known := hues[kernel]; known {
		return hue
	}
	return lipgloss.NewStyle().Foreground(base04)
}

// The list's messages: a new view of the store, a new reading of where the
// server's panes are, and the clock that rereads them.
type (
	storeMsg session.View
	panesMsg map[string]string
	tickMsg  time.Time
)

type list struct {
	tmux       Tmux
	self, view string

	records []session.Record
	panes   map[string]string

	sections []Section
	agents   []Agent
	// selected is the pane under the cursor, by id rather than by index, so
	// that the cursor stays on an agent while the list reorders around it.
	selected string
	// published is what @agents_selected was last set to.
	published string

	width, height int
}

func runList() error {
	t, self, err := here()
	if err != nil {
		return err
	}
	view, _ := t.run("display-message", "-p", "-t", self, "#{"+optionView+"}")
	model := &list{tmux: t, self: self, view: trimmed(view), panes: map[string]string{}}
	program := tea.NewProgram(model)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		err := subscribe.Run(ctx, watching, func(view session.View) error {
			program.Send(storeMsg(view))
			return nil
		})
		if err != nil {
			program.Send(tea.Quit())
		}
	}()
	_, err = program.Run()
	return err
}

func (l *list) Init() tea.Cmd { return tea.Batch(l.readPanes(), tick()) }

func tick() tea.Cmd {
	return tea.Tick(2*time.Second, func(at time.Time) tea.Msg { return tickMsg(at) })
}

// readPanes is where every pane of the server is now. The store says which
// pane an agent was started in; only tmux says the pane is still there.
func (l *list) readPanes() tea.Cmd {
	return func() tea.Msg {
		out, err := l.tmux.run("list-panes", "-a", "-F", "#{pane_id}"+separator+"#{session_name}:#{window_index}")
		if err != nil {
			return nil
		}
		return panesMsg(PanesOf(out))
	}
}

func (l *list) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case storeMsg:
		l.records = msg.Sessions
		return l, l.rebuild()
	case panesMsg:
		l.panes = msg
		return l, l.rebuild()
	case tickMsg:
		return l, tea.Batch(l.readPanes(), tick())
	case tea.WindowSizeMsg:
		l.width, l.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		return l, l.key(msg.String())
	}
	return l, nil
}

func (l *list) key(key string) tea.Cmd {
	switch key {
	case "j", "down", "ctrl+j", "ctrl+n", "alt+j", "alt+down":
		return l.move(1)
	case "k", "up", "ctrl+k", "ctrl+p", "alt+k", "alt+up":
		return l.move(-1)
	case "g", "home":
		return l.moveTo(0)
	case "G", "end":
		return l.moveTo(len(l.agents) - 1)
	case "enter":
		// Into the mirror, as Alt Tab does.
		return l.tmuxCmd("select-pane", "-t", l.view)
	case "o":
		// To where the agent lives, leaving this tab behind.
		if l.selected != "" {
			return l.tmuxCmd("switch-client", "-t", l.selected)
		}
	case "q":
		return l.tmuxCmd("kill-window", "-t", l.self)
	}
	return nil
}

func (l *list) index() int {
	for i, agent := range l.agents {
		if agent.Pane == l.selected {
			return i
		}
	}
	return -1
}

func (l *list) move(step int) tea.Cmd { return l.moveTo(l.index() + step) }

func (l *list) moveTo(i int) tea.Cmd {
	if len(l.agents) == 0 {
		return nil
	}
	l.selected = l.agents[max(0, min(i, len(l.agents)-1))].Pane
	return l.publish()
}

// rebuild regroups the list after the store or the panes moved. The cursor
// keeps its agent if it is still there, and otherwise takes the one now in
// its place.
func (l *list) rebuild() tea.Cmd {
	before := l.index()
	own := map[string]bool{l.self: true, l.view: true}
	l.sections = Sections(Agents(l.records, l.tmux.Socket, l.panes, own))
	l.agents = Flatten(l.sections)
	if l.index() < 0 {
		l.selected = ""
		if len(l.agents) > 0 {
			l.selected = l.agents[max(0, min(before, len(l.agents)-1))].Pane
		}
	}
	return l.publish()
}

// publish tells the view what to mirror, and names the view's frame after it.
func (l *list) publish() tea.Cmd {
	if l.selected == l.published {
		return nil
	}
	l.published = l.selected
	name := ""
	if i := l.index(); i >= 0 {
		name = l.agents[i].Record.DisplayName()
	}
	if l.selected == "" {
		return l.tmuxCmd("set-option", "-w", "-u", "-t", l.self, optionSelected, ";",
			"set-option", "-p", "-u", "-t", l.view, "@name")
	}
	return l.tmuxCmd("set-option", "-w", "-t", l.self, optionSelected, l.selected, ";",
		"set-option", "-p", "-t", l.view, "@name", name)
}

func (l *list) tmuxCmd(arguments ...string) tea.Cmd {
	return func() tea.Msg {
		_, _ = l.tmux.run(arguments...)
		return nil
	}
}

func (l *list) View() tea.View {
	view := tea.NewView(l.render(time.Now()))
	view.AltScreen = true
	return view
}

// render is the list: per section a header with its count, then two lines per
// agent, its name and age and then where it is, scrolled to keep the cursor
// on screen.
func (l *list) render(now time.Time) string {
	width := max(l.width, 20)
	if len(l.agents) == 0 {
		return lipgloss.NewStyle().Foreground(base04).Render(" no agents in this tmux server")
	}

	var lines []string
	cursor := 0
	for i, section := range l.sections {
		if i > 0 {
			lines = append(lines, "")
		}
		hue := hueOf(section.Kernel)
		header := hue.Bold(true).Render(" "+glyphs[section.Kernel]+" "+section.Title) +
			lipgloss.NewStyle().Foreground(base03).Render(fmt.Sprintf(" %d", len(section.Agents)))
		lines = append(lines, header)
		for _, agent := range section.Agents {
			if agent.Pane == l.selected {
				cursor = len(lines)
			}
			lines = append(lines, l.row(agent, width, now)...)
		}
	}

	height := max(l.height, 1)
	top := 0
	if cursor+2 > height {
		top = cursor + 2 - height
	}
	end := min(len(lines), top+height)
	return strings.Join(lines[top:end], "\n")
}

// row is one agent: a bar if it is selected, its name, its age at the far
// edge, and under it the session:window and directory it is in.
func (l *list) row(agent Agent, width int, now time.Time) []string {
	selected := agent.Pane == l.selected
	bar, base := "  ", lipgloss.NewStyle()
	if selected {
		bar = lipgloss.NewStyle().Foreground(base13).Background(base02).Render("▌ ")
		base = base.Background(base02)
	}
	age := session.Ago(agent.Record.Elapsed(now))
	room := max(width-lipgloss.Width(bar)-lipgloss.Width(age)-1, 1)

	name := base.Foreground(base05).Bold(selected).Width(room).Render(fit(agent.Record.DisplayName(), room))
	when := base.Foreground(base04).Render(" " + age)

	where := agent.Where
	if leaf := leafOf(agent.Record.Cwd); leaf != "" {
		where += "  " + leaf
	}
	under := base.Foreground(base04).Width(width - lipgloss.Width(bar)).Render(fit(where, width-lipgloss.Width(bar)))
	return []string{bar + name + when, bar + under}
}

func leafOf(path string) string {
	path = strings.TrimRight(path, "/")
	if at := strings.LastIndex(path, "/"); at >= 0 {
		return path[at+1:]
	}
	return path
}

// fit cuts text to a width with an ellipsis. lipgloss wraps rather than
// truncates, and a row that wraps tears the list in two.
func fit(text string, width int) string {
	text = strings.Join(strings.Fields(text), " ")
	if width <= 0 || lipgloss.Width(text) <= width {
		return text
	}
	var kept []rune
	for _, r := range text {
		if lipgloss.Width(string(append(kept, r)))+1 > width {
			break
		}
		kept = append(kept, r)
	}
	return string(kept) + "…"
}
