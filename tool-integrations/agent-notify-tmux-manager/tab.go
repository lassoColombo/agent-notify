package main

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
)

// Tab is the list's hold on tmux: the server, the list's own pane, and how the
// tab is built.
type Tab struct {
	Tmux   Tmux
	List   string
	Layout Layout
	Logger *slog.Logger
}

// rounds bounds one Reconcile. Showing another agent is one step; a tab found
// in pieces takes four or five. A server that keeps changing under it is left
// for the next look rather than chased.
const rounds = 8

// Reconcile makes the tab show what is wanted, a step at a time, looking at
// the server before each one. It returns the server as it last saw it. A step
// tmux refuses is logged and not retried here: the next look decides what is
// still to do.
func (t Tab) Reconcile(desire Desire) (World, error) {
	for round := 0; ; round++ {
		world, err := t.Tmux.Observe()
		if err != nil {
			return World{}, err
		}
		step, more := Next(world, t.List, Want(world, t.List, desire), t.Layout)
		if !more {
			return world, nil
		}
		if round == rounds {
			t.Logger.Warn("the tab did not settle", "still to do", step.Why)
			return world, nil
		}
		t.do(step)
	}
}

func (t Tab) do(step Step) {
	out, err := t.Tmux.run(step.Args...)
	if err != nil {
		t.Logger.Warn("tmux refused", "why", step.Why, "problem", err.Error())
		return
	}
	t.Logger.Debug("done", "what", step.Why)
	if !step.Spawn {
		return
	}
	if !IsPaneID(out) {
		t.Logger.Warn("tmux made a stand-in and did not say which pane it is", "answer", out)
		return
	}
	mark := append(marking(out), ";", "resize-pane", "-t", t.List, "-x", strconv.Itoa(t.Layout.ListWidth))
	if _, err := t.Tmux.run(mark...); err != nil {
		t.Logger.Warn("tmux refused", "why", "marking the stand-in "+out, "problem", err.Error())
	}
}

// Enter puts the focus on the stage, once the stage shows what is wanted.
func (t Tab) Enter(desire Desire) (World, error) {
	world, err := t.Reconcile(desire)
	if err != nil {
		return world, err
	}
	stage, found := world.Stage(t.List)
	if !found {
		return world, nil
	}
	if _, err := t.Tmux.run("select-pane", "-t", stage); err != nil {
		t.Logger.Warn("tmux refused", "why", "the focus goes to the stage", "problem", err.Error())
	}
	return t.Tmux.Observe()
}

// GoTo leaves the tab for the place a pane lives in: everything goes home, and
// then the terminal showing the tab goes to that pane.
func (t Tab) GoTo(pane string) (World, error) {
	world, err := t.Reconcile(Desire{Leaving: true})
	if err != nil {
		return world, err
	}
	arguments := []string{"switch-client"}
	if client := t.viewer(world); client != "" {
		arguments = append(arguments, "-c", client)
	}
	if _, err := t.Tmux.run(append(arguments, "-t", pane)...); err != nil {
		t.Logger.Warn("tmux refused", "why", "going to "+pane, "problem", err.Error())
	}
	return t.Tmux.Observe()
}

// Client is one terminal attached to the server, and what it is showing.
// `list-clients` is asked rather than `display-message -c`, which fills a
// format's session and window from its target, not from the client: for a
// window linked into two sessions it names whichever tmux thinks of first
// [measured on 3.8].
type Client struct {
	Name, Session, Window string
	Width, Height         string
	Activity              int64
}

var clientFormat = strings.Join([]string{
	"#{client_name}", "#{session_id}", "#{window_id}", "#{window_width}", "#{window_height}", "#{client_activity}",
}, separator)

func (t Tmux) Clients() ([]Client, error) {
	out, err := t.run("list-clients", "-F", clientFormat)
	if err != nil {
		return nil, err
	}
	var clients []Client
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(line, separator)
		if len(fields) != 6 {
			continue
		}
		activity, _ := strconv.ParseInt(fields[5], 10, 64)
		clients = append(clients, Client{
			Name: fields[0], Session: fields[1], Window: fields[2],
			Width: fields[3], Height: fields[4], Activity: activity,
		})
	}
	return clients, nil
}

// latest is the most recently used of the clients that `fits` lets through,
// which is the one a key was just typed in.
func latest(clients []Client, fits func(Client) bool) (Client, bool) {
	var best Client
	found := false
	for _, client := range clients {
		if fits(client) && (!found || client.Activity > best.Activity) {
			best, found = client, true
		}
	}
	return best, found
}

