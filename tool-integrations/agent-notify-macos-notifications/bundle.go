package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	agentnotify "github.com/lassoColombo/agent-notify"
)

// This program runs inside a `.app` bundle, and it has no choice at all
// [verified 2026-09-18, macOS 26.6.2]. `UNUserNotificationCenter` without a
// bundle identifier does not return an error, it TERMINATES the process:
// `bundleProxyForCurrentProcess is nil`, raised inside a `dispatch_once`, with
// nothing to catch. So a bare binary cannot ask for permission, cannot post
// anything, and cannot even find out that it cannot.
//
// The executable inside it is a COPY, and that is not the obvious choice — a
// symlink is tidier and costs nothing to keep fresh. It is a copy because of
// the second measured fact:
//
//	$ codesign -s - --force ~/Applications/agent-notify-macos-notifications.app
//	the main executable or Info.plist must be a regular file (no symlinks, etc.)
//
// and an unsigned bundle is invisible to the notification system: with a
// symlinked executable `codesign -dv` reports the identifier as `a.out` and
// asking for permission leaves the status at `not-determined` with no prompt.
//
// **An ad-hoc signature is not enough either.** It is answered "Notifications
// are not allowed for this application", the status is `denied` without anybody
// having been asked, and the app never appears in System Settings — so there is
// nowhere to go and turn it on. A self-signed certificate in the login
// keychain, untrusted, works: macOS wants an identity, not a trusted one and
// not Apple's. `install --sign` takes it, and the README has the recipe.
//
// The price is that rebuilding the binary does not refresh the bundle. Re-run
// `install`; it is idempotent and exists for exactly this.
//
// It is deliberately not a launchable app. LSUIElement keeps it out of the Dock
// and the app switcher, and nothing ever opens it through LaunchServices: the
// session-watcher execs the path inside it, exactly as it execs every other
// integration.

// program is the executable's name, which is also the name it carries inside
// the bundle and the value of CFBundleExecutable.
const program = "agent-notify-macos-notifications"

// Identifier is the bundle id: the app macOS files its notification decision
// against, and the most consequential constant in this repository.
//
// **Choose it before the first run and sign properly from the start.** A
// decision macOS has made about an identifier cannot be unmade — an identifier
// that was ever used with an ad-hoc signature carries a `denied` that survives
// being signed correctly afterwards, does not appear in System Settings, and
// has no reset. Two identifiers were burned finding that out, and this is the
// third; it is deliberately not the one the menu bar display used, because
// these are two apps now and macOS must be able to tell them apart.
const Identifier = "io.github.lassocolombo.agent-notify-notifications"

// iconName is what the icon is called inside the bundle, and therefore the
// value of CFBundleIconFile.
const iconName = "agent-notify"

// Bundle is where the wrapper is and what it points at.
type Bundle struct {
	// Path is the `.app` itself.
	Path string
	// Executable is the real binary, absolute.
	Executable string
	// Identity is the code signing identity to sign with, as `security
	// find-identity -p codesigning` names it. Empty means an ad-hoc signature,
	// which is enough for everything here EXCEPT notifications — see sign.
	Identity string
}

// DefaultBundle is where it goes when nobody says otherwise: the standard
// per-user application directory, so that it is somewhere a person would think
// to look and somewhere they can delete it from.
func DefaultBundle(executable string) (Bundle, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Bundle{}, fmt.Errorf("cannot find your home directory: %w", err)
	}
	return Bundle{
		Path:       filepath.Join(home, "Applications", program+".app"),
		Executable: executable,
	}, nil
}

// PathOfTheBinaryInside is what the session-watcher runs, and what goes in the
// config. Running the binary directly instead would work in every way except
// the one that matters.
func (b Bundle) PathOfTheBinaryInside() string {
	return filepath.Join(b.Path, "Contents", "MacOS", program)
}

// Write lays the bundle down, and does it again over an existing one.
//
// Idempotent, because `install` is run again whenever anything about the
// installation changes and re-running it must be the way to repair a bundle
// rather than a thing to be careful about.
func (b Bundle) Write() error {
	if !filepath.IsAbs(b.Executable) {
		return fmt.Errorf("the bundle has to point at an absolute path, and %q is not one", b.Executable)
	}
	if _, err := os.Stat(b.Executable); err != nil {
		return fmt.Errorf("there is no binary at %s to wrap: %w", b.Executable, err)
	}

	macos := filepath.Join(b.Path, "Contents", "MacOS")
	if err := os.MkdirAll(macos, 0o755); err != nil {
		return fmt.Errorf("cannot create %s: %w", macos, err)
	}
	if err := os.WriteFile(filepath.Join(b.Path, "Contents", "Info.plist"), b.plist(), 0o644); err != nil {
		return fmt.Errorf("cannot write the bundle's Info.plist: %w", err)
	}

	if err := b.icon(); err != nil {
		return err
	}
	if err := copyInto(b.Executable, b.PathOfTheBinaryInside()); err != nil {
		return err
	}
	// Last: a signature seals the bundle's contents, so anything written
	// afterwards invalidates it.
	return b.sign()
}

