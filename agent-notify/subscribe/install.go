package subscribe

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/lassoColombo/agent-notify/internal/config"
)

// WriteDropIn files this integration's table in `conf.d`, replacing what it
// wrote before, and says where (D-85). The user's own file is never touched.
func (i Integration) WriteDropIn(table string) (string, error) {
	layout, err := i.layout()
	if err != nil {
		return "", err
	}
	return config.WriteDropIn(layout, i.Name, table)
}

// RemoveDropIn takes the table away again.
func (i Integration) RemoveDropIn() error {
	layout, err := i.layout()
	if err != nil {
		return err
	}
	return config.RemoveDropIn(layout, i.Name)
}

// Install is what a tool-integration's `install` files: its table, with the
// path of this program and of the tool it drives filled in, then advice.
type Install struct {
	// Tool is the program this integration drives, looked up on PATH here
	// because install runs in your shell and nothing that runs later does
	// (D-67). Empty when there is none.
	Tool string
	// Table is the table to file, given this program's absolute path and the
	// tool's, which is "" when the tool was not found.
	Table func(program, tool string) string
	// Advice follows on stderr: what only the user can decide.
	Advice string
}

// Install writes the drop-in and says where, on stdout; the advice goes to
// stderr. It takes no options. The exit code is 1 when the tool was not
// found, and the table says so too.
func (i Integration) Install(install Install, out, problems io.Writer, arguments []string) int {
	if len(arguments) > 0 {
		fmt.Fprintf(problems, "%s install takes no options.\n", i.Name)
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
	written, err := i.WriteDropIn(install.Table(program, tool))
	if err != nil {
		fmt.Fprintf(problems, "%s install: %v\n", i.Name, err)
		return 1
	}
	fmt.Fprintf(out, "wrote %s\n", written)
	fmt.Fprint(problems, install.Advice)
	if lookup != nil {
		fmt.Fprintf(problems, "\n%s is not on this PATH, so the `%s` line in that file is empty: fill it\n"+
			"in with wherever %s actually is.\n", install.Tool, install.Tool, install.Tool)
		return 1
	}
	return 0
}

// Uninstall removes the drop-in and says so.
func (i Integration) Uninstall(out, problems io.Writer, arguments []string) int {
	if len(arguments) > 0 {
		fmt.Fprintf(problems, "%s uninstall takes no options.\n", i.Name)
		return 2
	}
	layout, err := i.layout()
	if err != nil {
		fmt.Fprintf(problems, "%s uninstall: %v\n", i.Name, err)
		return 1
	}
	if err := config.RemoveDropIn(layout, i.Name); err != nil {
		fmt.Fprintf(problems, "%s uninstall: %v\n", i.Name, err)
		return 1
	}
	fmt.Fprintf(out, "removed %s\n", layout.DropIn(i.Name))
	return 0
}

// BundleInstall is what a display that owns its process builds and files: a
// `.app` around a copy of this binary, which macOS gives it no choice about,
// its table, and the launch agent that keeps it running (D-81, D-85).
type BundleInstall struct {
	// Program is the executable's name, inside the bundle too.
	Program string
	// Identifier is the bundle id and the launch agent's label. It must
	// never change.
	Identifier string
	// Icon draws the app icon into an .iconset directory.
	Icon func(iconset string) bool
	// Table is the table to file, given the signing identity to remember so
	// that the next install does not need the flag again. It must name
	// `launch-agent = Identifier`, which is what lets doctor ask launchd.
	Table func(identity string) string
	// Advice follows on stderr.
	Advice string
}

// launchctl runs one launchctl command. A var so that a test can watch what
// would have been asked of launchd without asking it.
var launchctl = func(arguments ...string) error {
	out, err := exec.Command("launchctl", arguments...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl %v: %w\n%s", arguments, err, out)
	}
	return nil
}

// bootoutPatience bounds the wait for a job to finish going away. Generous,
// because what it costs to give up early is a display that is not running.
const bootoutPatience = 5 * time.Second

// loadedUnderLaunchd reports whether launchd still has this job.
func loadedUnderLaunchd(label string) bool {
	return launchctl("print", launchdDomain()+"/"+label) == nil
}

// reloadUnderLaunchd replaces the job this plist names with what the plist
// says now, so that a rebuilt binary is what runs.
//
// The wait is the whole of it. `bootout` returns before launchd has finished
// tearing the job down, and a `bootstrap` that lands inside that window fails
// with "Input/output error" and leaves NOTHING running — the display is gone
// and the install said so in a sentence nobody reads twice. So the old job is
// waited out, and only then is the new one asked for.
func reloadUnderLaunchd(label, plist string) error {
	if loadedUnderLaunchd(label) {
		_ = launchctl("bootout", launchdDomain()+"/"+label)
		deadline := time.Now().Add(bootoutPatience)
		for loadedUnderLaunchd(label) && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
	}
	return launchctl("bootstrap", launchdDomain(), plist)
}

// launchAgentPath is where launchd looks, under the user's home.
func launchAgentPath(label string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot find your home directory: %w", err)
	}
	return filepath.Join(home, "Library", "LaunchAgents", label+".plist"), nil
}

