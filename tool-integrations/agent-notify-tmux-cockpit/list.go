package main

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/lassoColombo/agent-notify/session"
	"github.com/lassoColombo/agent-notify/subscribe"
)

// The list is the tab's left pane, the tab's only program and the only part
// of it that decides anything: which agent the cursor is on, and so which
// pane the stage should show. It never acts on what it remembers. Every time
// something may have moved it looks at the server, and what it does comes
// from what it saw.

const (
	// lookEvery is how often the list looks at the server with nothing to
	// prompt it. It is told of what matters as it happens: the store by the
	// store, a terminal coming to the tab or to a borrowed pane's window by
	// the tab's hooks, which type a key into this pane. This is for whatever no hook announces — a pane
	// closed, the hooks themselves wiped by a configuration that sets one
	// whole — and nothing waits on it to send an agent home, which the
	// server does (R22).
	lookEvery = 5 * time.Second
	// drawEvery is how often the ages in the rows move.
	drawEvery = time.Second
	// settle is how long the cursor rests on an agent before its pane is
	// brought to the stage, so that going down a list of six does not swap,
	// and resize, six agents; and how long a terminal rests on the tab
	// before anything is, so that passing through it on the way to another
	// window does not either.
	settle = 120 * time.Millisecond
)

// The list's messages: a new view of the store, the two clocks, a terminal
// having rested on the tab, the cursor having rested, the server as it was
// found, and a stand-in that could not be drawn.
type (
	storeMsg   session.View
	tickMsg    time.Time
	dueMsg     time.Time
	arrivedMsg int
	settledMsg int
	lookedMsg  struct {
		world  World
		err    error
		desire Desire
	}
	undrawnMsg struct{}
)

type list struct {
	tab    Tab
	glyphs session.Palette

	records  []session.Record
	world    World
	sections []Section
	agents   []Agent
	// background is drawn below the agents and never selected (D-128).
	background []session.Record

	// selected is the pane under the cursor, by id rather than by index, so
	// that the cursor stays on an agent while the list reorders around it.
	selected string
	// staged is the pane asked onto the stage: the selected one, once the
	// cursor has rested on it.
	staged string
	// moves counts the cursor's moves, so that only the last one's rest
	// stages anything; arrivals, the same for a terminal coming to the tab.
	moves, arrivals int
	// drawn is the stand-in as it was last drawn on the stage, so that it is
	// drawn again only to say something else. Forgotten whenever it is seen
	// away from the stage, where its size is another's.
	drawn string

	// One look at the server at a time. What is asked for while one runs is
	// done after it: `next` if it was more than a look, another look if not.
	busy, again bool
	next        func(Desire) (World, error)

	width, height int
}

func runList() error {
	settings, err := Read()
	if err != nil {
		return err
	}
	socket, self, err := here()
	if err != nil {
		return err
	}
	logger := me.Logger()
	tmux := Tmux{Binary: settings.Binary, Socket: socket}

	program := tea.NewProgram(&list{
		tab:    Tab{Tmux: tmux, List: self, Logger: logger, Layout: Layout{ListWidth: settings.ListWidth}},
		glyphs: settings.Glyphs,
	})

	// `open` wrote these as it made the pane. Written again for a list
	// somebody started by hand, in a window that then becomes the tab.
	if _, err := tmux.run("set-option", "-p", "-t", self, roleOption, roleList, ";",
		"set-option", "-w", "-t", self, tabOption, "1"); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		err := subscribe.Run(ctx, me, func(view session.View) error {
			program.Send(storeMsg(view))
			return nil
		})
		if err != nil {
			logger.Warn("cannot watch the store", "problem", err.Error())
			program.Send(tea.Quit())
		}
	}()

	_, err = program.Run()
	// However the list ended, nothing stays borrowed. And if it ended in a
	// way that never gets here, the tab's own hook does the same.
	if closing := Close(tmux, logger); err == nil {
		err = closing
	}
	return err
}