// icon draws the app icon and puts it in the bundle.
//
// Drawn at install time rather than checked in, because an icon in a repository
// is a binary nobody reads in a diff, and this one is a rounded rectangle and a
// symbol the system already has. It matters more than it sounds: the bundle's
// icon is what a NOTIFICATION shows, and without one every banner carries the
// blank placeholder square.
func (b Bundle) icon() error {
	iconset, err := os.MkdirTemp("", "agent-notify-*.iconset")
	if err != nil {
		return fmt.Errorf("cannot draw the icon: %w", err)
	}
	defer os.RemoveAll(iconset)

	if !WriteIconset(iconset) {
		return fmt.Errorf("cannot draw the icon into %s", iconset)
	}
	resources := filepath.Join(b.Path, "Contents", "Resources")
	if err := os.MkdirAll(resources, 0o755); err != nil {
		return fmt.Errorf("cannot create %s: %w", resources, err)
	}

	iconutil, err := exec.LookPath("iconutil")
	if err != nil {
		return fmt.Errorf("iconutil is not on PATH, so the icon cannot be assembled: %w", err)
	}
	out, err := exec.Command(iconutil, "-c", "icns",
		"-o", filepath.Join(resources, iconName+".icns"), iconset).CombinedOutput()
	if err != nil {
		return fmt.Errorf("iconutil refused the icon: %w\n%s", err, out)
	}
	return nil
}

// copyInto puts the binary in the bundle, through a temporary file and a
// rename.
//
// Written that way because the file it replaces is usually being EXECUTED —
// this is how a running display is upgraded — and writing over a running binary
// is ETXTBSY. A rename swaps the directory entry and leaves the running process
// on the inode it already has, which is also what makes the upgrade atomic:
// there is no moment at which the bundle holds half a binary.
func copyInto(from, to string) error {
	source, err := os.Open(from)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", from, err)
	}
	defer source.Close()

	temporary := to + ".incoming"
	destination, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("cannot write into the bundle: %w", err)
	}
	if _, err := io.Copy(destination, source); err != nil {
		destination.Close()
		os.Remove(temporary)
		return fmt.Errorf("cannot write into the bundle: %w", err)
	}
	if err := destination.Close(); err != nil {
		os.Remove(temporary)
		return fmt.Errorf("cannot write into the bundle: %w", err)
	}
	if err := os.Rename(temporary, to); err != nil {
		os.Remove(temporary)
		return fmt.Errorf("cannot put the binary in place: %w", err)
	}
	return nil
}

// sign signs the bundle, with a named identity when there is one and ad-hoc
// when there is not.
//
// Shelling out, which this project otherwise does not do — there is no Go API
// for code signing, `codesign` is a tool in the sense the rule allows, and the
// alternative is a bundle macOS will not talk to.
//
// **The difference between the two signatures is whether notifications work at
// all**, and it took most of an afternoon to find because every symptom pointed
// elsewhere [verified 2026-09-18, macOS 26.6.2]:
//
//   - Ad-hoc: `requestAuthorization` answers "Notifications are not allowed for
//     this application", no prompt is ever shown, the status goes to `denied`
//     without anybody being asked, and the app never appears in
//     `com.apple.ncprefs` — so it is not in System Settings either, and there
//     is nowhere to go and turn it on. Tested from /private/tmp and from
//     ~/Applications, registered with lsregister, with a valid signature. The
//     location is not the variable; the identity is.
//   - A self-signed certificate with the code-signing extended key usage,
//     imported into the login keychain and NOT trusted: the prompt appears.
//     That is the whole difference. macOS does not require the certificate to
//     be trusted, or to come from Apple — only that the signature carries an
//     identity rather than being ad-hoc.
//
// A self-signed identity is also STABLE across rebuilds where an ad-hoc one is
// not, so the permission somebody grants survives the next `go build`.
func (b Bundle) sign() error {
	codesign, err := exec.LookPath("codesign")
	if err != nil {
		return fmt.Errorf("codesign is not on PATH, and an unsigned bundle is one macOS "+
			"will not talk to: %w", err)
	}
	identity := b.Identity
	if identity == "" {
		identity = "-"
	}
	out, err := exec.Command(codesign, "-s", identity, "--force", b.Path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("codesign refused the bundle as %q: %w\n%s", identity, err, out)
	}
	return nil
}

// CodeSigningIdentitiesInTheKeychain is every one this keychain holds, spelled
// as codesign would name it.
//
// Untrusted ones are included, and that is the point: a self-signed certificate
// is untrusted by construction and is exactly what is wanted here.
func CodeSigningIdentitiesInTheKeychain() []string {
	security, err := exec.LookPath("security")
	if err != nil {
		return nil
	}
	out, err := exec.Command(security, "find-identity", "-p", "codesigning").Output()
	if err != nil {
		return nil
	}
	var found []string
	for _, line := range strings.Split(string(out), "\n") {
		open := strings.Index(line, `"`)
		if open < 0 {
			continue
		}
		rest := line[open+1:]
		close := strings.Index(rest, `"`)
		if close < 0 {
			continue
		}
		if name := rest[:close]; name != "" && !slices.Contains(found, name) {
			found = append(found, name)
		}
	}
	return found
}

// plist is the smallest Info.plist that makes this a bundle macOS will treat as
// one: a name for the executable, an identifier to file preferences under, and
// LSUIElement so that a display never appears in the Dock or the app switcher.
func (b Bundle) plist() []byte {
	return []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleExecutable</key>
	<string>%s</string>
	<key>CFBundleIdentifier</key>
	<string>%s</string>
	<key>CFBundleName</key>
	<string>agent-notify</string>
	<key>CFBundleIconFile</key>
	<string>%s</string>
	<key>CFBundleDisplayName</key>
	<string>agent-notify</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>CFBundleShortVersionString</key>
	<string>%s</string>
	<key>CFBundleVersion</key>
	<string>%s</string>
	<key>LSUIElement</key>
	<true/>
	<key>NSHighResolutionCapable</key>
	<true/>
</dict>
</plist>
`, program, Identifier, iconName, agentnotify.Version, agentnotify.Version))
}
