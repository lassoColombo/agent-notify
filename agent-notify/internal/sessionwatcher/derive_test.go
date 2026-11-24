package sessionwatcher_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lassoColombo/agent-notify/internal/config"
	"github.com/lassoColombo/agent-notify/internal/sessionstore"
	"github.com/lassoColombo/agent-notify/session"
)

// aSlowContainer writes one that answers `capabilities` like a container and
// then takes this long to answer `interpret-environment`.
func aSlowContainer(t *testing.T, directory, name string, takes time.Duration) string {
	t.Helper()
	binary := filepath.Join(directory, "agent-notify-"+name)
	script := fmt.Sprintf("#!/bin/sh\ncase \"$1\" in\n"+
		"  %s) printf '%%s\\n' '{\"version\":\"0.0.0-dev\",\"methods\":[\"interpret-environment\",\"focus\",\"focused\"]}' ;;\n"+
		"  %s) sleep %d; printf '%%s\\n' '{\"pane\":\"placed\"}' ;;\n  *) exit 1 ;;\nesac\n",
		session.CapabilitiesCommand, session.MethodInterpret, int(takes/time.Second))
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return binary
}

// TestASlowContainerDoesNotDelayADraw is why derivation is not on the loop
// (D-91). A container's `interpret-environment` may talk to its tool and take
// seconds; while it did, every wake sat behind it and nothing was drawn.
func TestASlowContainerDoesNotDelayADraw(t *testing.T) {
	atATestablePace(t, 200*time.Millisecond)
	root := shortRoot(t)
	_, painted := aDisplay(t, root, "painter", "kernel")
	aSlowContainer(t, root, "placer", 3*time.Second)

	layout := running(t, root)
	waitFor(t, "the first render", func() bool {
		return len(everythingPainted(t, painted)) > 0
	})

	// One session with something for the placer to interpret, which starts
	// the three seconds, then a second with nothing: it is drawn while the
	// placer is still thinking about the first.
	applyToTheStore(t, layout, session.Report{
		Key:     session.Key{Host: "mac", Agent: "fake", SessionID: "placed"},
		Event:   session.TurnFinished,
		Process: session.Process{PID: os.Getpid()},
		CapturedContext: &session.CapturedContext{
			By: map[string]json.RawMessage{"placer": json.RawMessage(`{"pane":"1"}`)},
		},
	})
	time.Sleep(500 * time.Millisecond)
	before := time.Now()
	aTurnFinished(t, layout, "plain")
	waitFor(t, "the second session to be drawn", func() bool {
		views := everythingPainted(t, painted)
		return len(views) > 0 && len(views[len(views)-1].Sessions) == 2
	})
	if waited := time.Since(before); waited > 2*time.Second {
		t.Errorf("the second session was drawn after %s: the draw waited for the container", waited)
	}

	// And the placer's answer still lands, under its own name.
	waitFor(t, "the coordinates to be derived", func() bool {
		settings, _ := config.Load(layout)
		store, err := sessionstore.Open(layout, settings)
		if err != nil {
			return false
		}
		record, found, _ := store.Read(session.Key{Host: "mac", Agent: "fake", SessionID: "placed"})
		return found && string(record.DerivedContext["placer"]) == `{"pane":"placed"}`
	})
}

