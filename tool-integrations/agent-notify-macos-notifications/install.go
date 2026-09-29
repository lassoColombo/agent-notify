package main

import (
	"fmt"
	"os"

	"github.com/lassoColombo/agent-notify/subscribe"
)

// program is the executable's name, inside the bundle too.
const program = "agent-notify-macos-notifications"

// Identifier is the bundle id: what macOS files its notification decision
// against. A decision made about an identifier cannot be unmade, so it must
// never change, and it is deliberately not the menu bar's.
const Identifier = "io.github.lassocolombo.agent-notify-notifications"

// The table has no `binary`: a tap on a banner is answered on the posting
// process's main thread, so this keeps its process and launchd keeps it
// (D-81). `launch-agent` is how doctor asks launchd whether it did.
func theConfigTableToAdd(identity string) string {
	written := fmt.Sprintf("[integration.%s]\nlaunch-agent = %q\n", Name, Identifier)
	if identity != "" {
		written += fmt.Sprintf("\n[integration.%s.settings]\nsign = %q\n", Name, identity)
	}
	return written
}

// install builds the bundle, files the table, and loads the launch agent
// (D-85). Run again after a rebuild: that is the upgrade. The bundle must be
// signed: an unsigned or bundle-less program cannot post a notification at
// all, and is refused silently.
func install(arguments []string) int {
	return me.InstallBundle(bundle, os.Stdout, os.Stderr, arguments)
}

// uninstall stops the launch agent and removes it, the bundle and the table.
func uninstall(arguments []string) int {
	return me.UninstallBundle(bundle, os.Stdout, os.Stderr, arguments)
}

var bundle = subscribe.BundleInstall{
	Program: program, Identifier: Identifier, Icon: WriteIconset, Table: theConfigTableToAdd,
	Advice: "\nThe bundle must be SIGNED (--sign IDENTITY): an unsigned one cannot post a\n" +
		"notification at all, and is refused silently.\n" +
		"\nThe menu bar item is a second display, agent-notify-macos-bar, with a table\n" +
		"of its own: install it too if you want to be informed as well as interrupted.\n",
}
