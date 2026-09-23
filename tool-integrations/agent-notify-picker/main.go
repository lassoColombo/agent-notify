// Command agent-notify-picker is the picker: every session [agent-notify]
// knows about, what each one last said underneath, and enter to be taken to the
// one you choose.
//
// It is a **display**, in the sense §A10.2 means it: it reads through the SDK,
// declares itself in the config, and renders the answer core computed. What
// makes it unlike the bar and the pane titles is only that it is not a daemon —
// it needs a terminal, so it is run when somebody presses a key and it goes
// away when they have chosen. That is why its table says `enabled = false`: not
// "off", but "not something to start behind my back" (see install.go).
//
// **fzf owns the screen, as a library rather than as a binary.** A first
// version of this was a terminal UI of its own — a model, an update, a view, a
// keymap, a layout, and the three separate bugs that come of measuring columns
// in somebody else's terminal. A second used a fuzzy-finder library whose
// preview is hardcoded to the right half of the window. fzf is imported here
// and driven through channels, which keeps the screen somebody else's problem
// and still leaves the layout, the palette and the preview ours.
//
// The preview is this same program, run again with `--preview`. That is how
// every fzf preview works, and it is what makes the pane a full renderer —
// markdown, metadata and history — rather than a string squeezed through a
// callback.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	fzf "github.com/junegunn/fzf/src"
	"golang.org/x/term"

	agentnotify "github.com/lassoColombo/agent-notify"
	"github.com/lassoColombo/agent-notify/subscribe"
)

// Name is what this integration calls itself: its config table, and the word
// after `agent-notify install`.
const Name = "picker"

// me is this integration: how everything here asks where agent-notify's files
// are, and what its own settings say.
//
// Root is left empty on purpose — empty means "wherever this process's
// environment says", which is right everywhere but a test (D-69).
var me = subscribe.Integration{Name: Name}

// separator divides the key from what is drawn. A tab, because a session's name
// is somebody else's string and may contain anything else.
const separator = "\t"

func now() time.Time { return time.Now().UTC() }

func main() {
	// The config first, because all three jobs below draw with it and each of
	// them is a separate process: the list, a preview fzf runs per row, and a
	// label it runs per move of the cursor. What could not be used comes back
	// as complaints, which only the window has anywhere to put.
	complaints := Apply(me)

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "install":
			os.Exit(install(os.Args[2:]))
		case "--preview":
			// Run by fzf, once per row somebody looks at.
			os.Exit(previewOf(os.Args[2:]))
		case "--label":
			// Run by fzf's `focus` binding, for the preview pane's label.
			os.Exit(labelOf(os.Args[2:]))
		}
	}
	os.Exit(run(os.Args[1:], complaints))
}

func run(arguments []string, complaints []string) int {
	var all, choose bool
	for _, argument := range arguments {
		switch argument {
		case "--all":
			all = true
		case "--print":
			choose = true
		case "--help", "-h":
			usage()
			return 0
		default:
			fmt.Fprintf(os.Stderr, "%s: %q is not an option\n", Name, argument)
			usage()
			return 2
		}
	}

	// One cold read, already ordered by urgency and already corrected for
	// processes that have died — the SDK applies the same liveness decision
	// `agent-notify list` does, so a session killed an hour ago is not offered
	// as working (D-30).
	read := me.Read
	if all {
		read = me.ReadIncludingEnded
	}
	sessions, err := read()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", Name, err)
		return 1
	}
	if len(sessions) == 0 {
		fmt.Fprintln(os.Stderr, "no sessions")
		return 1
	}

	chosen, code := chooseOne(sessions, all, complaints)
	if chosen == nil {
		// Escape and ctrl+c are somebody changing their mind, not a failure.
		return code
	}
	if choose {
		fmt.Println(chosen.Key.String())
		return 0
	}
	return jump(*chosen)
}