func (l *list) Init() tea.Cmd { return tea.Batch(l.look(), tick(), due()) }

func tick() tea.Cmd {
	return tea.Tick(drawEvery, func(at time.Time) tea.Msg { return tickMsg(at) })
}

func due() tea.Cmd {
	return tea.Tick(lookEvery, func(at time.Time) tea.Msg { return dueMsg(at) })
}

func (l *list) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case storeMsg:
		l.records = msg.Sessions
		return l, l.look()
	case tickMsg:
		return l, tick()
	case dueMsg:
		return l, tea.Batch(l.look(), due())
	case tea.FocusMsg, tea.BlurMsg:
		// the focus moved between the tab's halves, or came to the terminal
		return l, l.woken()
	case arrivedMsg:
		if int(msg) != l.arrivals {
			return l, nil
		}
		return l, l.look()
	case undrawnMsg:
		l.drawn = ""
	case settledMsg:
		if int(msg) != l.moves {
			return l, nil
		}
		l.stage(l.selected)
		return l, l.look()
	case lookedMsg:
		return l, l.looked(msg)
	case tea.WindowSizeMsg:
		l.width, l.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		return l, l.key(msg.String())
	}
	return l, nil
}

// woken is something having moved in the server: a terminal came to the tab,
// or to the window of the pane on the stage, or the tab's panes changed. The
// list looks once it has rested.
func (l *list) woken() tea.Cmd {
	l.arrivals++
	arrivals := l.arrivals
	return tea.Tick(settle, func(time.Time) tea.Msg { return arrivedMsg(arrivals) })
}

// wake is wakeKey as the list reads it.
const wake = "f12"

