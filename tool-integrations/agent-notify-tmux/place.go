package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/lassoColombo/agent-notify/container"
)

// Coordinates is what `interpret-environment` returns. The session and window
// are here even though they are re-read at focus time, because "pane %12 of
// window 2 in session home" is something a person can check.
type Coordinates struct {
	Socket     string `json:"socket"`
	Pane       string `json:"pane"`
	Session    string `json:"session,omitempty"`
	Window     int    `json:"window"`
	WindowName string `json:"window_name,omitempty"`
}

func (c Coordinates) ours() bool { return c.Socket != "" && IsPaneID(c.Pane) }

// Interpret asks tmux which session and window hold the captured pane.
func Interpret(tmux Tmux, captured json.RawMessage) (any, error) {
	var given Captured
	if err := json.Unmarshal(captured, &given); err != nil {
		return nil, fmt.Errorf("what was captured is not this program's: %w", err)
	}
	if given.Server == "" || given.Pane == "" {
		return nil, fmt.Errorf("this session was not started inside a tmux pane")
	}
	socket, found := SocketOf(given.Server)
	if !found {
		return nil, fmt.Errorf("%s was %q, which does not name a server", serverVariable, given.Server)
	}
	if !IsPaneID(given.Pane) {
		return nil, fmt.Errorf("%s was %q, which is not a pane id", paneVariable, given.Pane)
	}

	rows, err := tmux.Panes(socket)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row.Pane == given.Pane {
			return Coordinates{
				Socket: socket, Pane: given.Pane,
				Session: row.SessionName, Window: row.WindowIndex, WindowName: row.WindowName,
			}, nil
		}
	}
	return nil, fmt.Errorf("pane %s is not in tmux server %q any more", given.Pane, socket)
}

// NotAttached: the server is running, the pane is there, and nobody is looking
// at any session that holds it. Its next move is `tmux attach`, which is
// neither "prune the record" nor "start tmux", so it is its own word (§A11.3).
const NotAttached = container.Problem("not-attached")

// Focus brings the pane to the front, then reads the state back.
//
// tmux answers a command once it has done it, so unlike zellij nothing is
// waited for; but the state is still read back afterwards, because an exit
// code is not evidence that anybody's screen changed. Unlike zellij, tmux can
// move any of a server's clients from anywhere, so a focus asked from outside
// tmux (a notification under launchd) still has a terminal to bring (D-123).
// In the order they are tried:
//
//   - Asked from inside the same server (the picker in a pane or popup): the
//     terminal that asked goes there with `switch-client`. It comes first
//     because it is the screen the person is looking at, even when some
//     other terminal already shows the pane's session.
//   - Somebody is attached to a session holding the pane: that session's
//     window and pane are brought forward, in that terminal.
//   - Somebody is attached to the server at all: the most recently active
//     client, the one tmux itself would pick, goes there.
//   - Nobody is: there is nothing here to bring, and the answer says how to
//     attach.
func Focus(tmux Tmux, coordinates json.RawMessage) (container.Outcome, error) {
	var where Coordinates
	if err := json.Unmarshal(coordinates, &where); err != nil || !where.ours() {
		return container.Failed(container.NeverPlaced,
			"these are not this program's coordinates"), nil
	}

	rows, err := tmux.Panes(where.Socket)
	if err != nil {
		return container.Failed(container.NotRunning, err.Error()), nil
	}
	views := rowsOf(rows, where.Pane)
	if len(views) == 0 {
		return container.Failed(container.PlaceIsGone,
			fmt.Sprintf("pane %s is not in tmux server %q any more", where.Pane, where.Socket)), nil
	}
	if Verdict(rows, where.Pane).Answer == container.Yes {
		return container.Done(), nil
	}

	if outcome, acted := bring(tmux, where, views); !acted {
		return outcome, nil
	}

	rows, err = tmux.Panes(where.Socket)
	if err != nil {
		return container.Failed(container.NotRunning, err.Error()), nil
	}
	if verdict := Verdict(rows, where.Pane); verdict.Answer != container.Yes {
		return container.Failed(container.Refused, fmt.Sprintf(
			"tmux took the focus commands and pane %s is still not in front: %s",
			where.Pane, verdict.Detail)), nil
	}
	return container.Done(), nil
}

