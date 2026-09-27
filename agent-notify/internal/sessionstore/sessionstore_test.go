package sessionstore_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/lassoColombo/agent-notify/internal/config"
	"github.com/lassoColombo/agent-notify/internal/paths"
	"github.com/lassoColombo/agent-notify/internal/sessionstore"
	"github.com/lassoColombo/agent-notify/session"
)

var (
	when = time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	key  = session.Key{Host: "mac", Agent: "claude", SessionID: "abc"}
)

// open gives each test its own store under a short root — short because
// t.TempDir() on macOS is already long enough to break a unix socket path, and
// consistency is worth more than the two lines it saves (§A7.2).
func open(t *testing.T) (*sessionstore.SessionStore, paths.Layout) {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "an-store")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	t.Setenv(paths.TheVariableThatNamesTheRoot, root)

	layout, err := paths.FromEnvironment()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	opened, err := sessionstore.Open(layout, config.Defaults())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return opened, layout
}

func TestApplyThenRead(t *testing.T) {
	opened, layout := open(t)

	message := "on it"
	written, err := opened.Apply(session.Report{
		Key: key, Event: session.UserSentPrompt, Message: &message, Cwd: "/repo",
	}, when)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if written.Kernel != session.Working || written.Sequence != 1 {
		t.Errorf("wrote %q at sequence %d", written.Kernel, written.Sequence)
	}

	read, found, err := opened.Read(key)
	if err != nil || !found {
		t.Fatalf("Read: %v, found %v", err, found)
	}
	if read.Message != message || read.Cwd != "/repo" || !read.UpdatedAt.Equal(when) {
		t.Errorf("read back %+v", read)
	}

	info, err := os.Stat(layout.SessionFile(key.String()))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != paths.FileMode {
		t.Errorf("record mode %o, want %o — it holds what an agent said", got, paths.FileMode)
	}
}

// TestEndingMovesAndResurrectionMovesBack is M4's second "done when", and R6:
// death is a transition, not a deletion.
func TestEndingMovesAndResurrectionMovesBack(t *testing.T) {
	opened, layout := open(t)

	for _, event := range []session.Event{
		session.SessionStarted, session.UserSentPrompt, session.TurnFinished,
	} {
		if _, err := opened.Apply(session.Report{Key: key, Event: event}, when); err != nil {
			t.Fatalf("Apply(%s): %v", event, err)
		}
	}
	ended, err := opened.Apply(session.Report{
		Key: key, Event: session.SessionEnded, Detail: "exited",
	}, when.Add(time.Minute))
	if err != nil {
		t.Fatalf("Apply(session-ended): %v", err)
	}
	if ended.Sequence != 4 {
		t.Fatalf("sequence %d, want 4", ended.Sequence)
	}

	if exists(layout.SessionFile(key.String())) {
		t.Error("the record is still in sessions/ after ending")
	}
	if !exists(layout.EndedFile(key.String())) {
		t.Error("the record is not in ended/ after ending")
	}

	// A day later, you resume it.
	later := when.Add(24 * time.Hour)
	resumed, err := opened.Apply(session.Report{Key: key, Event: session.SessionStarted}, later)
	if err != nil {
		t.Fatalf("Apply(resume): %v", err)
	}
	if resumed.Sequence != 5 {
		t.Errorf("sequence %d, want 5 — it must never restart, or a display sees it go backwards",
			resumed.Sequence)
	}
	if !resumed.CreatedAt.Equal(when) {
		t.Errorf("CreatedAt = %v, want the original %v — a return is not a birth", resumed.CreatedAt, when)
	}
	if !resumed.EndedAt.IsZero() {
		t.Errorf("EndedAt = %v, want it cleared", resumed.EndedAt)
	}
	if !exists(layout.SessionFile(key.String())) || exists(layout.EndedFile(key.String())) {
		t.Error("the record did not move back to sessions/ on resume")
	}
}

