package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

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

// install builds the bundle, which is this program's own artifact, and prints
// the table and the launch agent, which are the user's to place (D-66).
func install(arguments []string) int {
	return setUp(os.Stdout, os.Stderr, arguments)
}

func setUp(out, problems io.Writer, arguments []string) int {
	flags := flag.NewFlagSet("install", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	app := flags.String("app", "", "where to put the .app bundle (default: ~/Applications)")
	sign := flags.String("sign", "", "code signing identity for the bundle (default: ad-hoc)")
	if err := flags.Parse(arguments); err != nil {
		fmt.Fprintf(problems, program+" install: %v\n", err)
		return 2
	}

	binary, err := os.Executable()
	if err != nil {
		fmt.Fprintf(problems, program+" install: cannot find my own path: %v\n", err)
		return 1
	}
	// So that an install reached through a symlink copies the binary the link
	// points at.
	if resolved, err := filepath.EvalSymlinks(binary); err == nil {
		binary = resolved
	}

	bundle, err := subscribe.DefaultBundle(program, Identifier, binary)
	if err != nil {
		fmt.Fprintf(problems, program+" install: %v\n", err)
		return 1
	}
	bundle.Icon = WriteIconset
	if *app != "" {
		bundle.Path = filepath.Join(*app, filepath.Base(bundle.Path))
	}

	// Read back out of the config when not given, so that re-running install
	// after a rebuild does not quietly go back to ad-hoc.
	bundle.Identity = *sign
	if bundle.Identity == "" {
		var existing Settings
		_ = me.Settings(&existing)
		bundle.Identity = existing.Sign
	}
	known := subscribe.CodeSigningIdentitiesInTheKeychain()
	if bundle.Identity != "" && !slices.Contains(known, bundle.Identity) {
		fmt.Fprintf(problems, program+" install: this keychain has no code "+
			"signing identity called %q.\nIt has: %v\n", bundle.Identity, known)
		return 1
	}

	if err := bundle.Write(); err != nil {
		fmt.Fprintf(problems, program+" install: %v\n", err)
		return 1
	}

	fmt.Fprint(out, theConfigTableToAdd(bundle.Identity))

	where, err := me.ConfigFile()
	if err != nil {
		where = "agent-notify's config file"
	}
	fmt.Fprintf(problems, "\nThe bundle is at %s. Nothing else was written:\n"+
		"put the table above in %s when you want this configurable,\n"+
		"and load the launch agent below when you want it running.\n\n", bundle.Path, where)
	fmt.Fprint(problems, "The bundle holds a COPY of the binary, so run this again after rebuilding.\n\n")
	fmt.Fprintf(problems, "This display runs itself rather than being started by agent-notify:\n"+
		"a menu bar item dies with its process. It watches the store on its own.\n\n%s\n%s\n",
		subscribe.LaunchAgentPlist(Identifier, bundle.PathOfTheBinaryInside()),
		subscribe.HowToLoadTheLaunchAgent(Identifier))
	fmt.Fprint(problems, "\nBanners are a second display, agent-notify-macos-notifications, with a table\n"+
		"of its own — install it too if you want to be interrupted as well as informed.\n")
	return 0
}
