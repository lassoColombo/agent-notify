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
// (D-81). `launch-agent` is how doctor asks launchd whether it did.
func theConfigTableToAdd(identity string) string {
	written := fmt.Sprintf("[integration.%s]\nlaunch-agent = %q\n", Name, Identifier)
	if identity != "" {
		written += fmt.Sprintf("\n[integration.%s.settings]\nsign = %q\n", Name, identity)
	}
	return written
}

// install builds the bundle, files the table, and loads the launch agent
// (D-85). Run again after a rebuild: that is the upgrade.
func install(arguments []string) int {
	return me.InstallBundle(bundle, os.Stdout, os.Stderr, arguments)
}

// uninstall stops the launch agent and removes it, the bundle and the table.
func uninstall(arguments []string) int {
	return me.UninstallBundle(bundle, os.Stdout, os.Stderr, arguments)
}

var bundle = subscribe.BundleInstall{
	Program: program, Identifier: Identifier, Icon: WriteIconset, Table: theConfigTableToAdd,
	Advice: "\nBanners are a second display, agent-notify-macos-notifications, with a table\n" +
		"of its own: install it too if you want to be interrupted as well as informed.\n",
}
