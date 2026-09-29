package session

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
)

// MaxKeyBytes bounds the encoded key, which becomes a filename.
const MaxKeyBytes = 200

// Key identifies a session (plan.md §A6).
//
// The session id alone is not unique: two agents can mint the same string, and
// they mint them under different schemes. Host is in the key from the first
// commit even though v1 is local-only, because adding it later would change
// every stored filename and every display's notion of identity.
type Key struct {
	Host      string `json:"host"`
	Agent     string `json:"agent"`
	SessionID string `json:"session_id"`
}

// String renders the key as one filesystem-safe, reversible token — the
// session's name on disk.
//
// Everything outside the unreserved set is percent-encoded, which includes the
// separator, so the encoding cannot be ambiguous however strange an agent's
// session ids turn out to be.
func (k Key) String() string {
	return encodeKeyPart(k.Host) + "~" + encodeKeyPart(k.Agent) + "~" + encodeKeyPart(k.SessionID)
}

// ParseKey reverses String.
func ParseKey(token string) (Key, error) {
	parts := strings.Split(token, "~")
	if len(parts) != 3 {
		return Key{}, fmt.Errorf("%q is not a session key: want host~agent~session-id", token)
	}
	var key Key
	for i, into := range []*string{&key.Host, &key.Agent, &key.SessionID} {
		decoded, err := decodeKeyPart(parts[i])
		if err != nil {
			return Key{}, fmt.Errorf("%q is not a session key: %w", token, err)
		}
		*into = decoded
	}
	return key, key.ReasonThisKeyCannotBeUsed()
}

// ReasonThisKeyCannotBeUsed is given before anything is written, rather than
// letting a bad key fail at the moment a file is created under a name nobody
// chose.
func (k Key) ReasonThisKeyCannotBeUsed() error {
	for _, part := range []struct{ name, value string }{
		{"host", k.Host}, {"agent", k.Agent}, {"session-id", k.SessionID},
	} {
		if part.value == "" {
			// A slice and not a map: ranging a map would make the complaint
			// about a key missing two parts a different complaint each run.
			return fmt.Errorf("a session key needs its %s", part.name)
		}
	}
	if size := len(k.String()); size > MaxKeyBytes {
		return fmt.Errorf("the encoded session key is %d bytes, over the %d a filename may take: %s",
			size, MaxKeyBytes, k.String())
	}
	return nil
}

func encodeKeyPart(part string) string {
	var out strings.Builder
	for i := 0; i < len(part); i++ {
		c := part[i]
		unreserved := c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' ||
			c == '-' || c == '_' || c == '.'
		if unreserved {
			out.WriteByte(c)
			continue
		}
		fmt.Fprintf(&out, "%%%02X", c)
	}
	return out.String()
}

func decodeKeyPart(part string) (string, error) {
	var out strings.Builder
	for i := 0; i < len(part); i++ {
		if part[i] != '%' {
			out.WriteByte(part[i])
			continue
		}
		if i+2 >= len(part) {
			return "", fmt.Errorf("truncated %% escape")
		}
		var value int
		if _, err := fmt.Sscanf(part[i+1:i+3], "%02X", &value); err != nil {
			return "", fmt.Errorf("%q is not a %% escape", part[i:i+3])
		}
		out.WriteByte(byte(value))
		i += 2
	}
	return out.String(), nil
}

// Process is what liveness is decided on (§A8). No display reads it.
//
// The triple is the identity, not the pid: pids are reused, and a pid that came
// back after a reboot is a different process wearing the same number.
type Process struct {
	PID       int       `json:"pid,omitempty"`
	StartedAt time.Time `json:"started_at,omitzero"`
	BootID    string    `json:"boot_id,omitempty"`
}

// Ancestor is one step of the process chain the hook could see.
//
// The start time is here for the same reason it is beside the agent's own pid:
// a pid on its own is a number that will be reused, and an integration mapping
// a terminal's pid to a window needs to know it is still the same terminal.
type Ancestor struct {
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at,omitzero"`
	Command   string    `json:"command"`
}

