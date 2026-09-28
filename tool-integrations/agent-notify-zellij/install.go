package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
)

// The table this program needs. `binary` is an absolute path: the hook and
// the session-watcher run this, and neither has your shell's PATH.
func table(program, zellij string) string {
	written := fmt.Sprintf(`[integration.zellij]
binary = %q

[integration.zellij.settings]
`, program)
	if zellij == "" {
		// Printed anyway, with the hole visible. A table that looks complete
		// and is not is worse than one that says what is missing.
		return written + "# zellij was not on PATH when this was printed. Fill it in.\nzellij = \"\"\n"
	}
	return written + fmt.Sprintf("zellij = %q\n", zellij)
}

// order is the other half: which shell is outside which is something only
// the person running them knows (§A11.2).
const order = `[container]
order = ["zellij"]
`

// install prints both and changes nothing (D-66).
func install(arguments []string) int {
	return printTable(os.Stdout, os.Stderr, arguments)
}

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

	// Resolved here because install runs in your shell, where zellij is on
	// PATH, and everything that runs later does not (D-67).
	zellij, lookup := exec.LookPath("zellij")
	me.PrintTable(out, problems, table(program, zellij)+"\n"+order)
	fmt.Fprintf(problems, "\nIf you already have a [container] table, add %q to its `order` rather than\n"+
		"adding a second table — two of them is a TOML error. Outermost first: a\n"+
		"window manager before the multiplexer inside it. Leave it out of the order\n"+
		"to have titles without focus.\n", Name)
	return whetherZellijWasFound(problems, lookup)
}

func whetherZellijWasFound(problems io.Writer, lookup error) int {
	if lookup == nil {
		return 0
	}
	fmt.Fprint(problems, "\nzellij is not on this PATH, so the `zellij` line above is empty:\n"+
		"fill it in with wherever zellij actually is. Nothing can be focused without it.\n")
	return 1
}
