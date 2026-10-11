package config_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lassoColombo/agent-notify/internal/config"
	"github.com/lassoColombo/agent-notify/internal/paths"
	"github.com/lassoColombo/agent-notify/session"
)

func TestMissingFileIsNotAComplaint(t *testing.T) {
	layout, _ := paths.Under(t.TempDir())
	settings, problems := config.Load(layout)
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
	settings, problems := config.Parse([]byte("keep-ended-sessions: [\nthis is not yaml ]]"), "broken.yaml")
	if len(problems) != 1 {
		t.Fatalf("got %d complaints, want exactly 1: %v", len(problems), problems)
	}
	if !reflect.DeepEqual(settings, config.Defaults()) {
		t.Errorf("a malformed file did not fall back to defaults")
	}
	if !strings.Contains(problems[0].Error(), "broken.yaml") {
		t.Errorf("the complaint does not name the file:\n%v", problems[0])
	}
}

func TestAWholeFileIsRead(t *testing.T) {
	settings, problems := config.Parse([]byte(`
keep-ended-sessions: 72h
integrations:
  zellij:
    settings:
      glyphs: { working: "*" }
  sketchybar:
    enabled: false
  aerospace: {}
containers: [aerospace, zellij]
`), "config.yaml")

	for _, problem := range problems {
		t.Errorf("unexpected complaint: %v", problem)
	}
	if got := settings.KeepEndedSessions.Duration(); got != 3*24*time.Hour {
		t.Errorf("KeepEndedSessions = %s, want 72h", got)
	}
	if !settings.Integrations["zellij"].IsEnabled() {
		t.Errorf("an integration with no enabled key should be enabled")
	}
	if settings.Integrations["sketchybar"].IsEnabled() {
		t.Errorf("enabled = false was ignored")
	}
	if got := settings.Containers; len(got) != 2 || got[0] != "aerospace" {
		t.Errorf("containers = %q", got)
	}

	// Settings reach the integration verbatim and core never looks inside
	// (plan.md §A17 R7) — but they must survive the trip.
	glyphs, ok := settings.Integrations["zellij"].Settings["glyphs"].(map[string]any)
	if !ok {
		t.Fatalf("settings.glyphs did not survive decoding: %#v", settings.Integrations["zellij"].Settings)
	}
	if glyphs["working"] != "*" {
		t.Errorf("settings.glyphs.working = %v", glyphs["working"])
	}
}

func TestUnknownKeysAreReportedAndTheRestSurvives(t *testing.T) {
	settings, problems := config.Parse([]byte(`
keep-ended-sessions: 48h
retention-visible: 1m
integrations:
  zellij:
    colour: blue
`), "typo.yaml")

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
	if _, kept := settings.Integrations["zellij"]; !kept {
		t.Errorf("a typo in a table cost us the table")
	}
}

func TestBadValuesFallBackIndividually(t *testing.T) {
	settings, problems := config.Parse([]byte(`
keep-ended-sessions: 0
history-messages: -1
integrations:
  zellij: {}
`), "values.yaml")

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
	if _, kept := settings.Integrations["zellij"]; !kept {
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
			keys = append(keys, field.Tag.Get("yaml"))
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
		settings, problems := config.Parse([]byte(key+": 0\n"), "zeroed.yaml")
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

// TestAnEntryAloneIsKept: a name with nothing under it is a complete entry,
// kept so that its settings can be asked for (D-102).
func TestAnEntryAloneIsKept(t *testing.T) {
	settings, problems := config.Parse([]byte("integrations:\n  macos-notifier:\n"), "client.yaml")
	if len(problems) != 0 {
		t.Fatalf("an entry with nothing under it was complained about: %v", problems)
	}
	if _, present := settings.Integrations["macos-notifier"]; !present {
		t.Error("the entry was accepted and then not kept, so nothing can read its settings")
	}
}

// TestARemovedKeyDoesNotCostTheRestOfTheFile: losing the rest of somebody's
// configuration over a line that no longer does anything would be a worse
// answer than the one it replaced.
func TestARemovedKeyDoesNotCostTheRestOfTheFile(t *testing.T) {
	settings, _ := config.Parse([]byte(`
sweep-interval: 250ms
integrations:
  tmux:
    enabled: true
    capture: [TMUX, TMUX_PANE]
`), "old.yaml")

	if !settings.Integrations["tmux"].IsEnabled() {
		t.Errorf("a removed key cost us the rest of the entry")
	}
}

// TestWhatIsOnPATHIsWhatRuns is D-102: the set is PATH, the file turns one
// off, and a name in the file with no program behind it is a complaint.
func TestWhatIsOnPATHIsWhatRuns(t *testing.T) {
	bin := t.TempDir()
	for _, name := range []string{"agent-notify-painter", "agent-notify-placer"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	root := t.TempDir()
	layout, _ := paths.Under(root)
	if err := os.WriteFile(layout.ConfigFile, []byte("integrations:\n  placer:\n    enabled: false\n  ghost:\n    enabled: false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, problems := config.Load(layout)
	if got := settings.Enabled(); !reflect.DeepEqual(got, []string{"painter"}) {
		t.Errorf("Enabled = %v, want painter alone: placer is turned off and ghost is not there", got)
	}
	if len(problems) != 1 || !strings.Contains(problems[0].Error(), "ghost") {
		t.Errorf("problems = %v, want one naming ghost, which is written and not on PATH", problems)
	}
}

func TestDisabledIntegrationsAreNotNagged(t *testing.T) {
	_, problems := config.Parse([]byte("integrations:\n  zellij:\n    enabled: false\n"), "off.yaml")
	if len(problems) != 0 {
		t.Errorf("a disabled integration was complained about: %v", problems)
	}
}

func TestUnreadableFileIsAComplaintNotACrash(t *testing.T) {
	layout, _ := paths.Under(t.TempDir())
	if err := os.WriteFile(layout.ConfigFile, []byte("keep-ended-sessions = \"24h\"\n"), 0o000); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if os.Geteuid() == 0 {
		t.Skip("root reads anything")
	}
	t.Setenv("PATH", t.TempDir())
	settings, problems := config.Load(layout)
	if len(problems) != 1 {
		t.Fatalf("got %d complaints, want 1: %v", len(problems), problems)
	}
	if !reflect.DeepEqual(settings, config.Defaults()) {
		t.Errorf("an unreadable file did not fall back to defaults")
	}
}
