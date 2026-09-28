package main

import (
	"encoding/json"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lassoColombo/agent-notify/subscribe"
)

// bundledAt stands a bundle up around a real Mach-O binary, because a bundle is
// signed and `codesign` will not sign a shell script standing in for one.
func bundledAt(t *testing.T) Bundle {
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
	return Bundle{Path: filepath.Join(root, "Applications", program+".app"), Executable: binary}
}

// TestTheExecutableIsARegularFileAndNotALink. A symlink is the tidier design
// and it cannot be signed: `codesign` answers "the main executable or Info.plist
// must be a regular file", and an unsigned bundle never reaches the
// notification system at all.
func TestTheExecutableIsARegularFileAndNotALink(t *testing.T) {
	bundle := bundledAt(t)
	if err := bundle.Write(); err != nil {
		t.Fatalf("Write: %v", err)
	}

	info, err := os.Lstat(bundle.PathOfTheBinaryInside())
	if err != nil {
		t.Fatalf("Lstat: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("%s is a symlink, which cannot be signed", bundle.PathOfTheBinaryInside())
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("%s is not a regular file", bundle.PathOfTheBinaryInside())
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("%s is not executable", bundle.PathOfTheBinaryInside())
	}
}

// TestTheBundleIsSignedAsItself is the regression test for an afternoon: with a
// symlinked executable `codesign -dv` reports the identifier as `a.out`, macOS
// has no record of the app, and asking for notification permission silently
// does nothing at all — no prompt, and the status stays `not-determined`.
func TestTheBundleIsSignedAsItself(t *testing.T) {
	bundle := bundledAt(t)
	if err := bundle.Write(); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out, err := exec.Command("codesign", "-dv", bundle.Path).CombinedOutput()
	if err != nil {
		t.Fatalf("the bundle is not signed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "Identifier="+Identifier) {
		t.Errorf("codesign reports:\n%s\nwant Identifier=%s", out, Identifier)
	}
}

// TestRebuildingReplacesWhatTheBundleRuns, including while it is running, which
// is the ordinary case: install is how a running display is upgraded.
func TestRebuildingReplacesWhatTheBundleRuns(t *testing.T) {
	bundle := bundledAt(t)
	if err := bundle.Write(); err != nil {
		t.Fatalf("first Write: %v", err)
	}
	before, err := os.ReadFile(bundle.PathOfTheBinaryInside())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

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
	after, err := os.ReadFile(bundle.PathOfTheBinaryInside())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(after) == len(before) {
		t.Errorf("the bundle still holds the old binary")
	}
}

// TestTheInfoPlistSaysTheThingsMacOSReads. The identifier is the preferences
// domain the item's position is filed under, and LSUIElement is what keeps a
// display out of the Dock.
//
// Read back through `plutil` rather than through a plist library, because the
// question is not whether some parser accepts what was written — it is whether
// the one on this machine does, and that one is also the only opinion that
// counts at runtime.
func TestTheInfoPlistSaysTheThingsMacOSReads(t *testing.T) {
	bundle := bundledAt(t)
	if err := bundle.Write(); err != nil {
		t.Fatalf("Write: %v", err)
	}
	body, err := exec.Command("plutil", "-convert", "json", "-o", "-",
		filepath.Join(bundle.Path, "Contents", "Info.plist")).Output()
	if err != nil {
		t.Fatalf("plutil could not read what was written: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("what plutil gave back is not JSON: %v", err)
	}
	if parsed["CFBundleIdentifier"] != Identifier {
		t.Errorf("CFBundleIdentifier = %v, want %q", parsed["CFBundleIdentifier"], Identifier)
	}
	if parsed["CFBundleExecutable"] != program {
		t.Errorf("CFBundleExecutable = %v, want %q", parsed["CFBundleExecutable"], program)
	}
	if parsed["LSUIElement"] != true {
		t.Errorf("LSUIElement = %v — a display would appear in the Dock", parsed["LSUIElement"])
	}
}

// TestMacOSItselfAcceptsTheBundle, because a plist this program can parse and
// macOS cannot is exactly the bug a hand-written plist invites.
func TestMacOSItselfAcceptsTheBundle(t *testing.T) {
	bundle := bundledAt(t)
	if err := bundle.Write(); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out, err := exec.Command("plutil", "-lint",
		filepath.Join(bundle.Path, "Contents", "Info.plist")).CombinedOutput()
	if err != nil {
		t.Fatalf("plutil rejected it: %v\n%s", err, out)
	}
}

// TestInstallingTwiceRepairsRatherThanFails: install is what somebody runs when
// something is wrong, so it has to be the fix and not a thing to be careful
// about.
func TestInstallingTwiceRepairsRatherThanFails(t *testing.T) {
	bundle := bundledAt(t)
	if err := bundle.Write(); err != nil {
		t.Fatalf("first Write: %v", err)
	}
	// Whatever is in there is broken: truncated, half-copied, replaced by hand.
	if err := os.WriteFile(bundle.PathOfTheBinaryInside(), []byte("rubbish"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := bundle.Write(); err != nil {
		t.Fatalf("second Write: %v", err)
	}
	body, _ := os.ReadFile(bundle.PathOfTheBinaryInside())
	if string(body) == "rubbish" {
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

// TestTheLaunchAgentPointsInsideTheBundle. Naming the bare binary would work in
// every way except the one the bundle exists for.
//
// The path moved out of the config table and into the launch agent when this
// display stopped being something core runs, and the check moved with it: the
// table has no binary at all now, because `binary` means "core may run this".
func TestTheLaunchAgentPointsInsideTheBundle(t *testing.T) {
	bundle := bundledAt(t)

	plist := subscribe.LaunchAgentPlist(Identifier, bundle.PathOfTheBinaryInside())
	if !strings.Contains(plist, ".app/Contents/MacOS/") {
		t.Errorf("the launch agent names %q, which is not inside a bundle", plist)
	}

	if table := theConfigTableToAdd(""); strings.Contains(table, "binary") {
		t.Errorf("the table still names a binary, so core would try to run it:\n%s", table)
	}
}

// TestThisTestBinaryIsNotBundled is the other half of the runtime check: a bare
// binary must be able to tell that it is one, or the warning never fires.
func TestThisTestBinaryIsNotBundled(t *testing.T) {
	if got := RunningIn(); got != "" {
		t.Errorf("RunningIn() = %q, but `go test` does not run from a bundle", got)
	}
}

// TestABundleIsSignedWithTheIdentityItIsGiven, because the difference between
// an ad-hoc signature and a named one is the difference between a display that
// can notify and one macOS will not even list in System Settings.
func TestABundleIsSignedWithTheIdentityItIsGiven(t *testing.T) {
	identities := CodeSigningIdentitiesInTheKeychain()
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
	if !strings.Contains(string(out), "Authority="+identities[0]) {
		t.Errorf("signed by somebody else:\n%s\nwant Authority=%s", out, identities[0])
	}
	if strings.Contains(string(out), "adhoc") {
		t.Errorf("still ad-hoc despite being given %q", identities[0])
	}
}

// TestIdentitiesIncludesUntrustedOnes. A self-signed certificate is untrusted
// by construction — `security find-identity -v` hides it — and it is exactly
// the one wanted here, so the listing must not filter on validity.
func TestIdentitiesIncludesUntrustedOnes(t *testing.T) {
	out, err := exec.Command("security", "find-identity", "-p", "codesigning").Output()
	if err != nil {
		t.Skipf("no security command here: %v", err)
	}
	counted := strings.Count(string(out), `"`) / 2
	if got := len(CodeSigningIdentitiesInTheKeychain()); got > counted {
		t.Errorf("found %d identities, and security lists %d", got, counted)
	}
}

// TestTheIconIsDrawnInThePalette.
//
// The icon is the one drawing in this program that nobody is looking at while
// it is made — it is written at install time, into a bundle, and the next place
// it appears is a notification banner — so the palette it is drawn in is worth
// a test rather than a glance.
//
// It also pins the colour space. AppKit draws in Apple's calibrated RGB, and a
// PNG written straight out of that holds `#b693e1` where `#c4a7e7` was asked
// for: right on screen, because the profile travels with the file, and wrong to
// everything that reads the pixels.
func TestTheIconIsDrawnInThePalette(t *testing.T) {
	if !Available() {
		t.Skip("nothing to draw with here")
	}
	into := t.TempDir()
	if !WriteIconset(into) {
		t.Fatal("the iconset was not drawn")
	}
	drawn := decoded(t, filepath.Join(into, "icon_512x512.png"))

	// The mark is Iris, and deliberately neither hue a state wears: an icon in
	// Love would read as an agent waiting for you every time it appeared.
	if got := pixel(drawn, 256, 256); got != iris {
		t.Errorf("the mark is %06x, want Iris %06x", got, iris)
	}
	// The canvas is the palette's night, Overlay at the top down to Base at the
	// bottom. Within one step, because a gradient is sampled a few pixels away
	// from each end.
	for _, one := range []struct {
		y    int
		want uint32
		name string
	}{{40, 0x26233a, "Overlay"}, {470, 0x191724, "Base"}} {
		if got := pixel(drawn, 256, one.y); !alike(got, one.want, 2) {
			t.Errorf("the canvas at y=%d is %06x, want %s %06x", one.y, got, one.name, one.want)
		}
	}
	// And the corner is nothing at all: an app icon is a rounded rect with
	// transparent shoulders, not a square.
	if _, _, _, opacity := drawn.At(2, 2).RGBA(); opacity != 0 {
		t.Errorf("the corner has alpha %d, want a rounded icon", opacity>>8)
	}
}

func decoded(t *testing.T, path string) image.Image {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer file.Close()
	drawn, err := png.Decode(file)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	return drawn
}

// pixel is one point as 0xRRGGBB.
func pixel(drawn image.Image, x, y int) uint32 {
	red, green, blue, _ := drawn.At(x, y).RGBA()
	return (red>>8)<<16 | (green>>8)<<8 | blue>>8
}

func alike(got, want, step uint32) bool {
	for shift := 0; shift <= 16; shift += 8 {
		one, other := (got>>shift)&0xff, (want>>shift)&0xff
		if one > other+step || other > one+step {
			return false
		}
	}
	return true
}

// iris is the one colour the mark is drawn in.
const iris = 0xc4a7e7

// invader is the sprite, and it is written down twice on purpose: here, and in
// the Objective-C that draws it. There are a dozen arrangements of this alien
// about, they differ in the antennae and the legs, and this is the one that was
// asked for.
var invader = []string{
	"..X.....X..",
	"X..X...X..X",
	"X.XXXXXXX.X",
	"XXXXXXXXXXX",
	"XXX.XXX.XXX",
	"XXXXXXXXXXX",
	".XXXXXXXXX.",
	"..X.....X..",
	".X.......X.",
}

// TestTheIconIsTheInvader reads the sprite back out of the drawn icon.
//
// Cell by cell, off the picture, rather than by sampling a pixel or two: a
// sprite is a shape, the thing that can go wrong with it is being one row out
// or a mirror image of itself, and neither of those is visible in a spot check.
func TestTheIconIsTheInvader(t *testing.T) {
	if !Available() {
		t.Skip("nothing to draw with here")
	}
	into := t.TempDir()
	if !WriteIconset(into) {
		t.Fatal("the iconset was not drawn")
	}
	drawn := decoded(t, filepath.Join(into, "icon_512x512.png"))

	// Where the mark is, without knowing anything about how big it was drawn.
	box := drawn.Bounds()
	left, right, top, bottom := box.Max.X, box.Min.X, box.Max.Y, box.Min.Y
	for y := box.Min.Y; y < box.Max.Y; y++ {
		for x := box.Min.X; x < box.Max.X; x++ {
			if pixel(drawn, x, y) != iris {
				continue
			}
			left, right = min(left, x), max(right, x)
			top, bottom = min(top, y), max(bottom, y)
		}
	}
	across, down := right-left+1, bottom-top+1
	if across <= 0 || down <= 0 {
		t.Fatal("there is no mark on the icon at all")
	}

	// A sprite is square cells or it is not a sprite.
	cell := across / len(invader[0])
	if cell != down/len(invader) || cell*len(invader[0]) != across || cell*len(invader) != down {
		t.Fatalf("the mark is %dx%d, which is not %d by %d whole square cells",
			across, down, len(invader[0]), len(invader))
	}

	for row := range invader {
		read := ""
		for column := range invader[row] {
			if pixel(drawn, left+column*cell+cell/2, top+row*cell+cell/2) == iris {
				read += "X"
			} else {
				read += "."
			}
		}
		if read != invader[row] {
			t.Errorf("row %d of the icon reads %q, want %q", row, read, invader[row])
		}
	}
}