// CapturedContext is what the hook could read and nothing else could.
//
// The agent's environment exists only inside the agent's own process, so only a
// descendant of it can read it — which means the hook, and nothing else. Core
// captures the process ancestry itself; everything else is captured on behalf of
// one integration, by running its `capture-environment`, which is the only way
// anything gets in here (D-57).
//
// It is kept rather than discarded once interpreted, because interpretation
// happens later and somewhere else: an integration that was not running when
// the hook fired interprets this blob when it connects (R4).
type CapturedContext struct {
	// CapturedAt is when the hook took this snapshot. Nothing depends on it —
	// the writer that replaces a captured context clears the derived one in the
	// same write — but a stale placement is much easier to explain with it.
	CapturedAt time.Time `json:"captured_at,omitzero"`
	// Ancestry is core's own capture, used to recognise which agent this is
	// (§A8.4) and to resolve the ambient session (§A7.4.2).
	Ancestry []Ancestor `json:"ancestry,omitempty"`
	// By maps an integration's name to whatever its capture-environment
	// returned. Opaque to core, in both directions (R7).
	By map[string]json.RawMessage `json:"by,omitempty"`
}

// Usage is what a session has spent, in the one vocabulary every agent fills
// and every display can read without knowing which agent filled it (§A7.4.3,
// D-61).
//
// The four counters are disjoint and the sum of them is the total, which is the
// property a display needs to price them: a cache read costs a tenth of a fresh
// input token and a cache write costs more than one, so a single blended number
// is not a number anybody can use. Reasoning is the exception and is named for
// being one — it is the part of Output that was thinking, not a fifth addend.
//
// Nothing here is in currency. Pricing is a per-model rate table that changes
// underneath you, and §A14 refuses a fact about a program in the user's
// configuration; core counts tokens, and what they were worth is the display's
// business, with the record's own `model` sitting beside them.
type Usage struct {
	Input      uint64 `json:"input,omitempty"`
	Output     uint64 `json:"output,omitempty"`
	CacheRead  uint64 `json:"cache_read,omitempty"`
	CacheWrite uint64 `json:"cache_write,omitempty"`
	// Reasoning is how much of Output was thinking. Adding it to the rest
	// counts it twice.
	Reasoning uint64 `json:"reasoning,omitempty"`

	// CountedThrough is the agent's own id for the last response these totals
	// include. Core compares it for equality and never reads it (R7).
	CountedThrough string `json:"counted_through,omitempty"`
}

// Spend is what one model response cost, as an agent-integration read it out of
// its agent's own accounting.
//
// The id is not decoration. Claude writes one transcript line per content block
// and repeats the response's entire usage on every one of them — [verified
// 2026-09-20, 2.1.236] 1869 responses written as 3401 lines — so a total that
// does not deduplicate is a total twice the size of the truth. codex's
// token_usage_record carries response_id and has the same shape.
type Spend struct {
	// Response is the agent's own id for the thing it was charged for:
	// `requestId`, `response_id`, whatever that agent calls it.
	Response   string `json:"response"`
	Input      uint64 `json:"input,omitempty"`
	Output     uint64 `json:"output,omitempty"`
	CacheRead  uint64 `json:"cache_read,omitempty"`
	CacheWrite uint64 `json:"cache_write,omitempty"`
	Reasoning  uint64 `json:"reasoning,omitempty"`
}

// Adding returns these totals with every response after CountedThrough added to
// them, and the cursor moved to the last one named.
//
// `responses` is one read of the tail of the agent's file, oldest first, and it
// overlaps the last read almost entirely — which is the point. An adapter that
// exits in milliseconds cannot hold a cursor, and re-reading the whole
// transcript to rebuild the total costs more with every turn the session takes,
// so it re-reads a fixed window and this decides what in it is new.
//
// A window that no longer reaches back to the cursor adds everything it holds.
// That undercounts by whatever fell off the end, which is the failure worth
// having: counting again what has already been counted makes a total that grows
// on its own, and no later read can ever bring it back down.
func (u Usage) Adding(responses []Spend) Usage {
	// The cursor's own response is usually written across several lines, so
	// what is wanted is the last of them: stopping at the first would count its
	// twins a second time.
	first := 0
	for i, response := range responses {
		if response.Response != "" && response.Response == u.CountedThrough {
			first = i + 1
		}
	}
	for _, response := range responses[first:] {
		if response.Response == "" || response.Response == u.CountedThrough {
			// Either nobody can name it — which would be a promise to count it
			// again on the next read — or it is the line before this one saying
			// the same thing a second time.
			continue
		}
		u.Input += response.Input
		u.Output += response.Output
		u.CacheRead += response.CacheRead
		u.CacheWrite += response.CacheWrite
		u.Reasoning += response.Reasoning
		u.CountedThrough = response.Response
	}
	return u
}