// TestDuplicateFilesResolveBySequence covers the crash that can leave a record
// in both directories: the destination is written before the source is removed,
// because two files recover and zero do not.
func TestDuplicateFilesResolveBySequence(t *testing.T) {
	opened, layout := open(t)

	if _, err := opened.Apply(session.Report{Key: key, Event: session.UserSentPrompt}, when); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	stale, err := os.ReadFile(layout.SessionFile(key.String()))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if _, err := opened.Apply(session.Report{Key: key, Event: session.SessionEnded}, when); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	// Put the pre-move record back, as a crash between write and remove would.
	if err := os.WriteFile(layout.SessionFile(key.String()), stale, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	read, found, err := opened.Read(key)
	if err != nil || !found {
		t.Fatalf("Read: %v, found %v", err, found)
	}
	if read.Kernel != session.Ended || read.Sequence != 2 {
		t.Errorf("read %q at sequence %d, want the later write to win", read.Kernel, read.Sequence)
	}

	// And the next write cleans up after the crash.
	if _, err := opened.Apply(session.Report{Key: key, Event: session.SessionEnded}, when); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if exists(layout.SessionFile(key.String())) {
		t.Error("the duplicate survived a subsequent write")
	}
}

func TestHistoryRecordsWhatChangedAndNothingElse(t *testing.T) {
	opened, _ := open(t)

	say := func(text string, event session.Event) {
		t.Helper()
		if _, err := opened.Apply(session.Report{
			Key: key, Event: event, Message: &text,
		}, when); err != nil {
			t.Fatalf("Apply: %v", err)
		}
	}
	say("starting", session.UserSentPrompt)
	say("still going", session.AgentProgressed)
	say("still going", session.AgentProgressed) // same text, same state
	say("done", session.TurnFinished)

	history, err := opened.History(key)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(history.Messages) != 3 {
		t.Errorf("kept %d messages, want 3 — repeating itself is not saying something new: %+v",
			len(history.Messages), history.Messages)
	}
	if len(history.Changes) != 2 {
		t.Errorf("kept %d changes, want working then finished-a-turn: %+v",
			len(history.Changes), history.Changes)
	}
	if last := history.Changes[len(history.Changes)-1]; last.From != session.Working ||
		last.To != session.FinishedATurn {
		t.Errorf("last change was %q -> %q", last.From, last.To)
	}
	if first := history.Messages[0]; first.Kernel != session.Working {
		t.Errorf("a message does not carry the state it was said in: %+v", first)
	}
}

func TestHistoryIsBoundedByCount(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "an-store")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	t.Setenv(paths.TheVariableThatNamesTheRoot, root)
	layout, err := paths.FromEnvironment()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	settings := config.Defaults()
	settings.HistoryMessages = 3
	settings.HistoryChanges = 2
	opened, err := sessionstore.Open(layout, settings)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	for i := range 10 {
		text := "message " + strconv.Itoa(i)
		event := session.TurnFinished
		if i%2 == 0 {
			event = session.UserSentPrompt
		}
		if _, err := opened.Apply(session.Report{
			Key: key, Event: event, Message: &text,
		}, when); err != nil {
			t.Fatalf("Apply: %v", err)
		}
	}

	history, err := opened.History(key)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(history.Messages) != 3 || len(history.Changes) != 2 {
		t.Fatalf("kept %d messages and %d changes, want 3 and 2",
			len(history.Messages), len(history.Changes))
	}
	if history.Messages[2].Message != "message 9" {
		t.Errorf("kept %q as the newest; the tail is what anybody asked for",
			history.Messages[2].Message)
	}
}

func TestHistoryCanBeTurnedOff(t *testing.T) {
	root, _ := os.MkdirTemp("/tmp", "an-store")
	t.Cleanup(func() { os.RemoveAll(root) })
	t.Setenv(paths.TheVariableThatNamesTheRoot, root)
	layout, _ := paths.FromEnvironment()

	settings := config.Defaults()
	settings.HistoryMessages = 0
	settings.HistoryChanges = 0
	opened, err := sessionstore.Open(layout, settings)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	text := "something private"
	if _, err := opened.Apply(session.Report{
		Key: key, Event: session.TurnFinished, Message: &text,
	}, when); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if exists(layout.HistoryFile(key.String())) {
		t.Error("a history file was written despite both bounds being zero (§A15)")
	}
}

