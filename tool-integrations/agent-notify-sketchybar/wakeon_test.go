package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	agentnotify "github.com/lassoColombo/agent-notify"
	"github.com/lassoColombo/agent-notify/subscribe"
)

// These drive the whole program — the SDK's subscriber loop, a real socket, a
// real handshake — against a session-watcher with no agents behind it, and
// watch what the bar is TOLD rather than what it draws.
//
// That is a different question from sketchybar_test.go, which runs against the
// real bar on purpose because everything it asserts is a fact about sketchybar.
// Nothing here is: what is being tested is the wiring between what this display
// declares and what it renders, and a recording script sees that perfectly.

// recorder stands in for sketchybar and writes down every invocation.
func recorder(t *testing.T) (Sketchybar, func() string) {
	t.Helper()
	directory := t.TempDir()
	log := filepath.Join(directory, "told")
	script := filepath.Join(directory, "sketchybar")

	// "$@" on one line per invocation. --query has to answer something that
	// parses, or Seen treats every announcement as already looked at.
	body := "#!/bin/sh\necho \"$@\" >> " + log + "\nif [ \"$1\" = \"--query\" ]; then echo '{}'; fi\nexit 0\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatalf("writing the recorder: %v", err)
	}
	return Sketchybar{Binary: script, Timeout: 5 * time.Second}, func() string {
		written, err := os.ReadFile(log)
		if err != nil {
			return ""
		}
		return string(written)
	}
}

// painting brings the display up against a fake session-watcher and hands back
// everything the bar has been told so far.
func painting(t *testing.T, settings Resolved) (*subscribe.Fake, func() string) {
	t.Helper()
	// Not t.TempDir(): on macOS its path, with a test name in it, is already
	// long enough that appending a socket name exceeds what a unix socket may
	// be (§A7.2).
	root, err := os.MkdirTemp("/tmp", "an-sb")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })

	fake, err := subscribe.StartFake(root, nil)
	if err != nil {
		t.Fatalf("StartFake: %v", err)
	}
	t.Cleanup(func() { fake.Stop() })

	bar, told := recorder(t)
	display := &Display{
		Sketchybar: bar,
		Bar: Bar{
			Prefix: Name, Position: settings.Position, Rows: settings.Rows,
			Popup: settings.Popup, Announce: settings.Announce,
			Preview: settings.Preview, Sketchybar: bar.Binary, Core: "/usr/bin/true",
			Glyphs: settings.Glyphs, Colours: settings.Colours,
		},
		Logger: slog.New(slog.DiscardHandler), structure: true,
		wake: make(chan struct{}, 1),
	}

	ctx, stop := context.WithCancel(context.Background())
	t.Cleanup(stop)
	go subscribe.Run(ctx, subscribe.Integration{
		Name: Name, Roles: []string{"display"}, Root: root,
		WakeOn: WhatToWakeFor(settings), OnChange: display.Render,
	})
	waitFor(t, "the display to connect", func() bool { return len(fake.Connected()) == 1 })
	return fake, told
}

func waitFor(t *testing.T, what string, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("waited for %s and it never happened", what)
}

func working(id, message string) agentnotify.Record {
	return agentnotify.Record{
		Key:        agentnotify.Key{Host: "mac", Agent: "claude", SessionID: id},
		Name:       id,
		Kernel:     agentnotify.Working,
		Rank:       agentnotify.RankWorking,
		StateSince: time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC),
		Message:    message,
	}
}

// TestASecondPromptAtAWorkingAgentReachesThePreview is D-72.
//
// Queueing a prompt at an agent that is already working moves the message and
// nothing else: UserSentPrompt reduces to Working, which it already was, so the
// kernel does not change and state_since does not move, and Claude's
// UserPromptSubmit carries no detail. Before this the bar was not woken, and
// the popup went on showing the previous prompt for as long as the agent kept
// working.
func TestASecondPromptAtAWorkingAgentReachesThePreview(t *testing.T) {
	settings := previewing()
	fake, told := painting(t, settings)

	fake.Publish(working("alpha", "summarise the diff"))
	waitFor(t, "the first prompt to be drawn", func() bool {
		return strings.Contains(told(), "summarise the diff")
	})

	fake.Publish(working("alpha", "actually, rebase it first"))
	waitFor(t, "the second prompt to reach the preview", func() bool {
		return strings.Contains(told(), "actually, rebase it first")
	})
}

// TestNoPreviewMeansTheMessageIsNotAskedFor, which is the other half of making
// the declaration conditional: a bar with no popup under it must not be woken
// once per prompt to redraw something it does not show.
func TestNoPreviewMeansTheMessageIsNotAskedFor(t *testing.T) {
	if slices.Contains(WhatToWakeFor(Resolved{}), "message") {
		t.Error("a display with no preview asks to be woken for the message")
	}
	if !slices.Contains(WhatToWakeFor(previewing()), "message") {
		t.Error("a display with a preview does not ask for the message")
	}
}

// previewing is the settings of a bar that has a popup under it.
func previewing() Resolved {
	return Resolved{
		Position: defaultPosition, Rows: defaultRows,
		Glyphs:  agentnotify.NewPalette(DefaultGlyphs, nil),
		Colours: agentnotify.NewPalette(DefaultColours, nil),
		Popup:   defaultPopup,
		Preview: defaultPreview,
	}
}

// TestEveryFieldTheRenderReadsIsOneItWakesFor is the general form of D-72, and
// the reason it is a sweep rather than a list.
//
// D-72 was found by reading the render and the declaration side by side and
// noticing they disagreed about one field. That works exactly once. This asks
// the renderer instead: move one field, render again, and if the output moved
// then the renderer reads that field and the declaration has to name it.
// Nobody has to remember, and a field added to a chip next year fails here the
// day it is added.
func TestEveryFieldTheRenderReadsIsOneItWakesFor(t *testing.T) {
	settings := previewing()
	bar := Bar{
		Prefix: Name, Position: settings.Position, Rows: settings.Rows,
		Popup: settings.Popup, Preview: settings.Preview, Announce: settings.Announce,
		Glyphs: settings.Glyphs, Colours: settings.Colours,
		Sketchybar: "/usr/bin/true", Core: "/usr/bin/true",
		Now: time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC),
	}
	drawn := func(record agentnotify.Record) string {
		commands, _ := Render(bar, []agentnotify.Record{record}, agentnotify.Arrival{}, false)
		return strings.Join(commands, "\n")
	}

	base := working("alpha", "summarise the diff")
	base.Detail = "running-bash"
	wakeOn := WhatToWakeFor(settings)

	for field, moved := range agentnotify.EachFieldMoved(base) {
		if drawn(base) == drawn(moved) {
			continue
		}
		if !slices.Contains(wakeOn, field) {
			t.Errorf("the bar draws differently when %q moves, and this display does not "+
				"ask to be woken for it: the change would reach the bar only when something "+
				"else happened to move too", field)
		}
	}
}
