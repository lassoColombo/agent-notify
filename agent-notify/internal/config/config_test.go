package config_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lassoColombo/agent-notify/internal/config"
	"github.com/lassoColombo/agent-notify/session"
)

func TestMissingFileIsNotAComplaint(t *testing.T) {
	settings, problems := config.Load(filepath.Join(t.TempDir(), "absent.toml"))
	if len(problems) != 0 {
		t.Errorf("an absent file produced %d complaint(s): %v", len(problems), problems)
	}
	if settings.KeepEndedSessions != config.Defaults().KeepEndedSessions {
		t.Errorf("KeepEndedSessions = %s, want the default", settings.KeepEndedSessions)
	}
}

// TestMalformedFileIsRefusedWhole is the other half of M2's "done when":
// defaults, one complaint, and nothing that breaks the caller.
func TestMalformedFileIsRefusedWhole(t *testing.T) {
	settings, problems := config.Parse([]byte("keep-ended-sessions = \nthis is not toml ]]"), "broken.toml")
	if len(problems) != 1 {
		t.Fatalf("got %d complaints, want exactly 1: %v", len(problems), problems)
	}
	if !reflect.DeepEqual(settings, config.Defaults()) {
		t.Errorf("a malformed file did not fall back to defaults")
	}
	if !strings.Contains(problems[0].Error(), "broken.toml") {
		t.Errorf("the complaint does not name the file:\n%v", problems[0])
	}
}

func TestAWholeFileIsRead(t *testing.T) {
	settings, problems := config.Parse([]byte(`
keep-ended-sessions = "72h"

[agent.claude]
binary = "claude"

[integration.zellij]
binary = "agent-notify-zellij"
settings = { glyphs = { working = "*" } }

[integration.sketchybar]
enabled = false
binary  = "agent-notify-sketchybar"

[integration.aerospace]
binary = "agent-notify-aerospace-container"

[container]
order = ["aerospace", "zellij"]
`), "config.toml")

	for _, problem := range problems {
		t.Errorf("unexpected complaint: %v", problem)
	}
	if got := settings.KeepEndedSessions.Duration(); got != 3*24*time.Hour {
		t.Errorf("KeepEndedSessions = %s, want 72h", got)
	}
	if settings.Agent["claude"].Binary != "claude" {
		t.Errorf("agent.claude.binary = %q", settings.Agent["claude"].Binary)
	}
	if !settings.Integration["zellij"].IsEnabled() {
		t.Errorf("an integration with no enabled key should be enabled")
	}
	if settings.Integration["sketchybar"].IsEnabled() {
		t.Errorf("enabled = false was ignored")
	}
	if got := settings.Container.Order; len(got) != 2 || got[0] != "aerospace" {
		t.Errorf("container.order = %q", got)
	}

	// Settings reach the integration verbatim and core never looks inside
	// (plan.md §A17 R7) — but they must survive the trip.
	glyphs, ok := settings.Integration["zellij"].Settings["glyphs"].(map[string]any)
	if !ok {
		t.Fatalf("settings.glyphs did not survive decoding: %#v", settings.Integration["zellij"].Settings)
	}
	if glyphs["working"] != "*" {
		t.Errorf("settings.glyphs.working = %v", glyphs["working"])
	}
}

func TestUnknownKeysAreReportedAndTheRestSurvives(t *testing.T) {
	settings, problems := config.Parse([]byte(`
keep-ended-sessions = "48h"
retention-visible   = "1m"

[integration.zellij]
binary = "agent-notify-zellij"
colour = "blue"
`), "typo.toml")

	if len(problems) != 1 {
		t.Fatalf("got %d complaints, want 1: %v", len(problems), problems)
	}
	text := problems[0].Error()
	for _, want := range []string{"retention-visible", "colour"} {
		if !strings.Contains(text, want) {
			t.Errorf("the complaint does not mention %q:\n%s", want, text)
		}
	}
	if got := settings.KeepEndedSessions.Duration(); got != 2*24*time.Hour {
		t.Errorf("a typo elsewhere cost us keep-ended-sessions: %s", got)
	}
	if settings.Integration["zellij"].Binary != "agent-notify-zellij" {
		t.Errorf("a typo in a table cost us the rest of the table")
	}
}

func TestBadValuesFallBackIndividually(t *testing.T) {
	settings, problems := config.Parse([]byte(`
keep-ended-sessions = 0
history-messages    = -1

[agent.claude]
binary = "claude"
`), "values.toml")

	if len(problems) != 2 {
		t.Fatalf("got %d complaints, want 2: %v", len(problems), problems)
	}
	defaults := config.Defaults()
	if settings.KeepEndedSessions != defaults.KeepEndedSessions {
		t.Errorf("KeepEndedSessions = %s, want the default", settings.KeepEndedSessions)
	}
	// A negative count falls back to 0 — "keep none" — rather than to the
	// default, which is what config.go says it does.
	if settings.HistoryMessages != 0 {
		t.Errorf("HistoryMessages = %d, want 0", settings.HistoryMessages)
	}
	if settings.Agent["claude"].Binary != "claude" {
		t.Errorf("a good value was thrown away with the bad ones")
	}
}