// chooseOne runs fzf over the sessions and answers with the one that was
// picked, or nil.
func chooseOne(
	sessions []agentnotify.Record, all bool, complaints []string,
) (*agentnotify.Record, int) {
	me, err := os.Executable()
	if err != nil {
		me = "agent-notify-picker"
	}

	told := func() []string {
		return Arguments(me,
			PreviewWindow(len(sessions), terminalHeight()), Ghost(all), Complaint(complaints))
	}

	options, err := fzf.ParseOptions(true, told())
	if err != nil && rebound {
		// fzf is the only thing that knows what a key is called, so fzf is what
		// checks: a table it will not take is dropped for this program's own
		// and the window opens saying so, rather than a keypress answering with
		// a pane that appears and vanishes.
		complaints = append(complaints, TheKeysWeShippedWith(err))
		options, err = fzf.ParseOptions(true, told())
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", Name, err)
		return nil, 1
	}

	input, output := make(chan string, len(sessions)), make(chan string, 1)
	options.Input, options.Output = input, output
	columns := Measure(sessions)
	for _, record := range sessions {
		input <- record.Key.String() + separator + Row(record, now(), columns)
	}
	close(input)

	code, err := fzf.Run(options)
	close(output)
	if err != nil {
		// Said out loud rather than swallowed. fzf validates some of what it
		// was told here rather than at parse time, and a picker that answers a
		// keypress with a blank screen and exit 0 is the worst way to find
		// that out — which is exactly how this was found out once.
		fmt.Fprintf(os.Stderr, "%s: %v\n", Name, err)
		return nil, 1
	}
	if code != 0 {
		return nil, 0
	}

	picked, ok := <-output
	if !ok {
		return nil, 0
	}
	key, _, _ := strings.Cut(picked, separator)
	for i := range sessions {
		if sessions[i].Key.String() == key {
			return &sessions[i], 0
		}
	}
	return nil, 0
}

// Ghost is what the empty query says, and it is also the only place a run with
// `--all` differs from a run without one.
//
// It replaces a header that said how many sessions there were in gold across
// the top — the single brightest thing on the screen, above every session name.
// The count is a thing fzf already prints, on the prompt line, in
// `--info=inline-right`; what it does not know is which set it is counting.
func Ghost(all bool) string {
	if all {
		return "filter sessions, ended ones included…"
	}
	return "filter sessions…"
}

// PreviewWindow is how the window is divided, and it is computed rather than
// fixed at 72%.
//
// A fraction is the wrong instrument for this. Three sessions in 28% of a tall
// pane is a list with air under it; twelve sessions in the same 28% is a list
// that scrolls while the preview below it has rows to spare. The number of
// sessions is known before fzf is started, so the list gets exactly the rows it
// has rows for — capped at two fifths, because the message is what the window
// is for — and the preview gets the rest.
//
// A height of zero is a terminal that would not say, and then the old fraction
// is the honest answer.
func PreviewWindow(sessions, height int) string {
	const (
		chrome   = 3 // the prompt, the separator, and the preview's own top
		shortest = 8
	)
	if height <= 0 {
		return "down,68%"
	}
	rows := max(height-chrome, 1)
	list := min(max(sessions, 1), max(rows*2/5, 1))
	if preview := rows - list; preview >= shortest {
		return "down," + strconv.Itoa(preview)
	}
	return "down,68%"
}

// terminalHeight is the pane this was opened in, or zero if there is no
// terminal to ask — a `--print` run in a pipe, or a test.
func terminalHeight() int {
	for _, file := range []*os.File{os.Stdout, os.Stderr} {
		if _, rows, err := term.GetSize(int(file.Fd())); err == nil && rows > 0 {
			return rows
		}
	}
	if tty, err := os.Open("/dev/tty"); err == nil {
		defer tty.Close()
		if _, rows, err := term.GetSize(int(tty.Fd())); err == nil && rows > 0 {
			return rows
		}
	}
	return 0
}

// jump is `agent-notify focus-session`, and not a second implementation of
// anything.
//
// Which containers there are, what order they nest in, and what each one
// answers are core's business (§A11.2) — a display that learned any of that
// would be one that disagrees with the bar beside it. Its output is left on the
// terminal on purpose: a jump that fails says why, in the words the container
// that refused it wrote.
func jump(record agentnotify.Record) int {
	core, err := me.CoreBinary()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", Name, err)
		return 1
	}
	command := exec.Command(core, "focus-session", record.Key.String())
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	if err := command.Run(); err != nil {
		return 1
	}
	return 0
}

func usage() {
	fmt.Fprintf(os.Stderr, `%s — every session, and a way into one.

  agent-notify-picker            the live sessions
  agent-notify-picker --all      the ended-but-resumable ones too
  agent-notify-picker --print    answer with the session instead of going to it
  agent-notify-picker install    add it to agent-notify's config, and say how to bind it

Type to filter. Enter goes there; escape and ctrl+c leave.
`, Name)
}

