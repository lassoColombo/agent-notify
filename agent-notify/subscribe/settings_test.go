package subscribe_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lassoColombo/agent-notify/session"
	"github.com/lassoColombo/agent-notify/subscribe"
)

type zellijish struct {
	Binary string            `toml:"binary"`
	Glyphs map[string]string `toml:"glyphs"`
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

// TestAnEndedSessionLeavesAViewThatDidNotAskForOne: WantEnded has to mean the
// same thing in a delta as it does in a snapshot, or every display writes this
// filter itself and D-26 is a convention rather than a mechanism.
func TestAnEndedSessionLeavesAViewThatDidNotAskForOne(t *testing.T) {
	root := shortRoot(t)
	fake, err := subscribe.StartFake(root, nil)
	if err != nil {
		t.Fatalf("StartFake: %v", err)
	}
	defer fake.Stop()

	bar, picker := &watched{}, &watched{}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go subscribe.Run(ctx, subscribe.Integration{Name: "bar", Root: root, OnChange: bar.record})
	go subscribe.Run(ctx, subscribe.Integration{
		Name: "picker", Root: root, WantEnded: true, OnChange: picker.record,
	})
	waitFor(t, "both to connect", func() bool { return len(fake.Connected()) == 2 })

	fake.Publish(aSession("one", session.Working, "building"))
	waitFor(t, "the bar to see it", func() bool {
		view, ok := bar.latest()
		return ok && len(view.Sessions) == 1
	})

	ended := aSession("one", session.Ended, "building")
	ended.Sequence = 2
	fake.Publish(ended)

	waitFor(t, "the bar to drop it", func() bool {
		view, ok := bar.latest()
		return ok && len(view.Sessions) == 0
	})
	waitFor(t, "the picker to keep it", func() bool {
		view, ok := picker.latest()
		return ok && len(view.Sessions) == 1 && view.Sessions[0].Kernel == session.Ended
	})
}

// TestNamingTheRootAndLettingTheEnvironmentNameItAgree is D-69 as a test, and
// it failed before the change it pins.
//
// Every integration used to write os.Getenv("AGENT_NOTIFY_ROOT") at the call
// site and hand the answer over as Root. That looked like a no-op and was not:
// the environment branch ran the value through filepath.Abs and the explicit
// branch concatenated strings, so a relative root resolved against the calling
// process's working directory — which, for a child the session-watcher started,
// is not the one the person who set the variable was standing in.
func TestNamingTheRootAndLettingTheEnvironmentNameItAgree(t *testing.T) {
	for _, root := range []string{
		"a/relative/root",  // the case that differed
		"/tmp/an/absolute", // the case that did not
		"/tmp/trailing/",   // and the one that produced `root//config.toml`
	} {
		t.Setenv("AGENT_NOTIFY_ROOT", root)

		readByCore, err := subscribe.Integration{}.ConfigFile()
		if err != nil {
			t.Fatalf("%s: %v", root, err)
		}
		passedThrough, err := subscribe.Integration{Root: root}.ConfigFile()
		if err != nil {
			t.Fatalf("%s: %v", root, err)
		}
		if readByCore != passedThrough {
			t.Errorf("AGENT_NOTIFY_ROOT=%q gives two answers:\n  read by core:  %s\n"+
				"  passed in:     %s", root, readByCore, passedThrough)
		}
		if !filepath.IsAbs(readByCore) {
			t.Errorf("AGENT_NOTIFY_ROOT=%q resolved to %q, which is a direction and "+
				"not a place", root, readByCore)
		}
		if strings.Contains(readByCore, "//") {
			t.Errorf("AGENT_NOTIFY_ROOT=%q resolved to %q", root, readByCore)
		}
	}
}
