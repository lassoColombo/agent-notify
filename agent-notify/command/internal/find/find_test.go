package find_test

import (
	"testing"
	"time"

	"github.com/lassoColombo/agent-notify/command/internal/find"
	"github.com/lassoColombo/agent-notify/internal/core"
	"github.com/lassoColombo/agent-notify/internal/paths"
	"github.com/lassoColombo/agent-notify/session"
)

// TestALiveSessionWinsItsName: the completion offers the live sessions, so the
// name it offers has to find the live one even when an ended session went by
// the same name, which is the ordinary case for a name somebody chose and
// keeps choosing.
func TestALiveSessionWinsItsName(t *testing.T) {
	opened := aCore(t)
	now := time.Now().UTC()
	for _, report := range []session.Report{
		{Key: session.Key{Host: "mac", Agent: "fake", SessionID: "gone-one"}, Event: session.UserSentPrompt, Name: "shared"},
		{Key: session.Key{Host: "mac", Agent: "fake", SessionID: "gone-one"}, Event: session.SessionEnded},
		{Key: session.Key{Host: "mac", Agent: "fake", SessionID: "here-one"}, Event: session.UserSentPrompt, Name: "shared"},
	} {
		if _, err := opened.Store.Apply(report, now); err != nil {
			t.Fatal(err)
		}
	}

	found, err := find.Session(opened, "shared")
	if err != nil || found.Key.SessionID != "here-one" {
		t.Errorf("found %s, %v; want the live session", found.Key.SessionID, err)
	}

	// And an ended session is still found when it is the only one.
	found, err = find.Session(opened, "gone-one")
	if err != nil || found.Key.SessionID != "gone-one" {
		t.Errorf("found %s, %v; want the ended session it names", found.Key.SessionID, err)
	}
}

func aCore(t *testing.T) *core.Core {
	t.Helper()
	layout, err := paths.Under(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	opened, err := core.OpenAt(layout, "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(opened.Close)
	return opened
}
