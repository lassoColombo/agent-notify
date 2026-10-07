package main

import (
	"fmt"
	"os"

	"github.com/lassoColombo/agent-notify/subscribe"
)

// program is the executable's name.
const program = "agent-notify-macos-notifier"

// The table this program needs. Both paths are absolute: the session-watcher
// runs this, and the banner it posts runs alerter, and neither has your
// shell's PATH (D-67).
//
// `binary` is back, and that is the whole of what changed here. It means "core
// may run this", and for as long as this program posted its own notifications
// it could not be true: a tap is delivered to the process that posted the
// banner, so there had to be a process, and launchd had to keep it. alerter is
// that process now, so this is an ordinary display that core renders and that
// exits.
func table(program, alerter string) string {
	written := fmt.Sprintf("[integration.%s]\nbinary = %q\n\n[integration.%s.settings]\n",
		Name, program, Name)
	if alerter == "" {
		// Printed anyway, with the hole visible.
		return written + "# alerter was not on PATH when this was printed. Fill it in.\nalerter = \"\"\n"
	}
	return written + fmt.Sprintf("alerter = %q\n", alerter)
}

// install files the table. There is nothing else: no bundle to build, no
// identity to sign it with, and no launch agent to keep anything alive.
func install(arguments []string) int {
	return me.Install(subscribe.Install{
		Tool:  alerterProgram,
		Table: table,
		Advice: "\nalerter is what puts the banner on the screen, and it is not part of this\n" +
			"repository: brew install vjeantet/tap/alerter. Banners arrive under ITS\n" +
			"identity, so System Settings -> Notifications is where they are turned on\n" +
			"and tuned.\n" +
			"\nThis is the only macOS display there is: the menu bar one was deleted (D-87),\n" +
			"so a banner is the whole of what macOS will tell you.\n",
	}, os.Stdout, os.Stderr, arguments)
}

// uninstall removes the drop-in.
func uninstall(arguments []string) int {
	return me.Uninstall(os.Stdout, os.Stderr, arguments)
}
