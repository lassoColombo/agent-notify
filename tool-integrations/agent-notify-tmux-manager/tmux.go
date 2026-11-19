package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// The options the tab is held together by. The window ones say which pane is
// which half and what is selected; the pane one is what tmux.conf's Alt Tab
// and Alt v test, so that they do something else in every other tab.
const (
	optionSelected = "@agents_selected"
	optionList     = "@agents_list"
	optionView     = "@agents_view"
	optionRole     = "@agents_role"
	roleList       = "list"
	roleView       = "view"
)

// separator splits the fields of a format. A control character, which no name
// contains, and which tmux prints as itself because every call says -u.
const separator = "\x1f"

// Tmux is the server this program runs in. Every call names it with -S,
// rather than leaning on $TMUX, and says -u: without it a tmux that finds no
// UTF-8 locale prints every control and non-ASCII character as `_`.
type Tmux struct{ Socket string }

// here is this program's server and pane. Both halves of the tab are panes, so
// both variables are always there; outside tmux there is nothing to do.
func here() (Tmux, string, error) {
	socket, found := SocketOf(os.Getenv("TMUX"))
	pane := os.Getenv("TMUX_PANE")
	if !found || !IsPaneID(pane) {
		return Tmux{}, "", errors.New("this runs in a tmux pane, and $TMUX or $TMUX_PANE says it is not in one")
	}
	return Tmux{Socket: socket}, pane, nil
}

// run is one tmux invocation; a `;` argument separates commands within it, as
// on tmux's own command line.
func (t Tmux) run(arguments ...string) (string, error) {
	command := exec.Command("tmux", append([]string{"-u", "-S", t.Socket}, arguments...)...)
	out, err := command.Output()
	if err != nil {
		var exited *exec.ExitError
		if errors.As(err, &exited) {
			return string(out), fmt.Errorf("tmux %s: %s", arguments[0], strings.TrimSpace(string(exited.Stderr)))
		}
		return string(out), err
	}
	return string(out), nil
}

func trimmed(s string) string { return strings.TrimRight(s, "\n") }

// SocketOf is the socket path out of a $TMUX value, `<socket>,<pid>,<session>`,
// cut from the right because the path is whatever TMUX_TMPDIR made it.
func SocketOf(server string) (string, bool) {
	rest, index, found := cutLast(server, ",")
	if !found || !digits(index) {
		return "", false
	}
	socket, pid, found := cutLast(rest, ",")
	if !found || !digits(pid) || socket == "" {
		return "", false
	}
	return socket, true
}

// IsPaneID is `%` and a number, the only form of pane id tmux hands out.
func IsPaneID(pane string) bool {
	number, found := strings.CutPrefix(pane, "%")
	return found && digits(number)
}

func cutLast(s, separator string) (string, string, bool) {
	at := strings.LastIndex(s, separator)
	if at < 0 {
		return s, "", false
	}
	return s[:at], s[at+len(separator):], true
}

func digits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
