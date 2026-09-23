package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
)

// The table this integration needs.
//
// No `capture` line: a bar does not care where a session lives, only what it is
// doing. Clicking a chip focuses one, and that is the container's job, asked
// for through `agent-notify focus-session` rather than done here (D-37).
func table(program, sketchybar string) string {
	written := fmt.Sprintf(`[integration.sketchybar]
binary = %q

[integration.sketchybar.settings]
`, program)
	if sketchybar == "" {
		// Printed anyway, with the hole visible. A table that looks complete
		// and is not is worse than one that says what is missing.
		return written + "# sketchybar was not on PATH when this was printed. Fill it in.\nsketchybar = \"\"\n"
	}
	return written + fmt.Sprintf("sketchybar = %q\n", sketchybar)
}

// install prints that table and changes nothing (D-66).
//
// It used to append it to the configuration file. Everything in that file is
// the user's to write, and the test for whether something belongs there is
// whether only the user can know the answer (§A14, D-57) — which is exactly
// what "should this be running" is. The table being present is what says yes,
// since `enabled` defaults to true and writing the table IS the ask, so an
// install that wrote it was not configuring this display, it was turning it on.
//
// What this program knows and a person cannot — where the binary actually is —
// is still supplied, resolved, exactly. It is stated rather than filed.
func install(arguments []string) int {
	return printTable(os.Stdout, os.Stderr, arguments)
}

// printTable is install with its two streams named, because what goes to which
// is the whole ergonomics of this: the table alone on stdout, so that somebody
// who has already decided can redirect it —
//
//	agent-notify install sketchybar >> ~/.config/agent-notify/config.toml
//
// — and every word of explanation on stderr, where it cannot end up inside the
// file the table is destined for.
func printTable(out, problems io.Writer, arguments []string) int {
	if len(arguments) > 0 {
		fmt.Fprintf(problems,
			"%s install takes no options: it prints the table and writes nothing.\n", Name)
		return 2
	}

	program, err := os.Executable()
	if err != nil {
		fmt.Fprintf(problems, "%s install: cannot find my own path: %v\n", Name, err)
		return 1
	}

	// Resolved HERE, because here is the only place it can be: install runs in
	// your shell, where sketchybar is on PATH, and everything that runs later
	// does not (D-67).
	sketchybar, lookup := exec.LookPath("sketchybar")
	fmt.Fprint(out, table(program, sketchybar))

	where, err := me.ConfigFile()
	if err != nil {
		where = "agent-notify's config file"
	}
	fmt.Fprintf(problems, "\nNothing was written. Put that in %s when you want this\n"+
		"running: the table being there is what turns it on.\n\n"+
		"The session-watcher starts it; nothing goes in your sketchybarrc. It puts its\n"+
		"own items on the bar when it connects and takes them off when it stops.\n", where)

	if lookup != nil {
		fmt.Fprint(problems, "\nsketchybar is not on this PATH, so the `sketchybar` line above "+
			"is empty:\nfill it in with wherever sketchybar actually is. Nothing can be "+
			"painted without it.\n")
		return 1
	}
	return 0
}
