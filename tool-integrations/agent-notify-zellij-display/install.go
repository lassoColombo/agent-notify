package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
)

// The table this integration needs in agent-notify's config file.
//
// It is one line now. There used to be a second, `capture-environment = true`,
// which told the hook to run this program inside the agent — only a process in
// there can read which pane it is in — and it is gone because every integration
// is asked that now, so there is nothing left to say yes to.
//
// Before that it was `capture = ["ZELLIJ_SESSION_NAME", "ZELLIJ_PANE_ID"]`, and
// the two names are why it is not any more (D-57): they are already in this
// program's source, where `capture-environment` and `PlaceOf` both read them,
// and a third copy in a file the user edits is a copy that can disagree with
// the other two — silently, because a capture naming the wrong variable
// produces no error, just a display that paints nothing.
//
// `binary` is written as an ABSOLUTE path. It is what core will run — the hook
// on the agent's path, and the session-watcher — and neither has your shell's
// PATH. A launchd job's is /usr/bin:/bin and nothing else, which is how a
// display can be perfectly correct and never once be started.
func table(program, zellij string) string {
	written := fmt.Sprintf(`[integration.zellij-display]
binary = %q

[integration.zellij-display.settings]
`, program)
	if zellij == "" {
		// Printed anyway, with the hole visible. A table that looks complete
		// and is not is worse than one that says what is missing.
		return written + "# zellij was not on PATH when this was printed. Fill it in.\nzellij = \"\"\n"
	}
	return written + fmt.Sprintf("zellij = %q\n", zellij)
}

// install prints that table and changes nothing (D-66).
//
// It used to append it to the configuration file, and the reason it does not
// any more is §A14's own rule read one step further: everything in that file is
// the user's to write, and the test for whether something belongs there is
// whether only the user can know the answer. Whether this display should be
// running at all is exactly such a thing — and the table being present is what
// says yes, since `enabled` defaults to true and writing the table IS the ask.
// An install that wrote it did not configure this display, it turned it on.
//
// What the program knows and a person cannot — where this binary actually is,
// and that it answers `capture-environment` — is still supplied here, exactly,
// resolved. It is stated rather than filed.
func install(arguments []string) int {
	return printTable(os.Stdout, os.Stderr, arguments)
}

// printTable is install with its two streams named, because what goes to which
// is the whole ergonomics of this: the table alone on stdout, so that somebody
// who has already decided can redirect it —
//
//	agent-notify install zellij-display >> ~/.config/agent-notify/config.toml
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
	// your shell, where zellij is on PATH, and everything that runs later does
	// not (D-67).
	zellij, lookup := exec.LookPath("zellij")
	fmt.Fprint(out, table(program, zellij))

	where, err := me.ConfigFile()
	if err != nil {
		where = "agent-notify's config file"
	}
	fmt.Fprintf(problems, "\nNothing was written. Put that in %s when you want this\n"+
		"running: the table being there is what turns it on.\n", where)

	if lookup != nil {
		fmt.Fprintf(problems, "\nzellij is not on this PATH, so the `zellij` line above is "+
			"empty:\nfill it in with wherever zellij actually is. This display cannot\n"+
			"start without it.\n")
		return 1
	}
	return 0
}
