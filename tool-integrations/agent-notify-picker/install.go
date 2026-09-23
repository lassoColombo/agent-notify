package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// The table this integration needs.
//
// `enabled = false` is the whole of the unusual part, and it does not mean off.
// The session-watcher starts every integration that is enabled and has a binary
// (§A9.4), which is right for a bar and wrong for this: a picker needs a
// terminal, and a picker started behind your back is a process with nowhere to
// draw. So the table names the binary — for whatever binds a key to it, and for
// `doctor` — and says plainly that nothing should spawn it.
//
// The binary is written as an ABSOLUTE path for the reason every integration
// here writes one: this is run by a keybinding, whose PATH is not your shell's.
func table(program string) string {
	return fmt.Sprintf(`# The picker is run when you press a key, not supervised: it needs a terminal,
# so `+"`enabled = false`"+` here means "do not start this for me", not "off".
[integration.picker]
binary  = %q
enabled = false
`, program)
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
// its binary is, and that nothing should spawn it.
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

	fmt.Fprint(out, table(program))

	where, err := me.ConfigFile()
	if err != nil {
		where = "agent-notify's config file"
	}
	fmt.Fprintf(problems, "\nNothing was written. Put that in %s, and this in\n"+
		"zellij's config.kdl to open it with a key:\n\n%s\n", where, indent(keybinding(program)))
	return 0
}

func indent(text string) string {
	var out strings.Builder
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		out.WriteString("    " + line + "\n")
	}
	return out.String()
}
