package session_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lassoColombo/agent-notify/session"
)

// TestAKernelThisBuildHasNeverHeardOfIsCarriedNotBuried.
//
// A record can hold a state this binary has no name for — one written before a
// state was renamed, or by a build ahead of this one on the same machine. It is
// read, kept, reported live, and sorted by the rank that came with it, because
// failing to understand a state is not a licence to decide what it was (R5,
// §A5.5).
func TestAKernelThisBuildHasNeverHeardOfIsCarriedNotBuried(t *testing.T) {
	strange := []byte(`{
		"key": {"host":"mac","agent":"claude","session_id":"abc"},
		"sequence": 41,
		"kernel": "hibernating",
		"rank": 25,
		"detail": "dreaming",
		"state_since": "2026-09-17T12:00:00Z",
		"created_at": "2026-09-17T11:00:00Z",
		"updated_at": "2026-09-17T12:00:00Z",
		"message": "zzz"
	}`)

	var record session.Record
	if err := json.Unmarshal(strange, &record); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if record.Kernel != session.Kernel("hibernating") || record.Rank != 25 {
		t.Errorf("an unknown kernel was not carried through: %q rank %d", record.Kernel, record.Rank)
	}
	if record.Kernel.Known() {
		t.Error("an invented kernel reported itself as known")
	}
	if !record.Kernel.Live() {
		t.Error("an unknown kernel must be live: not knowing is not a licence to bury it")
	}

	// And it is only once the state actually moves that this build's own rank
	// is stamped over the one that arrived.
	next := session.Apply(record, session.Report{Key: key, Event: session.TurnFinished, Detail: "answered"}, later)
	written, err := json.Marshal(next)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var reread map[string]json.RawMessage
	if err := json.Unmarshal(written, &reread); err != nil {
		t.Fatalf("Unmarshal what we wrote: %v", err)
	}
	if got := string(reread["sequence"]); got != "42" {
		t.Errorf("sequence = %s, want 42", got)
	}
	if got := string(reread["rank"]); got != "30" {
		t.Errorf("rank = %s, want the rank of the state we just moved to", got)
	}
}

// TestAFieldThisBuildDoesNotKnowIsDroppedRatherThanKept. Nothing round-trips
// any more (D-77): every reader and writer of a record is built from the same
// source and released together, so a key that is not a field is somebody's
// leftover and carrying it forward on every write would preserve it for ever.
func TestAFieldThisBuildDoesNotKnowIsDroppedRatherThanKept(t *testing.T) {
	leftover := []byte(`{
		"key": {"host":"mac","agent":"claude","session_id":"abc"},
		"sequence": 41,
		"kernel": "working",
		"rank": 30,
		"quota": [{"name":"weekly","used":0.93}],
		"agent_data": {"prompt_id":"2e87792c"}
	}`)

	var record session.Record
	if err := json.Unmarshal(leftover, &record); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	written, err := json.Marshal(session.Apply(record, session.Report{
		Key: key, Event: session.TurnFinished, Detail: "answered",
	}, later))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, gone := range []string{"quota", "agent_data"} {
		if strings.Contains(string(written), `"`+gone+`"`) {
			t.Errorf("a record rewritten by this build still carries %q: %s", gone, written)
		}
	}
}

// TestFieldOrderIsReadable: these files are read by people with cat, and by jq.
func TestFieldOrderIsReadable(t *testing.T) {
	record := session.Apply(session.Record{}, session.Report{Key: key, Event: session.SessionStarted}, when)
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.HasPrefix(string(encoded), `{"key":{"host":"mac"`) {
		t.Errorf("a record does not start with its identity: %s", encoded)
	}
	if strings.Index(string(encoded), `"kernel"`) > strings.Index(string(encoded), `"updated_at"`) {
		t.Errorf("the state is buried below the timestamps: %s", encoded)
	}
}

func TestEmptyFieldsAreOmitted(t *testing.T) {
	record := session.Apply(session.Record{}, session.Report{Key: key, Event: session.SessionStarted}, when)
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, absent := range []string{"detail", "name", "cwd", "message", "process", "captured_context", "annotations", "agent_data", "ended_at"} {
		if strings.Contains(string(encoded), `"`+absent+`"`) {
			t.Errorf("%q was written despite being empty: %s", absent, encoded)
		}
	}
}

func TestKeyRoundTrips(t *testing.T) {
	keys := []session.Key{
		{Host: "mac", Agent: "claude", SessionID: "0e2804a7-a0e2-4a57"},
		{Host: "host~with~tildes", Agent: "co dex", SessionID: "a/b/../c"},
		{Host: "Ünicode", Agent: "claude", SessionID: "%41"},
		{Host: "h", Agent: "a", SessionID: "."},
	}
	for _, want := range keys {
		token := want.String()
		if strings.ContainsAny(token, "/\\") || strings.Count(token, "~") != 2 {
			t.Errorf("%v encoded to %q, which is not a safe filename with two separators", want, token)
		}
		got, err := session.ParseKey(token)
		if err != nil {
			t.Errorf("ParseKey(%q): %v", token, err)
			continue
		}
		if got != want {
			t.Errorf("ParseKey(%q) = %v, want %v", token, got, want)
		}
	}
}