// Arguments is everything fzf is told, in one place and returned rather than
// used, so that a test can hand them to fzf's own parser: a mistyped colour key
// or a preview-window spec fzf does not accept is then a failing test rather
// than a picker that refuses to open on the one keypress somebody makes.
func Arguments(me, previewWindow, ghost, complaint string) []string {
	arguments := []string{
		"--ansi",
		// Top-down, so the most urgent session is the first line read rather
		// than the last. fzf's default grows upward from the prompt, which is
		// right for files and wrong for a list whose order is the point.
		"--layout=reverse",
		// THE RULE THIS PICKER KEEPS: narrow, never reorder. Core computes one
		// urgency order for every display (§A5.5); sorting by match score would
		// move the row somebody is about to press enter on, every keystroke.
		"--no-sort",
		"--delimiter=" + separator,
		// The key is field one and is never drawn: it is what the preview, the
		// label and the jump are addressed by, and it is not something to read.
		// fzf keeps the original line for placeholders, so {1} is still the key
		// even though the row on screen starts at field two.
		"--with-nth=2..",
		"--preview", me + " --preview {1}",
		// HORIZONTAL SPLIT: the preview underneath, not beside. An agent's
		// message is prose and a paragraph in a half-width column is a column
		// of words; the full width is what makes it readable. The size is
		// computed from how many sessions there are — see PreviewWindow.
		// `noinfo`: fzf otherwise prints the scroll position over the top-right
		// corner of the pane, and the pane now starts with the agent's own
		// sentence rather than with a heading nobody minds a number through.
		// The scrollbar says the same thing at the edge instead.
		"--preview-window=" + previewWindow + ",wrap,noinfo,border-top",
		// WHO THIS IS, ONCE. The pane's label carries the name, the canonical
		// state and the age, so the pane's first line can be the thing being
		// decided on rather than a copy of the highlighted row. `bg-` because
		// it must not make moving the cursor wait on a process.
		"--preview-label-pos=3",
		"--bind=focus:bg-transform-preview-label:" + me + " --label {1}",
		// THE SELECTED ROW. Every row arrives pre-coloured under --ansi, so
		// there is nothing left for fzf's fg+ to repaint: the lift has to be
		// the background across the whole row, and the mark has to be a solid
		// rail rather than a glyph in a font that may not have it.
		"--highlight-line",
		"--pointer=\u258c",
		"--marker=\u258c",
		// Nothing to say on the rows that are not current: the pointer marks
		// the current one and the glyph column marks them all.
		"--gutter= ",
		// The chrome. A picker opens over somebody's work and is read for two
		// seconds, so everything here is either a thing to scan or a thing to
		// see past.
		"--prompt=\uf002  ",
		"--ghost=" + ghost,
		"--ellipsis=\u2026",
		"--info=inline-right",
		"--scrollbar=\u258e\u258e",
		"--separator=\u2500",
		"--padding=0,1",
		"--border=none",
		"--color=" + fzfColours(),
	}
	// THE KEYS, which are the user's (see Keymap) and default to this program's
	// own. Scrolling the PREVIEW rather than the list is the decision among
	// them worth defending: the list is three rows and the message under it is
	// a hundred and eleven. It costs fzf's defaults on four keys — ctrl-j/k
	// also move the list (↑↓ and ctrl-n/p still do), ctrl-u clears the query
	// (backspace still does), and ctrl-d quits on an empty query (esc and
	// ctrl-c still do). Each of those has another way; scrolling the preview
	// had none.
	//
	// A keymap that binds nothing at all is not passed at all: `--bind=` is a
	// thing fzf refuses, and somebody who has given every key back has asked
	// for an fzf with its own keys rather than for an error.
	if binds := fzfBinds(); binds != "" {
		arguments = append(arguments, "--bind="+binds)
	}
	// A HEADER, for the one thing worth interrupting a list of sessions with.
	// It is what the README rejected for a session count — the brightest row on
	// the screen spent on a number fzf already prints beside the prompt — and
	// it is right for this: a complaint about the config file is temporary, it
	// is gone the moment the file is fixed, and the alternative was a picker
	// that does not open.
	if complaint != "" {
		arguments = append(arguments, "--header", complaint)
	}
	return arguments
}
