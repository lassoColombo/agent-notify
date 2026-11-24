package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/lassoColombo/agent-notify/container"
)

// NotAttached: the server is running, the pane is there, and no terminal is
// attached to the server. Its next move is `tmux attach`, which is neither
// "prune the record" nor "start tmux", so it is its own word (§A11.3).
const NotAttached = container.Problem("not-attached")

// Asker is where a focus was asked from, when that is inside the pane's own
// server: the pane it was asked from, if it was asked from one, and the
// session `$TMUX` names. A popup — the picker's — has `$TMUX` and no
// `$TMUX_PANE` [measured on 3.8]; for it the session is the one of the
// terminal that opened it.
type Asker struct {
	Pane, Session string
}

// askedFrom is this process's own place, if it is in the pane's server: the
// same socket, and the same server on it.
func askedFrom(place Place) Asker {
	socket, pid, sessionID, found := serverOf(os.Getenv(serverVariable))
	if !found || socket != place.Socket || pid != place.Server {
		return Asker{}
	}
	asker := Asker{Session: sessionID}
	if pane := os.Getenv(paneVariable); IsPaneID(pane) {
		asker.Pane = pane
	}
	return asker
}

// Choose is the terminal a focus moves. A focus is somebody wanting to see a
// pane, and what has to change is the screen in front of them, so it is
// decided which screen that is before anything else:
//
//   - asked from inside the server, the terminal it was asked from: the one
//     showing the pane it was asked from, or, from a popup, one on the
//     session it was opened in. Even when another terminal already shows the
//     agent: that one is not the screen in front of the person asking (D-123).
//   - asked from anywhere else (a banner, a script), the terminal that has
//     the focus, and failing that the one used last, which is the one tmux
//     itself would pick.
//
// Of several that qualify, the one used last.
func Choose(clients []Client, asker Asker) (Client, bool) {
	if asker.Pane != "" {
		if client, found := latest(clients, func(c Client) bool { return c.Pane == asker.Pane }); found {
			return client, true
		}
	}
	if asker.Session != "" {
		if client, found := latest(clients, func(c Client) bool { return c.Session == asker.Session }); found {
			return client, true
		}
	}
	if client, found := latest(clients, func(c Client) bool { return c.Focused }); found {
		return client, true
	}
	return latest(clients, func(Client) bool { return true })
}

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

// Through is the session a terminal is sent through to see a pane, of those
// the pane's window is linked into: the terminal's own when it is one of
// them, else the one a terminal was attached to last. A session nobody was
// ever attached to — the agents tab's hidden `_agents` — is the last choice,
// and a terminal is never left in one while another holds the window.
//
// It is named because tmux, given a pane alone, picks the session that was
// active last, and the agents tab's hidden session is active whenever the tab
// is made [measured on 3.8].
func Through(views []View, client Client) (View, bool) {
	var best View
	found := false
	for _, view := range views {
		if view.Session == client.Session {
			return view, true
		}
		if !found || view.LastAttached > best.LastAttached {
			best, found = view, true
		}
	}
	return best, found
}

// Focus brings the pane in front of one terminal, then reads back that it is.
// tmux answers once it has acted, so nothing is waited for; but an exit code
// is not evidence that anybody's screen changed.
//
// `switch-client -t <session>:<window>.<pane>` is the session, the window and
// the pane in one command, unzooming a window zoomed on another pane
// [measured on 3.8].
func Focus(tmux Tmux, coordinates json.RawMessage) (container.Outcome, error) {
	place, ours := placeIn(coordinates)
	if !ours {
		return container.Failed(container.NeverPlaced, "these are not this program's coordinates"), nil
	}
	server, err := tmux.Look(place.Socket)
	if err != nil {
		return container.Failed(container.NotRunning, err.Error()), nil
	}
	if gone := goneFrom(server, place); gone != "" {
		return container.Failed(container.PlaceIsGone, gone), nil
	}

	client, found := Choose(server.Clients, askedFrom(place))
	if !found {
		return container.Failed(NotAttached, fmt.Sprintf(
			"no terminal is attached to tmux server %q, so there is nothing here to bring — "+
				"`tmux -S %s attach` first", place.Socket, place.Socket)), nil
	}
	if client.Pane == place.Pane {
		return container.Done(), nil
	}
	through, _ := Through(server.Views[place.Pane], client)

	target := through.Session + ":" + through.Window + "." + place.Pane
	if err := tmux.Do(place.Socket, []string{"switch-client", "-c", client.Name, "-t", target}); err != nil {
		return container.Failed(container.Refused, err.Error()), nil
	}
	server, err = tmux.Look(place.Socket)
	if err != nil {
		return container.Failed(container.NotRunning, err.Error()), nil
	}
	for _, now := range server.Clients {
		if now.Name == client.Name && now.Pane == place.Pane && now.Session == through.Session {
			return container.Done(), nil
		}
	}
	return container.Failed(container.Refused, fmt.Sprintf(
		"tmux took switch-client and terminal %s does not show pane %s through session %s",
		client.Name, place.Pane, through.Session)), nil
}

// Focused answers whether a person is looking at the pane. Anything that
// cannot be established is `cannot-tell` rather than `no` (R27).
func Focused(tmux Tmux, coordinates json.RawMessage) (container.Verdict, error) {
	place, ours := placeIn(coordinates)
	if !ours {
		return container.Verdict{Answer: container.CannotTell,
			Detail: "these are not this program's coordinates"}, nil
	}
	server, err := tmux.Look(place.Socket)
	if err != nil {
		return container.Verdict{Answer: container.CannotTell, Detail: err.Error()}, nil
	}
	return Verdict(server, place), nil
}

// Verdict is the pure half of Focused: the pane is in front when a terminal
// shows it and that terminal has the focus. What no format says is whether a
// popup is open over it.
func Verdict(server Server, place Place) container.Verdict {
	if gone := goneFrom(server, place); gone != "" {
		return container.Verdict{Answer: container.No, Detail: gone}
	}
	detail := fmt.Sprintf("no terminal shows pane %s", place.Pane)
	for _, client := range server.Clients {
		if client.Pane != place.Pane {
			continue
		}
		if client.Focused {
			return container.Verdict{Answer: container.Yes}
		}
		detail = fmt.Sprintf("terminal %s shows pane %s, and is not the one with the focus", client.Name, place.Pane)
	}
	return container.Verdict{Answer: container.No, Detail: detail}
}

// goneFrom says why the pane is not in this server, or nothing if it is.
func goneFrom(server Server, place Place) string {
	if server.PID != place.Server {
		return fmt.Sprintf("tmux server %q has been started again since pane %s was in it", place.Socket, place.Pane)
	}
	if _, there := server.Panes[place.Pane]; !there {
		return fmt.Sprintf("pane %s is not in tmux server %q any more", place.Pane, place.Socket)
	}
	return ""
}