// bring runs the first of Focus's cases that applies. It answers false with
// the outcome to return when it did nothing, and true once tmux has acted.
func bring(tmux Tmux, where Coordinates, views []Row) (container.Outcome, bool) {
	here := os.Getenv(serverVariable)
	if sameServer(here, where.Socket) {
		client, found, err := clientOf(tmux, where.Socket, here)
		if err != nil {
			return container.Failed(container.NotRunning, err.Error()), false
		}
		if found {
			return switchTo(tmux, where, client)
		}
	}

	if watched, anybody := firstAttached(views); anybody {
		if err := tmux.SelectWindow(where.Socket, watched.SessionID, watched.WindowID); err != nil {
			return container.Failed(container.Refused, err.Error()), false
		}
		if err := tmux.SelectPane(where.Socket, where.Pane); err != nil {
			return container.Failed(container.Refused, err.Error()), false
		}
		return container.Outcome{}, true
	}

	client, found, err := latestClient(tmux, where.Socket)
	if err != nil {
		return container.Failed(container.NotRunning, err.Error()), false
	}
	if !found {
		return notAttached(where, views), false
	}
	return switchTo(tmux, where, client)
}

func switchTo(tmux Tmux, where Coordinates, client string) (container.Outcome, bool) {
	if err := tmux.SwitchClient(where.Socket, client, where.Pane); err != nil {
		return container.Failed(container.Refused, err.Error()), false
	}
	return container.Outcome{}, true
}

func notAttached(where Coordinates, views []Row) container.Outcome {
	name := views[0].SessionName
	return container.Failed(NotAttached, fmt.Sprintf(
		"tmux session %q is running and no terminal is attached to its server, so there "+
			"is nothing here to bring — `tmux -S %s attach -t %s` first",
		name, where.Socket, name))
}

// Focused answers whether that pane is the one a person is looking at.
// Anything it cannot establish is `cannot-tell` rather than `no`: a wrong `no`
// silences a notification that should have fired (R27).
func Focused(tmux Tmux, coordinates json.RawMessage) (container.Verdict, error) {
	var where Coordinates
	if err := json.Unmarshal(coordinates, &where); err != nil || !where.ours() {
		return container.Verdict{Answer: container.CannotTell,
			Detail: "these are not this program's coordinates"}, nil
	}

	rows, err := tmux.Panes(where.Socket)
	if err != nil {
		return container.Verdict{Answer: container.CannotTell, Detail: err.Error()}, nil
	}
	return Verdict(rows, where.Pane), nil
}

// Verdict is the pure half of Focused. A pane is in front when some session
// holding it has a client on it, has its window as the current one, and has it
// as that window's active pane. Every client of one session sees the same
// window, so the row says it all; what no format says is whether a popup is
// open over it, and a popup is gone the moment its command exits.
func Verdict(rows []Row, pane string) container.Verdict {
	views := rowsOf(rows, pane)
	if len(views) == 0 {
		return container.Verdict{Answer: container.No,
			Detail: fmt.Sprintf("pane %s is not there", pane)}
	}

	detail := ""
	for _, row := range views {
		switch {
		case row.Attached == 0:
			detail = fmt.Sprintf("nobody is attached to session %q", row.SessionName)
		case !row.WindowActive:
			detail = fmt.Sprintf("window %d is not the current one of session %q",
				row.WindowIndex, row.SessionName)
		case !row.PaneActive:
			detail = fmt.Sprintf("pane %s is not the active pane of window %d", pane, row.WindowIndex)
		default:
			return container.Verdict{Answer: container.Yes}
		}
	}
	return container.Verdict{Answer: container.No, Detail: detail}
}

// rowsOf is every row of one pane: one per session its window is linked into.
func rowsOf(rows []Row, pane string) []Row {
	var views []Row
	for _, row := range rows {
		if row.Pane == pane {
			views = append(views, row)
		}
	}
	return views
}

func firstAttached(views []Row) (Row, bool) {
	for _, row := range views {
		if row.Attached > 0 {
			return row, true
		}
	}
	return Row{}, false
}

// sameServer is whether this program was started inside the server the pane
// is in. Empty for every caller but a terminal or a popup.
func sameServer(here, socket string) bool {
	mine, found := SocketOf(here)
	return found && mine == socket
}

// latestClient is the server's most recently active client, which is the one
// tmux would pick itself for a command that names none.
func latestClient(tmux Tmux, socket string) (string, bool, error) {
	clients, err := tmux.Clients(socket)
	if err != nil {
		return "", false, err
	}
	var best *Client
	for i, client := range clients {
		if best == nil || client.Activity > best.Activity {
			best = &clients[i]
		}
	}
	if best == nil {
		return "", false, nil
	}
	return best.Name, true, nil
}

// clientOf is the terminal this program was started from: the client on its
// own session, the most recently active one if there are several, which is the
// one tmux would pick itself.
func clientOf(tmux Tmux, socket, here string) (string, bool, error) {
	session, found := SessionOf(here)
	if !found {
		return "", false, nil
	}
	clients, err := tmux.Clients(socket)
	if err != nil {
		return "", false, err
	}
	var best *Client
	for i, client := range clients {
		if client.SessionID == session && (best == nil || client.Activity > best.Activity) {
			best = &clients[i]
		}
	}
	if best == nil {
		return "", false, nil
	}
	return best.Name, true, nil
}
