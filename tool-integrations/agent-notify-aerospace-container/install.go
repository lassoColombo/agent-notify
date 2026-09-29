package main

import (
	"fmt"
	"os"

	"github.com/lassoColombo/agent-notify/subscribe"
)

// The table this program needs. `binary` is an absolute path: the hook and
// the session-watcher run this, and neither has your shell's PATH.
func table(program, aerospace string) string {
	written := fmt.Sprintf("[integration.aerospace-container]\nbinary = %q\n\n[integration.aerospace-container.settings]\n", program)
	if aerospace == "" {
		// Printed anyway, with the hole visible.
		return written + "# aerospace was not on PATH when this was printed. Fill it in.\naerospace = \"\"\n"
	}
	return written + fmt.Sprintf("aerospace = %q\n", aerospace)
}

// install prints the table and the order and changes nothing (D-66). Which
// shell is outside which is something only the person running them knows
// (§A11.2), so the order is offered, never written.
func install(arguments []string) int {
	return me.Install(subscribe.Install{
		Tool: "aerospace",
		Table: func(program, aerospace string) string {
			return table(program, aerospace) + "\n[container]\norder = [\"aerospace-container\"]\n"
		},
		Advice: "\nIf you already have a [container] table, add \"aerospace-container\" to its `order`\n" +
			"rather than adding a second table: two of them is a TOML error. Put it FIRST: a\n" +
			"window manager is outside whatever multiplexer is running inside it.\n",
	}, os.Stdout, os.Stderr, arguments)
}
