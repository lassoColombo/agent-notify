package main

import (
	"fmt"
	"os"

	"github.com/lassoColombo/agent-notify/subscribe"
)

// program is the executable's name, inside the bundle too.
const program = "agent-notify-macos-bar"

// Identifier is the bundle id: the preferences domain the item's position is
// filed under. It must never change, or every person who has placed the item
// loses that placement.
const Identifier = "io.github.lassocolombo.agent-notify-bar"

// The table has no `binary`: core cannot run a menu bar, launchd starts it
// (D-81). What is left is settings.
func theConfigTableToAdd(identity string) string {
	written := fmt.Sprintf("[integration.%s]\n", Name)
	if identity != "" {
		written += fmt.Sprintf("\n[integration.%s.settings]\nsign = %q\n", Name, identity)
	}
	return written
}

// install builds the bundle and prints the table and the launch agent (D-66).
func install(arguments []string) int {
	return me.InstallBundle(subscribe.BundleInstall{
		Program: program, Identifier: Identifier, Icon: WriteIconset, Table: theConfigTableToAdd,
		Advice: "\nBanners are a second display, agent-notify-macos-notifications, with a table\n" +
			"of its own: install it too if you want to be interrupted as well as informed.\n",
	}, os.Stdout, os.Stderr, arguments)
}
