//go:build darwin

package subscribe_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lassoColombo/agent-notify/subscribe"
)

const (
	program    = "agent-notify-test-display"
	identifier = "io.github.lassocolombo.agent-notify-test"
)

// bundledAt stands a bundle up around a real Mach-O binary, because codesign
// will not sign a shell script standing in for one.
func bundledAt(t *testing.T) subscribe.Bundle {
	t.Helper()
	root := t.TempDir()
	binary := filepath.Join(root, "bin", program)
	if err := os.MkdirAll(filepath.Dir(binary), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	real, err := os.ReadFile("/bin/echo")
	if err != nil {
		t.Skipf("no binary to stand in for one: %v", err)
	}
	if err := os.WriteFile(binary, real, 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return subscribe.Bundle{
		Path: filepath.Join(root, "Applications", program+".app"), Executable: binary,
		Program: program, Identifier: identifier,
	}
}

// A symlink is tidier and cannot be signed: codesign answers "the main
// executable or Info.plist must be a regular file".
func TestTheExecutableIsARegularFileAndNotALink(t *testing.T) {
	bundle := bundledAt(t)
	if err := bundle.Write(); err != nil {
		t.Fatalf("Write: %v", err)
	}
	info, err := os.Lstat(bundle.PathOfTheBinaryInside())
	if err != nil {
		t.Fatalf("Lstat: %v", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("%s is not a regular executable", bundle.PathOfTheBinaryInside())
	}
}

// With a symlinked executable codesign reports the identifier as `a.out`, and
// macOS has no record of the app.
func TestTheBundleIsSignedAsItself(t *testing.T) {
	bundle := bundledAt(t)
	if err := bundle.Write(); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out, err := exec.Command("codesign", "-dv", bundle.Path).CombinedOutput()
	if err != nil {
		t.Fatalf("the bundle is not signed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "Identifier="+identifier) {
		t.Errorf("codesign reports:\n%s\nwant Identifier=%s", out, identifier)
	}
}

// Install is how a running display is upgraded, so the copy replaces a binary
// that is being executed.
func TestRebuildingReplacesWhatTheBundleRuns(t *testing.T) {
	bundle := bundledAt(t)
	if err := bundle.Write(); err != nil {
		t.Fatalf("first Write: %v", err)
	}
	before, _ := os.ReadFile(bundle.PathOfTheBinaryInside())

	newer, err := os.ReadFile("/bin/date")
	if err != nil {
		t.Skipf("nothing else to stand in for a rebuild: %v", err)
	}
	if err := os.WriteFile(bundle.Executable, newer, 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := bundle.Write(); err != nil {
		t.Fatalf("second Write: %v", err)
	}
	after, _ := os.ReadFile(bundle.PathOfTheBinaryInside())
	if len(after) == len(before) {
		t.Errorf("the bundle still holds the old binary")
	}
}

// Read back through plutil, because the parser that counts is the one on this
// machine.
func TestTheInfoPlistSaysTheThingsMacOSReads(t *testing.T) {
	bundle := bundledAt(t)
	if err := bundle.Write(); err != nil {
		t.Fatalf("Write: %v", err)
	}
	plist := filepath.Join(bundle.Path, "Contents", "Info.plist")
	if out, err := exec.Command("plutil", "-lint", plist).CombinedOutput(); err != nil {
		t.Fatalf("plutil rejected it: %v\n%s", err, out)
	}
	body, err := exec.Command("plutil", "-convert", "json", "-o", "-", plist).Output()
	if err != nil {
		t.Fatalf("plutil could not read what was written: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("what plutil gave back is not JSON: %v", err)
	}
	if parsed["CFBundleIdentifier"] != identifier || parsed["CFBundleExecutable"] != program {
		t.Errorf("plist says %v", parsed)
	}
	if parsed["LSUIElement"] != true {
		t.Errorf("LSUIElement = %v — a display would appear in the Dock", parsed["LSUIElement"])
	}
}

func TestInstallingTwiceRepairsRatherThanFails(t *testing.T) {
	bundle := bundledAt(t)
	if err := bundle.Write(); err != nil {
		t.Fatalf("first Write: %v", err)
	}
	if err := os.WriteFile(bundle.PathOfTheBinaryInside(), []byte("rubbish"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := bundle.Write(); err != nil {
		t.Fatalf("second Write: %v", err)
	}
	if body, _ := os.ReadFile(bundle.PathOfTheBinaryInside()); string(body) == "rubbish" {
		t.Errorf("a broken bundle was not repaired")
	}
}

func TestABundleCannotPointAtSomethingThatIsNotThere(t *testing.T) {
	bundle := bundledAt(t)
	bundle.Executable = filepath.Join(t.TempDir(), "gone")
	err := bundle.Write()
	if err == nil || !strings.Contains(err.Error(), "no binary") {
		t.Errorf("err = %v, want a refusal naming the missing binary", err)
	}
}

// The difference between an ad-hoc signature and a named one is whether macOS
// will deliver notifications at all.
func TestABundleIsSignedWithTheIdentityItIsGiven(t *testing.T) {
	identities := subscribe.CodeSigningIdentitiesInTheKeychain()
	if len(identities) == 0 {
		t.Skip("this keychain has no code signing identity to sign with")
	}
	bundle := bundledAt(t)
	bundle.Identity = identities[0]
	if err := bundle.Write(); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out, err := exec.Command("codesign", "-dv", "--verbose=4", bundle.Path).CombinedOutput()
	if err != nil {
		t.Fatalf("codesign: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "Authority="+identities[0]) || strings.Contains(string(out), "adhoc") {
		t.Errorf("signed by somebody else:\n%s\nwant Authority=%s", out, identities[0])
	}
}

// A self-signed certificate is untrusted by construction, and `security
// find-identity -v` hides it, so the listing must not filter on validity.
func TestIdentitiesIncludesUntrustedOnes(t *testing.T) {
	out, err := exec.Command("security", "find-identity", "-p", "codesigning").Output()
	if err != nil {
		t.Skipf("no security command here: %v", err)
	}
	counted := strings.Count(string(out), `"`) / 2
	if got := len(subscribe.CodeSigningIdentitiesInTheKeychain()); got > counted {
		t.Errorf("found %d identities, and security lists %d", got, counted)
	}
}