// durationKeys is every duration in Config, by the key a person writes.
func durationKeys() []string {
	structure := reflect.TypeOf(config.Config{})
	var keys []string
	for i := range structure.NumField() {
		field := structure.Field(i)
		if field.Type == reflect.TypeOf(session.Duration{}) {
			keys = append(keys, field.Tag.Get("toml"))
		}
	}
	return keys
}

// TestOneRetentionDuration guards D-26: the second one was presentation policy
// in core, and whether an ended session is rendered belongs to the subscriber.
//
// It counts retention durations rather than durations. Since the timeouts and
// intervals left this file for named constants beside the code they bound, the
// two counts happen to be the same — but the thing that must stay at one is how
// many answers there are to "how long is this kept".
func TestOneRetentionDuration(t *testing.T) {
	var retention []string
	for _, key := range durationKeys() {
		if strings.Contains(key, "keep") || strings.Contains(key, "retention") {
			retention = append(retention, key)
		}
	}
	if len(retention) != 1 {
		t.Errorf("Config has %d retention durations (%v); want exactly one, keep-ended-sessions",
			len(retention), retention)
	}
}

// TestEveryDurationIsValidated is R18 as a test rather than a promise: a
// duration with no defined behaviour on a bad value is a timeout somebody will
// set to zero and then wonder why nothing happens.
func TestEveryDurationIsValidated(t *testing.T) {
	defaults := config.Defaults()
	for _, key := range durationKeys() {
		settings, problems := config.Parse([]byte(key+" = 0\n"), "zeroed.toml")
		named := false
		for _, problem := range problems {
			if strings.Contains(problem.Error(), key) {
				named = true
			}
		}
		if !named {
			t.Errorf("%s = 0 was accepted in silence: %v", key, problems)
		}
		if !reflect.DeepEqual(settings, defaults) {
			t.Errorf("%s = 0 was not replaced by its default", key)
		}
	}
}

// TestATableWithNoBinaryIsAccepted: the absence is a declaration, not an
// omission (D-81).
//
// It is how a client says core must not run it — both macOS displays and the
// picker say exactly this, and `install` prints exactly this table for all
// three. Complaining here meant complaining about our own printed output, which
// is what it did: three FAILs on a correct config file.
//
// Where running one IS required, the requirement belongs to whatever needs to
// run it. [container] order still refuses a container with no binary, because a
// container that cannot be run cannot answer `focus`
// (containers.TestAContainerWithNoBinaryIsRefusedAndNamed).
func TestATableWithNoBinaryIsAccepted(t *testing.T) {
	settings, problems := config.Parse([]byte("[integration.macos-bar]\n"), "client.toml")
	if len(problems) != 0 {
		t.Fatalf("a client's settings table was complained about: %v", problems)
	}
	if _, present := settings.Integration["macos-bar"]; !present {
		t.Error("the table was accepted and then not kept, so nothing can read its settings")
	}
}

// The two keys that used to describe a container written entirely in
// configuration get a sentence rather than the generic "not recognised", because
// the generic one says a key was ignored and not that the mechanism has gone
// (D-57).

// A file that still carries them is otherwise used. Losing the rest of somebody's
// configuration over a line that no longer does anything would be a worse answer
// than the one it replaced.
func TestARemovedKeyDoesNotCostTheRestOfTheFile(t *testing.T) {
	settings, _ := config.Parse([]byte(`
sweep-interval = "250ms"

[integration.tmux]
binary  = "agent-notify-tmux"
capture = ["TMUX", "TMUX_PANE"]
focus   = ["tmux", "switch-client", "-t", "{TMUX_PANE}"]
`), "old.toml")

	if got := settings.Integration["tmux"].Binary; got != "agent-notify-tmux" {
		t.Errorf("integration.tmux.binary = %q; a removed key cost us the rest", got)
	}
}

func TestDisabledIntegrationsAreNotNagged(t *testing.T) {
	_, problems := config.Parse([]byte("[integration.zellij]\nenabled = false\n"), "off.toml")
	if len(problems) != 0 {
		t.Errorf("a disabled integration was complained about: %v", problems)
	}
}

func TestUnreadableFileIsAComplaintNotACrash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("keep-ended-sessions = \"24h\"\n"), 0o000); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if os.Geteuid() == 0 {
		t.Skip("root reads anything")
	}
	settings, problems := config.Load(path)
	if len(problems) != 1 {
		t.Fatalf("got %d complaints, want 1: %v", len(problems), problems)
	}
	if !reflect.DeepEqual(settings, config.Defaults()) {
		t.Errorf("an unreadable file did not fall back to defaults")
	}
}
