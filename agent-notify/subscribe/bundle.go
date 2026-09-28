package subscribe

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/lassoColombo/agent-notify/session"
)

// Bundle is a macOS `.app` around a display that has to own its process.
//
// [verified 2026-09-18, macOS 26.6.2] A bundle-less process has no preferences
// domain that survives it, so an NSStatusItem forgets its position; and
// UNUserNotificationCenter without a bundle identifier terminates the process.
// The executable inside is a COPY, because codesign refuses a symlinked one
// and an unsigned bundle is invisible to the notification system. So a rebuilt
// binary does not reach the bundle on its own: `install` is run again.
type Bundle struct {
	// Path is the `.app` itself.
	Path string
	// Executable is the real binary, absolute.
	Executable string
	// Program is the executable's name inside the bundle.
	Program string
	// Identifier is the bundle id. It must never change: a decision macOS has
	// made about one cannot be unmade.
	Identifier string
	// Identity is the code signing identity, as `security find-identity -p
	// codesigning` names it. Empty is ad-hoc, which is enough for everything
	// except notifications: macOS wants an identity, not a trusted one.
	Identity string
	// Icon draws the app icon into an .iconset directory. Nil for none.
	Icon func(iconset string) bool
}

const iconName = "agent-notify"

// DefaultBundle is `~/Applications/<program>.app`.
func DefaultBundle(program, identifier, executable string) (Bundle, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Bundle{}, fmt.Errorf("cannot find your home directory: %w", err)
	}
	return Bundle{
		Path:       filepath.Join(home, "Applications", program+".app"),
		Executable: executable,
		Program:    program,
		Identifier: identifier,
	}, nil
}

// PathOfTheBinaryInside is what the launch agent runs.
func (b Bundle) PathOfTheBinaryInside() string {
	return filepath.Join(b.Path, "Contents", "MacOS", b.Program)
}

// Write lays the bundle down, again over an existing one: re-running install
// is how a bundle is repaired.
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
	if b.Icon != nil {
		if err := b.icon(); err != nil {
			return err
		}
	}
	if err := copyInto(b.Executable, b.PathOfTheBinaryInside()); err != nil {
		return err
	}
	// Last: a signature seals the contents.
	return b.sign()
}

func (b Bundle) icon() error {
	iconset, err := os.MkdirTemp("", "agent-notify-*.iconset")
	if err != nil {
		return fmt.Errorf("cannot draw the icon: %w", err)
	}
	defer os.RemoveAll(iconset)

	if !b.Icon(iconset) {
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

// copyInto writes through a temporary file and a rename: the file it replaces
// is usually being executed, and a rename leaves the running process on the
// inode it has.
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

// sign signs the bundle. [verified 2026-09-18, macOS 26.6.2] Ad-hoc: no prompt,
// the status goes to `denied` without anybody being asked, and the app never
// appears in System Settings. A self-signed, untrusted identity: the prompt
// appears, and the grant survives the next `go build`.
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

// CodeSigningIdentitiesInTheKeychain includes untrusted ones: a self-signed
// certificate is untrusted by construction and is exactly what is wanted.
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

// plist is the smallest Info.plist that makes this a bundle: LSUIElement keeps
// a display out of the Dock and the app switcher.
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
`, b.Program, b.Identifier, iconName, session.Version, session.Version))
}
