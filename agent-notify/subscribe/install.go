package subscribe

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
)

// Install is what a tool-integration's `install` prints: its table, with the
// path of this program and of the tool it drives filled in, then advice. It
// writes nothing, because everything in agent-notify's config file is the
// user's (D-66).
type Install struct {
	// Tool is the program this integration drives, looked up on PATH here
	// because install runs in your shell and nothing that runs later does
	// (D-67). Empty when there is none.
	Tool string
	// Table is the table to print, given this program's absolute path and the
	// tool's, which is "" when the tool was not found.
	Table func(program, tool string) string
	// Advice follows the table, on stderr.
	Advice string
}

// Install prints it: the table alone on stdout, so that somebody who has
// decided can redirect it into the file, and every word of explanation on
// stderr where it cannot end up inside that file. It takes no options. The
// exit code is 1 when the tool was not found, and the table says so too.
func (i Integration) Install(install Install, out, problems io.Writer, arguments []string) int {
	if len(arguments) > 0 {
		fmt.Fprintf(problems, "%s install takes no options: it prints what it needs and writes nothing.\n", i.Name)
		return 2
	}
	program, err := os.Executable()
	if err != nil {
		fmt.Fprintf(problems, "%s install: cannot find my own path: %v\n", i.Name, err)
		return 1
	}

	tool, lookup := "", error(nil)
	if install.Tool != "" {
		tool, lookup = exec.LookPath(install.Tool)
	}
	i.PrintTable(out, problems, install.Table(program, tool))
	fmt.Fprint(problems, install.Advice)
	if lookup != nil {
		fmt.Fprintf(problems, "\n%s is not on this PATH, so the `%s` line above is empty: fill it in\n"+
			"with wherever %s actually is.\n", install.Tool, install.Tool, install.Tool)
		return 1
	}
	return 0
}

// BundleInstall is what a display that owns its process builds and prints: a
// `.app` around a copy of this binary, which macOS gives it no choice about,
// then its table and the launch agent that starts it, which are the user's to
// place (D-66, D-81).
type BundleInstall struct {
	// Program is the executable's name, inside the bundle too.
	Program string
	// Identifier is the bundle id, which must never change.
	Identifier string
	// Icon draws the app icon into an .iconset directory.
	Icon func(iconset string) bool
	// Table is the table to print, given the signing identity it should
	// remember so that the next install does not need the flag again.
	Table func(identity string) string
	// Advice follows the launch agent, on stderr.
	Advice string
}

// InstallBundle takes `--app DIR` for where the bundle goes and `--sign
// IDENTITY` for what signs it; an identity not given is read back out of the
// config, and one the keychain does not have is refused before anything is
// built.
func (i Integration) InstallBundle(bundleInstall BundleInstall, out, problems io.Writer, arguments []string) int {
	flags := flag.NewFlagSet("install", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	app := flags.String("app", "", "where to put the .app bundle (default: ~/Applications)")
	sign := flags.String("sign", "", "code signing identity for the bundle (default: ad-hoc)")
	if err := flags.Parse(arguments); err != nil {
		fmt.Fprintf(problems, "%s install: %v\n", bundleInstall.Program, err)
		return 2
	}

	binary, err := os.Executable()
	if err != nil {
		fmt.Fprintf(problems, "%s install: cannot find my own path: %v\n", bundleInstall.Program, err)
		return 1
	}
	// So that an install reached through a symlink copies the binary the link
	// points at.
	if resolved, err := filepath.EvalSymlinks(binary); err == nil {
		binary = resolved
	}

	bundle, err := DefaultBundle(bundleInstall.Program, bundleInstall.Identifier, binary)
	if err != nil {
		fmt.Fprintf(problems, "%s install: %v\n", bundleInstall.Program, err)
		return 1
	}
	bundle.Icon = bundleInstall.Icon
	if *app != "" {
		bundle.Path = filepath.Join(*app, filepath.Base(bundle.Path))
	}
	bundle.Identity = *sign
	if bundle.Identity == "" {
		if opened, err := i.core(); err == nil {
			bundle.Identity, _ = opened.Settings.Integration[i.Name].Settings["sign"].(string)
		}
	}
	known := CodeSigningIdentitiesInTheKeychain()
	if bundle.Identity != "" && !slices.Contains(known, bundle.Identity) {
		fmt.Fprintf(problems, "%s install: this keychain has no code signing identity called %q.\nIt has: %v\n",
			bundleInstall.Program, bundle.Identity, known)
		return 1
	}
	if err := bundle.Write(); err != nil {
		fmt.Fprintf(problems, "%s install: %v\n", bundleInstall.Program, err)
		return 1
	}

	fmt.Fprint(out, bundleInstall.Table(bundle.Identity))
	where, err := i.ConfigFile()
	if err != nil {
		where = "agent-notify's config file"
	}
	fmt.Fprintf(problems, "\nThe bundle is at %s. Nothing else was written:\n"+
		"put the table above in %s when you want this configurable,\n"+
		"and load the launch agent below when you want it running.\n\n"+
		"The bundle holds a COPY of the binary, so run this again after rebuilding.\n\n"+
		"This display runs itself rather than being started by agent-notify, and\n"+
		"watches the store on its own.\n\n%s\n%s\n%s",
		bundle.Path, where,
		LaunchAgentPlist(bundleInstall.Identifier, bundle.PathOfTheBinaryInside()),
		HowToLoadTheLaunchAgent(bundleInstall.Identifier), bundleInstall.Advice)
	return 0
}