func (l *list) key(key string) tea.Cmd {
	switch key {
	case wake:
		// typed by the tab's hooks, and by anybody who wants a look now
		return l.woken()
	case "j", "down", "ctrl+j", "ctrl+n", "alt+j", "alt+down":
		return l.moveTo(l.index() + 1)
	case "k", "up", "ctrl+k", "ctrl+p", "alt+k", "alt+up":
		return l.moveTo(l.index() - 1)
	case "g", "home":
		return l.moveTo(0)
	case "G", "end":
		return l.moveTo(len(l.agents) - 1)
	case "enter":
		// Onto the stage, as Alt Tab does; and what is selected is asked for
		// now, rested or not.
		l.staged = l.selected
		return l.ask(l.tab.Enter)
	case "o":
		// To where the agent lives, leaving the tab behind.
		if pane := l.selected; pane != "" {
			return l.ask(func(Desire) (World, error) { return l.tab.GoTo(pane) })
		}
	case "q", "ctrl+c":
		return tea.Quit
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

func (l *list) moveTo(i int) tea.Cmd {
	if len(l.agents) == 0 {
		return nil
	}
	return l.pick(l.agents[max(0, min(i, len(l.agents)-1))].Pane)
}

// pick puts the cursor on a pane, which reaches the stage once the cursor has
// rested there.
func (l *list) pick(pane string) tea.Cmd {
	if pane == l.selected {
		return nil
	}
	l.selected = pane
	l.moves++
	moves := l.moves
	return tea.Tick(settle, func(time.Time) tea.Msg { return settledMsg(moves) })
}

// stage asks a pane onto the stage.
func (l *list) stage(pane string) { l.staged = pane }

// look is one look at the server, and whatever it takes to make the tab show
// what is wanted.
func (l *list) look() tea.Cmd { return l.ask(nil) }

// ask runs `act` — a look, when nil — if nothing is running, and after what
// is running otherwise.
func (l *list) ask(act func(Desire) (World, error)) tea.Cmd {
	if l.busy {
		if act != nil {
			l.next = act
		}
		l.again = true
		return nil
	}
	if act == nil {
		act = l.tab.Reconcile
	}
	l.busy = true
	desire := Desire{Staged: l.staged}
	return func() tea.Msg {
		world, err := act(desire)
		return lookedMsg{world: world, err: err, desire: desire}
	}
}

// looked takes in the server as it was found, and looks again if something
// was asked for meanwhile or the list now wants something else.
func (l *list) looked(msg lookedMsg) tea.Cmd {
	l.busy = false
	var moved tea.Cmd
	if msg.err != nil {
		l.tab.Logger.Warn("cannot read the tmux server", "problem", msg.err.Error())
	} else {
		l.world = msg.world
		moved = tea.Batch(l.rebuild(), l.show())
	}
	next, again := l.next, l.again
	l.next, l.again = nil, false
	if next != nil || again || (Desire{Staged: l.staged}) != msg.desire {
		return tea.Batch(moved, l.ask(next))
	}
	return moved
}

// rebuild regroups the list from the records and the server as last seen. The
// cursor keeps its agent if it is still there, and otherwise takes the one now
// in its place; except while somebody is on the stage, when nothing moves: the
// pane they are typing into stays under their hands, even if its agent has
// just ended, until they come back to the list.
func (l *list) rebuild() tea.Cmd {
	before := l.index()
	l.sections = Sections(Agents(l.records, l.tab.Tmux.Socket, l.world, l.tab.List))
	l.agents = Flatten(l.sections)
	l.background = Background(l.records)
	if l.index() >= 0 {
		return nil
	}
	if _, there := l.world.Panes[l.selected]; there && l.world.StageFocused(l.tab.List) {
		return nil
	}
	if len(l.agents) == 0 {
		l.selected, l.staged = "", ""
		return nil
	}
	first := l.selected == ""
	rested := l.pick(l.agents[max(0, min(before, len(l.agents)-1))].Pane)
	if first {
		// nothing is on the stage to keep there meanwhile
		l.stage(l.selected)
	}
	return rested
}

// show draws the stand-in, when it is on the stage and has something else to
// say: that there is no agent, or why the one selected is not here. Holding a
// place it is drawn for nobody.
func (l *list) show() tea.Cmd {
	tab := l.world.Panes[l.tab.List].Window
	standIns := l.world.StandIns(tab)
	if len(standIns) == 0 {
		return nil
	}
	standIn := standIns[0]
	if standIn.Window != tab {
		l.drawn = ""
		return nil
	}
	showing := Showing{Title: nobody}
	switch {
	case len(l.agents) == 0:
		showing.Lines = []string{"no agents in this tmux server"}
	case l.world.OnScreenAtHome(l.staged, tab):
		// Nothing is taken from under a terminal that is looking at it.
		name := l.nameOf(l.staged, "this agent")
		showing = Showing{Title: name, Lines: []string{
			name + " is on screen in " + l.world.Where(l.staged), "o goes there",
		}}
	}
	// Otherwise an agent is on its way here, and anything said would only
	// flash.
	frame := Frame(showing, standIn.Width, standIn.Height)
	if drawn := standIn.ID + frame; drawn != l.drawn {
		l.drawn = drawn
		return func() tea.Msg {
			if err := l.tab.Tmux.paint(standIn.ID, frame); err != nil {
				l.tab.Logger.Warn("tmux refused", "why", "the stand-in "+standIn.ID+" is drawn", "problem", err.Error())
				return undrawnMsg{}
			}
			return nil
		}
	}
	return nil
}

func (l *list) nameOf(pane, otherwise string) string {
	for _, agent := range l.agents {
		if agent.Pane == pane {
			return session.CleanLine(agent.Record.DisplayName())
		}
	}
	return otherwise
}

func (l *list) View() tea.View {
	view := tea.NewView(Draw(l.sections, l.background, l.selected, l.glyphs, l.width, l.height, time.Now()))
	view.AltScreen = true
	// The focus arriving here is somebody arriving at the tab.
	view.ReportFocus = true
	view.WindowTitle = tabName
	return view
}
