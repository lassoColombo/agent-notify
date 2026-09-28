package subscribe_test

import (
	"os"
	"strings"
	"testing"

	"github.com/lassoColombo/agent-notify/subscribe"
)

type zellijish struct {
	Binary string            `toml:"binary"`
	Glyphs map[string]string `toml:"glyphs"`
}

// shortRoot is a root a unix socket path still fits under.
//
// Not t.TempDir(): its path on macOS is long enough on its own that adding a
// socket name exceeds what a unix socket may be.
func shortRoot(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "an-sdk")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	return root
}

func configured(t *testing.T, body string) string {
	t.Helper()
	root := shortRoot(t)
	if err := os.WriteFile(root+"/config.toml", []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return root
}

func TestSettingsDecodeAnIntegrationsOwnTable(t *testing.T) {
	root := configured(t, `
[integration.zellij]
binary = "agent-notify-zellij"

[integration.zellij.settings]
binary = "/opt/homebrew/bin/zellij"

[integration.zellij.settings.glyphs]
working = "*"
"blocked-on-you/permission-prompt" = "?"

[integration.sketchybar.settings]
colour = "red"
`)

	var mine zellijish
	if err := (subscribe.Integration{Name: "zellij", Root: root}).Settings(&mine); err != nil {
		t.Fatalf("Settings: %v", err)
	}
	if mine.Binary != "/opt/homebrew/bin/zellij" {
		t.Errorf("binary = %q", mine.Binary)
	}
	if mine.Glyphs["working"] != "*" || mine.Glyphs["blocked-on-you/permission-prompt"] != "?" {
		t.Errorf("glyphs = %v", mine.Glyphs)
	}
	// Another integration's table is another integration's business.
	if len(mine.Glyphs) != 2 {
		t.Errorf("glyphs picked up something that was not ours: %v", mine.Glyphs)
	}
}

// TestAMisspelledSettingIsLoud is the failure this exists to prevent: a glyph
// name with a typo in it changes nothing and says nothing, and the person
// concludes the feature does not work.
func TestAMisspelledSettingIsLoud(t *testing.T) {
	root := configured(t, "[integration.zellij.settings]\nbinry = \"zellij\"\n")

	var mine zellijish
	err := (subscribe.Integration{Name: "zellij", Root: root}).Settings(&mine)
	if err == nil {
		t.Fatal("a key nobody declared was accepted in silence")
	}
	if !strings.Contains(err.Error(), "binry") {
		t.Errorf("the error does not name the offending key: %v", err)
	}
}

func TestNoTableAtAllLeavesTheDisplaysOwnDefaults(t *testing.T) {
	root := shortRoot(t)

	mine := zellijish{Binary: "zellij"}
	if err := (subscribe.Integration{Name: "zellij", Root: root}).Settings(&mine); err != nil {
		t.Fatalf("Settings with no config file at all: %v", err)
	}
	if mine.Binary != "zellij" {
		t.Errorf("binary = %q, want the default the display arrived with", mine.Binary)
	}
}