// Record is one session, as stored and as handed to every display.
//
// Every field traces to a display that demanded it (§A7.4). Nothing is here
// because it seemed useful.
type Record struct {
	Key      Key    `json:"key"`
	Sequence uint64 `json:"sequence"`

	Kernel Kernel `json:"kernel"`
	// Rank is stored rather than derived so that a display meeting a kernel it
	// has never heard of can still sort it and paint it from a fallback,
	// instead of dropping it (§A5.5, R21).
	Rank   int    `json:"rank"`
	Detail string `json:"detail,omitempty"`

	// StateSince moves only when the kernel moves. Naming a session, annotating
	// it, or an agent reporting continued progress must not reset "waiting 12m".
	StateSince time.Time `json:"state_since,omitzero"`
	CreatedAt  time.Time `json:"created_at,omitzero"`
	UpdatedAt  time.Time `json:"updated_at,omitzero"`
	// EndedAt is when it went; retention counts from here, and resurrection
	// clears it.
	EndedAt time.Time `json:"ended_at,omitzero"`

	// Name is what the agent calls this session, reported by the
	// agent-integration on any event it likes (§A7.4.2, D-60). Core never
	// derives it and no event clears it. Empty means unnamed: the fall back to
	// the last component of Cwd belongs in the SDK, so that every display does
	// it identically (R24).
	Name string `json:"name,omitempty"`
	Cwd  string `json:"cwd,omitempty"`
	// Branch is what is checked out where this session is working, or empty
	// when that cannot be answered — a directory in no repository, or a
	// detached head, which is a commit and not a branch. Core reads it on the
	// hook path rather than taking the agent's word for it (§A7.4.4).
	Branch string `json:"branch,omitempty"`
	// Model is what the agent is using right now, in the agent's own spelling,
	// which core stores and never interprets (R7). It is a field of its own
	// because a display reads it across agents — "three on opus, one on gpt-5"
	// — and because tokens cannot be priced without it.
	Model string `json:"model,omitempty"`
	// Message is the last thing the agent said, whatever kind of thing that
	// was: the (kernel, detail) pair already says how to read it (D-17). It can
	// therefore be older than the state, which is why a display that cares
	// shows StateSince beside it.
	Message string `json:"message,omitempty"`

	// Usage is what this session has spent. The agent-integration reports the
	// responses it can see out of its agent's own accounting and this
	// accumulates them (§A7.4.3, D-61) — so a bar counts tokens across Claude
	// and codex together without either word reaching core.
	Usage Usage `json:"usage,omitzero"`

	Process         Process         `json:"process,omitzero"`
	CapturedContext CapturedContext `json:"captured_context,omitzero"`
	// DerivedContext is what each integration made of the captured context: a
	// pane and a tab, a workspace, a branch. It is void the moment the captured
	// context is replaced, and the two are replaced in the same write, so no
	// reader ever has to compare them (§A7.4.1, D-27).
	DerivedContext map[string]json.RawMessage `json:"derived_context,omitempty"`

	// Annotations is what somebody attached to this session that was derived
	// from nothing of ours — a task id, a ticket, a label. Nobody asked for it,
	// so nothing we do invalidates it: it lives until its owner overwrites it.
	// Core carries it without inspecting it (R7), and an owner's section is
	// replaced wholesale, never merged.
	Annotations map[string]json.RawMessage `json:"annotations,omitempty"`
}

// recordFields is every field a record has, and it is what a display's
// wake-on list is checked against (wakeon.go). A test checks it against the
// struct, so the two cannot drift.
var recordFields = []string{
	"key", "sequence",
	"kernel", "rank", "detail",
	"state_since", "created_at", "updated_at", "ended_at",
	"name", "cwd", "branch", "model", "message", "usage",
	"process", "captured_context", "derived_context",
	"annotations",
}

// Clone returns a copy that shares nothing mutable with the original, so that a
// reducer returning a new record cannot be caught changing the old one.
func (r Record) Clone() Record {
	clone := r
	clone.Annotations = maps.Clone(r.Annotations)
	clone.DerivedContext = maps.Clone(r.DerivedContext)
	clone.CapturedContext.By = maps.Clone(r.CapturedContext.By)
	clone.CapturedContext.Ancestry = slices.Clone(r.CapturedContext.Ancestry)
	return clone
}
