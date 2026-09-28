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

// The theConfigTableToAdd this integration needs.
//
// It has no `binary`, and the absence is the declaration. `binary` means "core
// may run this" — the hook runs it, the session-watcher runs it — and neither
// can run a menu bar: an NSStatusItem dies with the process that made it, and
// AppKit wants a main thread and a run loop it can keep. So this display is not
// something core starts. launchd starts it, and it connects on its own, which
// §A10.2 has always allowed.
//
// What is left in the table is settings, which is what a table is for. The
// binary inside the bundle is named in the launch agent instead, and that is
// the whole point of the bundle (see bundle.go): run the bare binary and the
// item's position is forgotten on every restart. What sits at that path is a
// COPY, not a symlink, because codesign refuses a symlinked executable outright
// — so a rebuilt binary does not reach the bundle on its own and `install` has
// to run again, which is why it is idempotent.
//
// Nothing about capturing either: a menu bar does not care where a session
// lives, only what it is doing. Choosing a row focuses one, and that is the
// container's job, asked for through `agent-notify focus-session` rather than
// done here (D-37).
func theConfigTableToAdd(identity string) string {
	written := fmt.Sprintf("[integration.%s]\n", Name)
	if identity != "" {
		written += fmt.Sprintf("\n[integration.%s.settings]\nsign = %q\n", Name, identity)
	}
	return written
}

// install builds the bundle and prints the table (D-66).
//
// The two halves are deliberately not alike, and the line between them is the
// one D-66 drew. The bundle is this program's own artifact — macOS will not
// remember where you put a menu bar item without one, and there is no other way for it to exist, so building
// it is mechanical and install does it. Agent-notify's config file is not this
// program's: everything in it is the user's to write, and the test for whether
// something belongs there is whether only the user can know the answer (§A14,
// D-57). Whether this display should be running is exactly that, and since
// `enabled` defaults to true, writing the table IS turning it on.
//
// So the table is stated rather than filed, and so is the launch agent — with
// the one path a person could not work out for themselves, the binary inside
// the bundle, already resolved in it. Loading a launch agent puts a program in
// your login session for ever, which is even less a thing a program should
// arrange for itself while you are still reading its output.
func install(arguments []string) int {
	return setUp(os.Stdout, os.Stderr, arguments)
}

// setUp is install with its two streams named: the table alone on stdout, so
// that somebody who has already decided can redirect it —
//
//	agent-notify install macos-bar >> ~/.config/agent-notify/config.toml
//
// — and everything else on stderr, where it cannot end up inside the file the
// table is destined for.
func setUp(out, problems io.Writer, arguments []string) int {
	flags := flag.NewFlagSet("install", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	app := flags.String("app", "", "where to put the .app bundle (default: ~/Applications)")
	sign := flags.String("sign", "", "code signing identity for the bundle (default: ad-hoc, "+
		"which cannot notify — see the README)")
	if err := flags.Parse(arguments); err != nil {
		fmt.Fprintf(problems, program+" install: %v\n", err)
		return 2
	}

	binary, err := os.Executable()
	if err != nil {
		fmt.Fprintf(problems, program+" install: cannot find my own path: %v\n", err)
		return 1
	}
	// Resolved, so that an `install` reached through a symlink on PATH copies
	// the binary the link points at rather than the link itself.
	if resolved, err := filepath.EvalSymlinks(binary); err == nil {
		binary = resolved
	}

	bundle, err := DefaultBundle(binary)
	if err != nil {
		fmt.Fprintf(problems, program+" install: %v\n", err)
		return 1
	}
	if *app != "" {
		bundle.Path = filepath.Join(*app, filepath.Base(bundle.Path))
	}

	// The identity is read back out of the config when it is not given, because
	// install is re-run after every rebuild and having to remember a flag each
	// time is how a bundle quietly goes back to ad-hoc. Reading is not writing:
	// the value is the user's, and it is theirs to have put there.
	bundle.Identity = *sign
	if bundle.Identity == "" {
		var existing Settings
		_ = me.Settings(&existing)
		bundle.Identity = existing.Sign
	}
	known := CodeSigningIdentitiesInTheKeychain()
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
		"a menu bar item dies with its process, so there is nothing core could\n"+
		"usefully run. It watches `agent-notify tail --json` instead.\n\n%s\n%s\n",
		subscribe.LaunchAgentPlist(Identifier, bundle.PathOfTheBinaryInside()),
		subscribe.HowToLoadTheLaunchAgent(Identifier))

	fmt.Fprint(problems, "\nBanners are a second display, agent-notify-macos-notifications, with a table\n"+
		"of its own — install it too if you want to be interrupted as well as informed.\n")
	return 0
}
