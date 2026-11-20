package main

import (
	"errors"
	"os"
	"strings"
	"time"

	"github.com/lassoColombo/agent-notify/tool"
)

// What this program writes into tmux, which is all it remembers: a tab that
// is restarted, or a stand-in left alone, reads the state back from here.
const (
	// tabSession is the hidden session the tab is born in. Every session that
	// shows the tab has it linked, so killing one of those only unlinks it;
	// and since tmux refuses a second session of the same name, making it is
	// also what keeps two tabs from being opened at once.
	tabSession = "_agents"
	tabName    = "agents"

	// tabOption is on the tab's window, for the tmux configuration's keys.
	tabOption = "@agent-notify-tab"
	// roleOption is on the tab's own two panes.
	roleOption  = "@agent-notify-tab-role"
	roleList    = "list"
	roleStandIn = "stand-in"

	// The two a borrowed pane carries while it is away: the pane holding its
	// place, and the session that place is in, where an agent whose place is
	// lost is given a window of its own.
	standInOption     = "@agent-notify-stand-in"
	homeSessionOption = "@agent-notify-home-session"
)

// separator splits the fields of a format: a control character, which no name
// contains, and which tmux prints as itself because every call says -u.
const separator = "\x1f"

// tmuxTimeout bounds one tmux invocation (R18). On expiry the step is given
// up, and the next look at the server decides what is still to do.
const tmuxTimeout = 2 * time.Second

// Tmux is one server. Every call names it with -S rather than leaning on
// $TMUX, and says -u: without it a tmux that finds no UTF-8 locale prints
// every control and non-ASCII character as `_`, the separator included
// (D-123).
type Tmux struct {
	Binary string
	Socket string
}

// run is one tmux invocation; a `;` argument separates commands within it, as
// on tmux's own command line, and a command that fails stops the ones after
// it.
func (t Tmux) run(arguments ...string) (string, error) {
	out, err := tool.Run(t.Binary, tmuxTimeout, append([]string{"-u", "-S", t.Socket}, arguments...)...)
	return strings.TrimRight(string(out.Stdout), "\n"), err
}

// here is the server and pane this process runs in. Outside a pane there is
// nothing for the tab's own two programs to do.
func here() (socket, pane string, err error) {
	socket, found := SocketOf(os.Getenv("TMUX"))
	pane = os.Getenv("TMUX_PANE")
	if !found || !IsPaneID(pane) {
		return "", "", errors.New("this runs in a tmux pane, and $TMUX or $TMUX_PANE says it is not in one")
	}
	return socket, pane, nil
}

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
