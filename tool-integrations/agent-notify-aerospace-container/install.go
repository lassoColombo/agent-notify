package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
)

// The table this integration needs.
//
// It used to carry `capture-environment = true`, which told the hook to RUN
// this program rather than read variables itself, because the hook has no
// socket and cannot ask (D-39). Every integration is asked now, so the line has
// nothing left to declare and is gone.
//
// `binary` is an ABSOLUTE path, for the reason every integration here states
// one: this program is run by the hook and by the session-watcher, and neither
// has your shell's PATH. A launchd job's is /usr/bin:/bin and nothing else,
// which is how a container can be perfectly correct and never once be found.
func table(program, aerospace string) string {
	written := fmt.Sprintf(`[integration.aerospace-container]
binary = %q

[integration.aerospace-container.settings]
`, program)
	if aerospace == "" {
		// Printed anyway, with the hole visible. A table that looks complete
		// and is not is worse than one that says what is missing.
		return written + "# aerospace was not on PATH when this was printed. Fill it in.\naerospace = \"\"\n"
	}
	return written + fmt.Sprintf("aerospace = %q\n", aerospace)
}

// order is the other half, and it was always the clearer case: which shell is
// outside which is something only the person running them knows (§A11.2).
//
// The install that came before this one said exactly that in a comment and then
// wrote the order anyway, whenever there was no [container] table to collide
// with. That it felt harmless is an argument about likelihood, not about whose
// line it is.
const order = `[container]
order = ["aerospace-container"]
`

// install prints both and changes nothing (D-66).
//
// Everything in agent-notify's config file is the user's to write, and the test
// for whether something belongs there is whether only the user can know the
// answer (§A14, D-57). Whether this container should be running, and where it
// sits among the others, are both exactly that. The table being present is what
// says yes — `enabled` defaults to true and writing the table IS the ask — so
// an install that wrote it was not configuring this container, it was turning
// it on and deciding its nesting.
//
// What this program knows and a person cannot — where its binary is, and that
// it answers `capture-environment` — is still supplied, resolved, exactly. It
// is stated rather than filed.
func install(arguments []string) int {
	return printTable(os.Stdout, os.Stderr, arguments)
}

// printTable is install with its two streams named, because what goes to which
// is the whole ergonomics of this: the tables alone on stdout, so that somebody
// who has already decided can redirect them —
//
//	agent-notify install aerospace-container >> ~/.config/agent-notify/config.toml
//
// — and every word of explanation on stderr, where it cannot end up inside the
// file they are destined for.
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

	// Resolved HERE, because here is the only place it can be: install runs in
	// your shell, where aerospace is on PATH, and everything that runs later
	// does not (D-67).
	aerospace, lookup := exec.LookPath("aerospace")
	fmt.Fprint(out, table(program, aerospace)+"\n"+order)

	where, err := me.ConfigFile()
	if err != nil {
		where = "agent-notify's config file"
	}
	fmt.Fprintf(problems, "\nNothing was written. Put that in %s when you want this\n"+
		"running: the table being there is what turns it on.\n\n"+
		"If you already have a [container] table, add %q to its `order` rather than\n"+
		"adding a second table — two of them is a TOML error. Put it FIRST: a window\n"+
		"manager is outside whatever multiplexer is running inside it.\n", where, Name)

	if lookup != nil {
		fmt.Fprint(problems, "\naerospace is not on this PATH, so the `aerospace` line above "+
			"is empty:\nfill it in with wherever aerospace actually is. No window can be "+
			"focused\nwithout it.\n")
		return 1
	}
	return 0
}
