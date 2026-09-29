package main

import (
	"fmt"
	"os"

	"github.com/lassoColombo/agent-notify/subscribe"
)

// The table this program needs. `binary` is an absolute path: the hook and
// the session-watcher run this, and neither has your shell's PATH.
func table(program, zellij string) string {
	written := fmt.Sprintf("[integration.zellij]\nbinary = %q\n\n[integration.zellij.settings]\n", program)
	if zellij == "" {
		// Printed anyway, with the hole visible.
		return written + "# zellij was not on PATH when this was printed. Fill it in.\nzellij = \"\"\n"
	}
	return written + fmt.Sprintf("zellij = %q\n", zellij)
}

// install files the table, with an order that is right only when this is the
// only container (D-85). Which shell is outside which is something only the
// person running them knows (§A11.2), so the rest is advice.
func install(arguments []string) int {
	return me.Install(subscribe.Install{
		Tool: "zellij",
		Table: func(program, zellij string) string {
			return table(program, zellij) + "\n[container]\norder = [\"zellij\"]\n"
		},
		Advice: "\nIf you already have a [container] table, add \"zellij\" to its `order` rather than\n" +
			"adding a second table: two of them is a TOML error. Outermost first: a\n" +
			"window manager before the multiplexer inside it. Leave it out of the order\n" +
			"to have titles without focus.\n",
	}, os.Stdout, os.Stderr, arguments)
}

// uninstall removes the drop-in. The [container] order is yours, so a line
// naming this program there is yours to take out.
func uninstall(arguments []string) int {
	return me.Uninstall(os.Stdout, os.Stderr, arguments)
}