// TestAnEmptyCaptureIsAskedAboutOnce: a container that captured nothing can
// still place by the chain, so it is asked (D-135); and one that answers
// `null` has said there is nothing to place the session by, which is stored
// and not asked about again however often the store moves.
func TestAnEmptyCaptureIsAskedAboutOnce(t *testing.T) {
	atATestablePace(t, 200*time.Millisecond)
	root := shortRoot(t)
	placer := filepath.Join(root, "agent-notify-placer")
	asked := filepath.Join(root, "placer.asked")
	write(t, placer, fmt.Sprintf("#!/bin/sh\ncase \"$1\" in\n"+
		"  %s) printf '%%s\\n' '{\"version\":\"0.0.0-dev\",\"methods\":[\"interpret-environment\",\"focus\",\"focused\"]}' ;;\n"+
		"  %s) echo asked >> %s; printf '%%s\\n' null ;;\n  *) exit 1 ;;\nesac\n",
		session.CapabilitiesCommand, session.MethodInterpret, asked))
	if err := os.Chmod(placer, 0o700); err != nil {
		t.Fatal(err)
	}

	layout := running(t, root)
	key := session.Key{Host: "mac", Agent: "fake", SessionID: "nowhere"}
	applyToTheStore(t, layout, session.Report{
		Key:     key,
		Event:   session.TurnFinished,
		Process: session.Process{PID: os.Getpid()},
		CapturedContext: &session.CapturedContext{
			By: map[string]json.RawMessage{"placer": json.RawMessage(`{}`)},
		},
	})
	waitFor(t, "the answer to be stored", func() bool {
		settings, _ := config.Load(layout)
		store, err := sessionstore.Open(layout, settings)
		if err != nil {
			return false
		}
		record, found, _ := store.Read(key)
		answer, stored := record.DerivedContext["placer"]
		return found && stored && string(answer) == "null"
	})

	for i := range 3 {
		somethingWasSaid(t, layout, "nowhere", fmt.Sprintf("said %d", i))
		time.Sleep(300 * time.Millisecond)
	}
	content, _ := os.ReadFile(asked)
	if times := strings.Count(string(content), "asked"); times != 1 {
		t.Errorf("the placer was asked %d times about one capture, want once", times)
	}
}

// TestAContainerIsHandedItsOwnEntryAndTheChain is the shape of what arrives
// on `interpret-environment`'s stdin (D-93): the record's captured_context
// with this container's entry and nobody else's, and the chain core walked.
func TestAContainerIsHandedItsOwnEntryAndTheChain(t *testing.T) {
	atATestablePace(t, 200*time.Millisecond)
	root := shortRoot(t)
	placer := filepath.Join(root, "agent-notify-placer")
	handed := filepath.Join(root, "placer.handed")
	write(t, placer, fmt.Sprintf("#!/bin/sh\ncase \"$1\" in\n"+
		"  %s) printf '%%s\\n' '{\"version\":\"0.0.0-dev\",\"methods\":[\"interpret-environment\",\"focus\",\"focused\"]}' ;;\n"+
		"  %s) cat > %s; printf '%%s\\n' '{\"placed\":true}' ;;\n  *) exit 1 ;;\nesac\n",
		session.CapabilitiesCommand, session.MethodInterpret, handed))
	if err := os.Chmod(placer, 0o700); err != nil {
		t.Fatal(err)
	}

	layout := running(t, root)
	applyToTheStore(t, layout, session.Report{
		Key:     session.Key{Host: "mac", Agent: "fake", SessionID: "placed"},
		Event:   session.TurnFinished,
		Process: session.Process{PID: os.Getpid()},
		CapturedContext: &session.CapturedContext{
			Ancestry: []session.Ancestor{{PID: 7, Command: "claude"}, {PID: 3, Command: "ghostty"}},
			By: map[string]json.RawMessage{
				"placer": json.RawMessage(`{"pane":"1"}`),
				"other":  json.RawMessage(`{"secret":"not for the placer"}`),
			},
		},
	})

	var given session.CapturedContext
	waitFor(t, "the placer to be asked", func() bool {
		content, err := os.ReadFile(handed)
		return err == nil && json.Unmarshal(content, &given) == nil
	})
	if len(given.Ancestry) != 2 || given.Ancestry[1].Command != "ghostty" {
		t.Errorf("the placer was handed the chain %+v, want the two rungs that were captured", given.Ancestry)
	}
	if string(given.By["placer"]) != `{"pane":"1"}` {
		t.Errorf("the placer was handed %s as its own entry, want what it captured", given.By["placer"])
	}
	if _, leaked := given.By["other"]; leaked || len(given.By) != 1 {
		t.Errorf("the placer was handed %d entries %v, want only its own", len(given.By), given.By)
	}
	if given.CapturedAt.IsZero() {
		t.Error("the placer was handed no captured_at, want the one the store stamped")
	}
}