func launchdDomain() string { return "gui/" + strconv.Itoa(os.Getuid()) }

// InstallBundle takes `--app DIR` for where the bundle goes and `--sign
// IDENTITY` for what signs it; an identity not given is read back out of the
// config, and one the keychain does not have is refused before anything is
// built. It then writes the drop-in, writes the launch agent, and loads it,
// restarting a display that was already running: run it again after a
// rebuild and that is the whole upgrade.
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
	fmt.Fprintf(out, "built %s\n", bundle.Path)

	written, err := i.WriteDropIn(bundleInstall.Table(bundle.Identity))
	if err != nil {
		fmt.Fprintf(problems, "%s install: %v\n", bundleInstall.Program, err)
		return 1
	}
	fmt.Fprintf(out, "wrote %s\n", written)

	plist, err := launchAgentPath(bundleInstall.Identifier)
	if err != nil {
		fmt.Fprintf(problems, "%s install: %v\n", bundleInstall.Program, err)
		return 1
	}
	if err := os.MkdirAll(filepath.Dir(plist), 0o755); err != nil {
		fmt.Fprintf(problems, "%s install: cannot create %s: %v\n", bundleInstall.Program, filepath.Dir(plist), err)
		return 1
	}
	if err := os.WriteFile(plist, []byte(LaunchAgentPlist(bundleInstall.Identifier, bundle.PathOfTheBinaryInside())), 0o644); err != nil {
		fmt.Fprintf(problems, "%s install: cannot write %s: %v\n", bundleInstall.Program, plist, err)
		return 1
	}
	fmt.Fprintf(out, "wrote %s\n", plist)

	if err := reloadUnderLaunchd(bundleInstall.Identifier, plist); err != nil {
		fmt.Fprintf(problems, "%s install: %v\n", bundleInstall.Program, err)
		return 1
	}
	fmt.Fprintf(out, "started %s\n", bundleInstall.Identifier)
	fmt.Fprint(problems, bundleInstall.Advice)
	return 0
}

// UninstallBundle stops the launch agent and removes it, the bundle and the
// drop-in. `--app DIR` names where the bundle was put.
func (i Integration) UninstallBundle(bundleInstall BundleInstall, out, problems io.Writer, arguments []string) int {
	flags := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	app := flags.String("app", "", "where the .app bundle is (default: ~/Applications)")
	if err := flags.Parse(arguments); err != nil {
		fmt.Fprintf(problems, "%s uninstall: %v\n", bundleInstall.Program, err)
		return 2
	}

	failed := false
	complain := func(err error) {
		fmt.Fprintf(problems, "%s uninstall: %v\n", bundleInstall.Program, err)
		failed = true
	}
	removed := func(path string) {
		switch err := os.RemoveAll(path); {
		case err == nil:
			fmt.Fprintf(out, "removed %s\n", path)
		case !errors.Is(err, fs.ErrNotExist):
			complain(err)
		}
	}

	_ = launchctl("bootout", launchdDomain()+"/"+bundleInstall.Identifier)
	fmt.Fprintf(out, "stopped %s\n", bundleInstall.Identifier)
	if plist, err := launchAgentPath(bundleInstall.Identifier); err != nil {
		complain(err)
	} else {
		removed(plist)
	}
	if bundle, err := DefaultBundle(bundleInstall.Program, bundleInstall.Identifier, ""); err != nil {
		complain(err)
	} else {
		if *app != "" {
			bundle.Path = filepath.Join(*app, filepath.Base(bundle.Path))
		}
		removed(bundle.Path)
	}
	if err := i.RemoveDropIn(); err != nil {
		complain(err)
	} else if layout, err := i.layout(); err == nil {
		fmt.Fprintf(out, "removed %s\n", layout.DropIn(i.Name))
	}
	if failed {
		return 1
	}
	return 0
}