func TestPruneForgetsOnlyWhatIsPastItsWelcome(t *testing.T) {
	opened, layout := open(t)

	live := session.Key{Host: "mac", Agent: "claude", SessionID: "live"}
	fresh := session.Key{Host: "mac", Agent: "claude", SessionID: "fresh"}
	stale := session.Key{Host: "mac", Agent: "claude", SessionID: "stale"}

	if _, err := opened.Apply(session.Report{Key: live, Event: session.UserSentPrompt}, when); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	for _, k := range []session.Key{fresh, stale} {
		text := "bye"
		if _, err := opened.Apply(session.Report{
			Key: k, Event: session.SessionEnded, Message: &text,
		}, when); err != nil {
			t.Fatalf("Apply: %v", err)
		}
	}
	// Age only the stale one, by rewriting its ended_at.
	age(t, layout.EndedFile(stale.String()), when.Add(-30*24*time.Hour))

	removed, err := opened.ForgetWhatIsTooOld(when)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(removed) != 1 || removed[0] != stale {
		t.Fatalf("Prune removed %v, want only the stale one", removed)
	}
	if exists(layout.HistoryFile(stale.String())) {
		t.Error("pruning a session left its history behind — what it said outlived it")
	}
	if !exists(layout.EndedFile(fresh.String())) {
		t.Error("an ended session still within its window was forgotten")
	}
	if !exists(layout.SessionFile(live.String())) {
		t.Error("a live session was pruned")
	}
}

// TestPruneNeverForgetsWhatItCannotDate is R5: not knowing must never become
// deleting.
func TestPruneNeverForgetsWhatItCannotDate(t *testing.T) {
	opened, layout := open(t)
	if _, err := opened.Apply(session.Report{Key: key, Event: session.SessionEnded}, when); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	age(t, layout.EndedFile(key.String()), time.Time{})

	removed, err := opened.ForgetWhatIsTooOld(when.Add(365 * 24 * time.Hour))
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("Prune removed %v — a record with no ended_at has no age to be past", removed)
	}
}

func TestUpdateStampsWhatACallerMayNotSet(t *testing.T) {
	opened, _ := open(t)
	if _, err := opened.Apply(session.Report{Key: key, Event: session.BlockedOnHuman}, when); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	later := when.Add(time.Hour)
	named, err := opened.Update(key, later, func(previous session.Record) session.Record {
		next := previous.Clone()
		next.Name = "the-refactor"
		next.Sequence = 9999 // a caller does not get to decide this
		return next
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if named.Sequence != 2 {
		t.Errorf("Sequence = %d, want 2: the store stamps it, never the caller", named.Sequence)
	}
	if !named.UpdatedAt.Equal(later) {
		t.Errorf("UpdatedAt = %v, want %v", named.UpdatedAt, later)
	}
	if !named.StateSince.Equal(when) {
		t.Errorf("StateSince = %v, want %v — naming a session is not a state change, and "+
			"\"waiting 12m\" has to survive it", named.StateSince, when)
	}
	if named.Name != "the-refactor" {
		t.Errorf("Name = %q", named.Name)
	}
}

func TestListSeparatesLiveFromEnded(t *testing.T) {
	opened, _ := open(t)
	for i := range 3 {
		k := session.Key{Host: "mac", Agent: "claude", SessionID: "s" + strconv.Itoa(i)}
		event := session.UserSentPrompt
		if i == 2 {
			event = session.SessionEnded
		}
		if _, err := opened.Apply(session.Report{Key: k, Event: event}, when); err != nil {
			t.Fatalf("Apply: %v", err)
		}
	}

	live, err := opened.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	ended, err := opened.ListEnded()
	if err != nil {
		t.Fatalf("ListEnded: %v", err)
	}
	if len(live) != 2 || len(ended) != 1 {
		t.Errorf("got %d live and %d ended, want 2 and 1", len(live), len(ended))
	}
}

func TestOneUnreadableRecordDoesNotCostTheOthers(t *testing.T) {
	opened, layout := open(t)
	for i := range 3 {
		k := session.Key{Host: "mac", Agent: "claude", SessionID: "s" + strconv.Itoa(i)}
		if _, err := opened.Apply(session.Report{Key: k, Event: session.UserSentPrompt}, when); err != nil {
			t.Fatalf("Apply: %v", err)
		}
	}
	bad := session.Key{Host: "mac", Agent: "claude", SessionID: "s1"}
	if err := os.WriteFile(layout.SessionFile(bad.String()), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	live, err := opened.List()
	if err == nil {
		t.Error("List did not report the unreadable record at all")
	}
	if len(live) != 2 {
		t.Errorf("got %d records, want the 2 that were fine", len(live))
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// age rewrites a stored record's ended_at, which is the only way to make a
// session old without waiting.
func age(t *testing.T, path string, at time.Time) {
	t.Helper()
	var fields map[string]json.RawMessage
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if at.IsZero() {
		delete(fields, "ended_at")
	} else {
		stamped, err := json.Marshal(at)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		fields["ended_at"] = stamped
	}
	rewritten, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(path, rewritten, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_ = filepath.Base(path)
}