// viewer is the terminal showing the tab, which is the one a key in the list
// was typed in.
func (t Tab) viewer(world World) string {
	clients, err := t.Tmux.Clients()
	if err != nil {
		return ""
	}
	tab := world.Panes[t.List].Window
	client, _ := latest(clients, func(client Client) bool { return client.Window == tab })
	return client.Name
}

// Open is the key that shows the tab: `client` is the terminal it was pressed
// in. The tab is made if there is none, linked into that terminal's session
// if it is not there yet, and selected; pressed on the tab itself, it goes
// back to the window before. `list` is the command a new tab's list runs.
func Open(tmux Tmux, client string, list []string) error {
	clients, err := tmux.Clients()
	if err != nil {
		return err
	}
	asker, found := latest(clients, func(one Client) bool { return client == "" || one.Name == client })
	if !found {
		return fmt.Errorf("no terminal %q is attached to this tmux server", client)
	}
	session, window, width, height := asker.Session, asker.Window, asker.Width, asker.Height

	world, err := tmux.Observe()
	if err != nil {
		return err
	}
	tab, found := world.List()
	if !found {
		if tab, err = create(tmux, list, width, height); err != nil {
			return err
		}
	}

	if tab.Window == window {
		// No window before this one is nothing to go back to, not a failure.
		_, _ = tmux.run("last-window", "-t", session)
		return nil
	}
	var arguments []string
	if !linkedInto(tab.Views, session) {
		arguments = append(arguments, "link-window", "-d", "-s", tab.Window, "-t", session+":", ";")
	}
	_, err = tmux.run(append(arguments, "select-window", "-t", session+":"+tab.Window)...)
	return err
}

func linkedInto(views []View, session string) bool {
	for _, view := range views {
		if view.SessionID == session {
			return true
		}
	}
	return false
}

// create makes the tab in its hidden session, the size of the window it will
// first be shown over, and returns its list pane. The pane is marked in the
// invocation that makes it, so the tab exists from the first moment anybody
// can see the session. tmux refuses a second session of that name: if the key
// was pressed twice, the second finds the first one's tab; and if what is left
// of an old tab holds the name, the new one is a window beside it.
func create(tmux Tmux, list []string, width, height string) (Pane, error) {
	target := "=" + tabSession + ":"
	session := append([]string{"new-session", "-d", "-s", tabSession, "-n", tabName, "-x", width, "-y", height}, list...)
	_, refused := tmux.run(append(session, ";",
		"set-option", "-p", "-t", target, roleOption, roleList, ";",
		"set-option", "-w", "-t", target, tabOption, "1")...)
	if refused != nil {
		if world, err := tmux.Observe(); err == nil {
			if tab, found := world.List(); found {
				return tab, nil
			}
		}
		pane, err := tmux.run(append([]string{"new-window", "-d", "-t", target, "-n", tabName,
			"-P", "-F", "#{pane_id}"}, list...)...)
		if err != nil {
			return Pane{}, fmt.Errorf("cannot make the tab: %w (and before that: %v)", err, refused)
		}
		if _, err := tmux.run("set-option", "-p", "-t", pane, roleOption, roleList, ";",
			"set-option", "-w", "-t", pane, tabOption, "1"); err != nil {
			return Pane{}, err
		}
	}
	world, err := tmux.Observe()
	if err != nil {
		return Pane{}, err
	}
	tab, found := world.List()
	if !found {
		return Pane{}, fmt.Errorf("the tab was made and is not there")
	}
	return tab, nil
}

// SendHome gives back everything borrowed, whoever asks: `restore`, and the
// first half of closing the tab.
func SendHome(tmux Tmux, logger *slog.Logger) (World, error) {
	world, err := tmux.Observe()
	if err != nil {
		return World{}, err
	}
	for _, step := range Leaving(world) {
		if _, err := tmux.run(step.Args...); err != nil {
			logger.Warn("tmux refused", "why", step.Why, "problem", err.Error())
			continue
		}
		logger.Debug("done", "what", step.Why)
	}
	return world, nil
}

// Close sends everything home and then closes the tab's own two panes, the
// stand-ins first: no agent is in the tab's window by the time it goes. A pane
// somebody else put there keeps the window open, and is left alone.
func Close(tmux Tmux, logger *slog.Logger) error {
	world, err := SendHome(tmux, logger)
	if err != nil {
		return err
	}
	var lists []string
	for _, pane := range world.sorted() {
		switch pane.Role {
		case roleStandIn:
			_, _ = tmux.run("kill-pane", "-t", pane.ID)
		case roleList:
			lists = append(lists, pane.ID)
		}
	}
	for _, pane := range lists {
		_, _ = tmux.run("set-option", "-w", "-u", "-t", pane, tabOption, ";", "kill-pane", "-t", pane)
	}
	return nil
}