func TestKeyValidation(t *testing.T) {
	if err := (session.Key{Agent: "claude", SessionID: "x"}).ReasonThisKeyCannotBeUsed(); err == nil {
		t.Error("a key with no host was accepted")
	}
	long := session.Key{Host: strings.Repeat("h", 300), Agent: "claude", SessionID: "x"}
	if err := long.ReasonThisKeyCannotBeUsed(); err == nil {
		t.Error("a key too long to be a filename was accepted")
	}
	for _, bad := range []string{"mac~claude", "mac~claude~a~b", "mac~claude~%ZZ", "mac~claude~%4"} {
		if _, err := session.ParseKey(bad); err == nil {
			t.Errorf("ParseKey(%q) succeeded", bad)
		}
	}
}

func TestZeroTimesAreNotWritten(t *testing.T) {
	record := session.Record{Key: key, Kernel: session.Idle, CreatedAt: when}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(encoded), "0001-01-01") {
		t.Errorf("a zero time was written out: %s", encoded)
	}
	var back session.Record
	if err := json.Unmarshal(encoded, &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !back.CreatedAt.Equal(when) || !back.UpdatedAt.IsZero() {
		t.Errorf("times did not round trip: %v / %v", back.CreatedAt, back.UpdatedAt)
	}
	_ = time.Time{}
}

// TestAMisspelledWakeOnIsRefusedRatherThanIgnored is D-70, and the list of
// spellings is not invented: every one of them was accepted before this, and
// every one produced a subscriber that connected, painted once and then never
// woke again.
func TestAMisspelledWakeOnIsRefusedRatherThanIgnored(t *testing.T) {
	for _, one := range []struct{ named, mention string }{
		{"Kernel", `did you mean "kernel"`}, // the Go field name
		{"KEY", `did you mean "key"`},
		{"kernal", "kernal"},
		{"state", "state"},
		{"status", "status"},
		{"what_it_is_doing", "what_it_is_doing"},
	} {
		err := session.ReasonTheseFieldsCannotBeWokenOn([]string{one.named})
		if err == nil {
			t.Errorf("wake-on %q was accepted, and would have woken nobody", one.named)
			continue
		}
		if !strings.Contains(err.Error(), one.mention) {
			t.Errorf("wake-on %q said %q, want it to mention %q", one.named, err, one.mention)
		}
	}
}

// TestOneBadNameAmongGoodOnesIsStillRefused. The mechanism is per-field, so a
// list that is right four times out of five is a display missing a fifth of
// what it asked for, in silence.
func TestOneBadNameAmongGoodOnesIsStillRefused(t *testing.T) {
	err := session.ReasonTheseFieldsCannotBeWokenOn(
		[]string{"kernel", "detail", "rank", "captured_context", "cwdd"})
	if err == nil {
		t.Fatal("a list with one bad name in it was accepted")
	}
	if !strings.Contains(err.Error(), "cwdd") {
		t.Errorf("err = %q, want it to name the one that is wrong", err)
	}
	for _, fine := range []string{`"kernel"`, `"detail"`, `"rank"`} {
		if strings.Contains(err.Error(), fine+" (did") {
			t.Errorf("err = %q, want it to leave the good ones alone", err)
		}
	}
}

// TestEveryFieldARecordHasCanBeWokenOn, both ways round: the list this checks
// against is the record's own, so a field renamed in the struct cannot go on
// being accepted here.
func TestEveryFieldARecordHasCanBeWokenOn(t *testing.T) {
	record := reflect.TypeOf(session.Record{})
	for i := range record.NumField() {
		name, _, _ := strings.Cut(record.Field(i).Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		if err := session.ReasonTheseFieldsCannotBeWokenOn([]string{name}); err != nil {
			t.Errorf("a record has %q and wake-on refuses it: %v", name, err)
		}
	}
}

// TestAskingForNothingIsNotAMistake: an empty WakeOn means "anything but the
// stamps", which is the documented default and the commonest answer.
func TestAskingForNothingIsNotAMistake(t *testing.T) {
	if err := session.ReasonTheseFieldsCannotBeWokenOn(nil); err != nil {
		t.Errorf("an empty wake-on was refused: %v", err)
	}
}

// TestTheSpellingsInUseTodayAreReal is the check that would have caught this
// before it was a class of bug rather than after: every WakeOn any subscriber
// in this repository actually declares.
func TestTheSpellingsInUseTodayAreReal(t *testing.T) {
	for _, inUse := range [][]string{
		{"kernel", "detail", "rank", "name", "cwd", "captured_context"}, // zellij-display
		{"kernel", "rank", "detail", "name", "message", "state_since"},  // the bars
		{"kernel"},
	} {
		if err := session.ReasonTheseFieldsCannotBeWokenOn(inUse); err != nil {
			t.Errorf("%v: %v", inUse, err)
		}
	}
}
