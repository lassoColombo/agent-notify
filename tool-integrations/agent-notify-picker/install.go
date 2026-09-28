package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// The table this integration needs, which is a heading and nothing else.
//
// **No `binary`, and the absence is the declaration.** Naming one means "core
// may run this" — the hook runs it on the agent's path, the session-watcher
// runs it to render or to focus — and a picker needs a terminal, so a picker
// started behind your back is a process with nowhere to draw. That used to be
// said with `enabled = false`, which was a workaround rather than a statement:
// the rule was "started if enabled and has a binary", so the only way to say
// "do not start this" was to say "this is off", about a thing that was on.
//
// Where the path goes instead is the keybinding below, which is the only thing
// that runs this and is where a person would look for it. It is ABSOLUTE for
// the reason every integration here writes one: a keybinding's PATH is not your
// shell's.
//
// The table is still worth writing. It carries the settings, and it is what
// makes this appear in `doctor` as something of yours rather than as a program
// on your PATH that nobody asked for.
func table() string {
	return `# Run when you press a key, and never by core: it has no binary here, which is
# what says so. The path lives in the keybinding, which is the thing that runs it.
[integration.picker]
`
}

// keybinding is how zellij is asked to open it: a floating pane that closes
// when the picker exits, which is what makes enter feel like a jump rather than
// like starting a program.
//
// It is PRINTED rather than written. A zellij config is KDL, it is full of
// somebody's own comments and their own key choices, and an integration that
// rewrote it would be guessing at both.
func keybinding(program string) string {
	return fmt.Sprintf(`bind "Alt a" {
    Run %q {
        name "agents"
        floating true
        close_on_exit true
        width "90%%"
        height "90%%"
        x "5%%"
        y 1
    }
}`, program)
}

// install prints what this needs and changes nothing (D-66).
//
// Everything in agent-notify's config file is the user's to write, and the test
// for whether something belongs there is whether only the user can know the
// answer (§A14, D-57). That has always been obvious for the keybinding — this
// never wrote your zellij config, it showed you the block — and it is the same
// answer for the table: whether a picker should be set up at all is yours.
//
// What this program knows and a person cannot, it still supplies exactly: where
// its binary is, resolved, in the one place that needs it.
func install(arguments []string) int {
	return printTable(os.Stdout, os.Stderr, arguments)
}

// printTable is install with its two streams named, because what goes to which
// is the whole ergonomics of this: the table alone on stdout, so that somebody
// who has already decided can redirect it —
//
//	agent-notify install picker >> ~/.config/agent-notify/config.toml
//
// — and the keybinding and the explanation on stderr, where neither can end up
// inside a TOML file. The keybinding is KDL and belongs in a different file
// altogether, which is the sharpest form of the same point.
func printTable(out, problems io.Writer, arguments []string) int {
	if len(arguments) > 0 {
		fmt.Fprintf(problems,
			"%s install takes no options: it prints what it needs and writes nothing.\n", Name)
		return 2
	}

	program, err := os.Executable()
	if err != nil {
		fmt.Fprintf(problems, "%s install: cannot find my own path: %v\n", Name, err)
		return 1
	}

	fmt.Fprint(out, table())

	where, err := me.ConfigFile()
	if err != nil {
		where = "agent-notify's config file"
	}
	fmt.Fprintf(problems, "\nNothing was written. Put that in %s, and this in\n"+
		"zellij's config.kdl to open it with a key:\n\n%s\n"+
		"\nThe path is in the keybinding and not in the table, because a table with a\n"+
		"binary in it is one core may run, and nothing core runs has a terminal.\n",
		where, indent(keybinding(program)))
	return 0
}

func indent(text string) string {
	var out strings.Builder
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		out.WriteString("    " + line + "\n")
	}
	return out.String()
}
