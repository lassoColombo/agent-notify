package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/lassoColombo/agent-notify/tool"
)

// Aerospace is the only thing here that runs anything.
//
// Like zellij's container half, this program is run rather than connected: core
// spawns it with a subcommand when it needs an answer (D-38). So there is no
// daemon and no cache — every invocation asks aerospace afresh, which is also
// what §A11.3 requires of coordinates at the moment of use. Measured at about
// 12ms per invocation, which is what makes asking every time affordable.
type Aerospace struct {
	Binary  string
	Timeout time.Duration
}

// Window is the part of `list-windows` this needs.
//
// The field names are aerospace's own interpolation variables, which is why
// they are spelled with hyphens: `--json --format` prints exactly the variables
// the format names, under those names. Asking for the format is not decoration
// — `--json` alone answers with app-name, window-id and window-title and no
// pid, and the pid is half of how a window is recognised here.
type Window struct {
	ID    int    `json:"window-id"`
	App   int    `json:"app-pid"`
	Name  string `json:"app-name"`
	Title string `json:"window-title"`
}

// format is what every listing asks for. It is one string because the two
// listings must agree: a window the resolution chose and a focused window it is
// compared against have to be the same shape.
const format = "%{window-id}%{app-pid}%{app-name}%{window-title}"

// Windows is every window aerospace knows about, on every monitor and every
// workspace — including the ones nobody can currently see, which is the point:
// a session in a window on another workspace is exactly the case focus exists
// for.
func (a Aerospace) Windows() ([]Window, error) {
	return a.list("--all")
}

// Focused is the window in front, or false if aerospace says there is none.
func (a Aerospace) Focused() (Window, bool, error) {
	windows, err := a.list("--focused")
	if err != nil {
		return Window{}, false, err
	}
	if len(windows) == 0 {
		return Window{}, false, nil
	}
	return windows[0], true, nil
}

func (a Aerospace) list(which string) ([]Window, error) {
	out, err := a.run("list-windows", which, "--json", "--format", format)
	if err != nil {
		return nil, err
	}
	var windows []Window
	if err := json.Unmarshal(out, &windows); err != nil {
		return nil, fmt.Errorf("aerospace list-windows answered %q, which is not a window list",
			tool.Summarise(out))
	}
	return windows, nil
}

// Focus brings one window to the front.
//
// [verified 2026-09-18, aerospace 0.20.3] One command does the whole job: a
// window on another workspace comes with its workspace, and focusing the window
// that is already focused exits 0 rather than complaining — unlike zellij's
// focus-pane-id, which exits 2 at the same request. So there is nothing to ask
// first and nothing to make idempotent by hand.
func (a Aerospace) Focus(id int) error {
	_, err := a.run("focus", "--window-id", strconv.Itoa(id))
	return err
}

// run is one `aerospace` invocation under this program's timeout.
//
// The running is [tool.Run], which is where the WaitDelay, the check order and
// the cleaning of whatever aerospace printed all live (D-68).
//
// [verified 2026-09-18] aerospace tells the truth with its exit code: a window
// id it does not know exits 1 and says so, and so does a usage mistake. There
// is no stderr vocabulary to keep here, which is why nothing is read out of
// the failure beyond what tool.Run already says about it.
func (a Aerospace) run(args ...string) ([]byte, error) {
	out, err := tool.Run(a.Binary, a.Timeout, args...)
	return out.Stdout, err
}

// TheAerospaceToRun is the path out of the configuration, checked.
//
// There is no search here and no PATH fallback, and the absence is the design
// (D-67). This program is run by the session-watcher, whose PATH is not your
// shell's — a launchd job's is /usr/bin:/bin and nothing else — so a lookup
// performed HERE is performed in the one context that cannot answer it. The
// list of Homebrew prefixes that used to sit at this line was an attempt to
// guess what the environment would not say.
//
// The lookup happens at install instead, which runs in your shell, where the
// answer is simply available.
func TheAerospaceToRun(given string) (string, error) {
	const key = "[integration." + Name + ".settings] aerospace"
	switch {
	case given == "":
		return "", fmt.Errorf("%s is not set — run `agent-notify install %s`, which finds "+
			"aerospace in your own shell and prints the line to add", key, Name)
	case !filepath.IsAbs(given):
		return "", fmt.Errorf("%s = %q must be an absolute path: a supervised child's PATH "+
			"is not yours, so a bare name means something different here than it does to you",
			key, given)
	}
	if _, err := os.Stat(given); err != nil {
		return "", fmt.Errorf("%s = %q: %w", key, given, err)
	}
	return given, nil
}
