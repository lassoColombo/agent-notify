package main

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lassoColombo/agent-notify/tool"
)

// What this program writes into tmux, which is all it remembers: a tab that
// is restarted, `restore`, and the hooks the server runs by itself all read
// the state back from here.
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

	// What a borrowed pane carries while it is away: the pane holding its
	// place, and the session that place is in, where an agent whose place is
	// lost is given a window of its own. And, when the window it lives in was
	// taking its name from it, that window, whose `automatic-rename` is held
	// off until the pane is back: the name would otherwise be the stand-in's.
	standInOption     = "@agent-notify-stand-in"
	homeSessionOption = "@agent-notify-home-session"
	renamedOption     = "@agent-notify-renamed"

	// hookName is the name the tab's hooks are stored under, each a member of
	// the hook it is on, so that they are replaced and removed without
	// touching anybody else's.
	hookName = "agent-notify-tab"

	// wakeKey is the key that makes the list look at the server, and the one
	// the tab's hooks type into its pane (`send-keys`) to say that something
	// moved. A key rather than a `wait-for` channel, which is what tmux has
	// for this, because neither form of one both keeps every signal and
	// stays quiet: of two signals nobody was waiting for the second cancels
	// the first, and of two unlocks the second is an error, which a hook
	// shows on the status line [measured on 3.8]. Keys wait in the pane's
	// terminal until they are read.
	wakeKey = "F12"
)

// events are the hooks under which a terminal can come to show another
// window: a session changing its window, a terminal changing its session,
// arriving or going, and two windows trading places, which changes what a
// session's current index holds and fires none of the others [measured on
// 3.8].
var events = []string{
	"session-window-changed", "client-session-changed",
	"client-attached", "client-detached", "after-swap-window",
}

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

// paint writes bytes into an empty pane, as if a program in it had printed
// them: `display-message -I` reads them from stdin.
func (t Tmux) paint(pane string, frame string) error {
	_, err := tool.RunWithInput(t.Binary, tmuxTimeout, []byte(frame),
		"-u", "-S", t.Socket, "display-message", "-I", "-t", pane)
	return err
}

// here is the server and pane this process runs in. Outside a pane there is
// nothing for the list to do.
func here() (socket, pane string, err error) {
	socket, found := SocketOf(os.Getenv("TMUX"))
	pane = os.Getenv("TMUX_PANE")
	if !found || !IsPaneID(pane) {
		return "", "", errors.New("this runs in a tmux pane, and $TMUX or $TMUX_PANE says it is not in one")
	}
	return socket, pane, nil
}

// SocketOf is the socket path out of a $TMUX value.
func SocketOf(server string) (string, bool) {
	socket, _, found := ServerOf(server)
	return socket, found
}

// ServerOf is the socket path and the server's pid out of a $TMUX value,
// `<socket>,<pid>,<session>`, cut from the right because the path is whatever
// TMUX_TMPDIR made it. The pid says which server: one started again on the
// same socket hands pane ids out again from `%0` (D-126).
func ServerOf(server string) (socket string, pid int, found bool) {
	rest, index, found := cutLast(server, ",")
	if !found || !digits(index) {
		return "", 0, false
	}
	socket, number, found := cutLast(rest, ",")
	if !found || !digits(number) || socket == "" {
		return "", 0, false
	}
	pid, err := strconv.Atoi(number)
	if err != nil || pid <= 0 {
		return "", 0, false
	}
	return socket, pid, true
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
