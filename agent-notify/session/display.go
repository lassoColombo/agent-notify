package session

import (
	"cmp"
	"encoding/json"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The rules in this file are the ones every display would otherwise write for
// itself, and write slightly differently (R24). They live here so that a bar, a
// picker and a tab title agree about what a session is called and which one
// matters most, without any of them having to decide.

// enoughOfTheIDToTellThemApart is how much of the session id is stuck on the
// end of a guessed name. Two characters, because this goes in a zellij tab
// beside a state and a spinner, and because it is what an agent that does the
// same thing chose: Claude's own unnamed sessions read `agent-notify-8e`.
const enoughOfTheIDToTellThemApart = 2

// DisplayName is what to call a session.
//
// A name comes from the agent, reported by its agent-integration on whatever
// hook it was firing anyway (§A7.4.2, D-60) — and the record stays honest when
// there is none: an empty Name means unnamed, never a guess. The guess is here
// instead, so that it is the same guess everywhere (R24), and so that a display
// can tell the two apart and dim, annotate or offer to rename the ones nobody
// has named.
//
// The fallback is the last component of the working directory, because that is
// almost always the repository you are in, which is almost always how you think
// of the session — with two characters of the session id after it, because
// otherwise the three sessions you have open in one repository are all called
// the same thing (D-75). It is per-record and it never moves: `DisplayName` is
// an address as well as a label — `agent-notify focus <name>` matches on it —
// so a name that depended on which other sessions happened to be on screen
// would break focusing and flicker in a per-pane renderer.
//
// Failing even a directory, the session id, truncated: unreadable is better
// than absent, because a row with no label cannot be clicked on with intent.
func (r Record) DisplayName() string {
	if name := strings.TrimSpace(r.Name); name != "" {
		return name
	}
	if r.Cwd != "" {
		if base := filepath.Base(r.Cwd); base != "." && base != string(filepath.Separator) {
			return base + r.idSuffix()
		}
	}
	if id := r.Key.SessionID; id != "" {
		if len(id) > 8 {
			return id[:8]
		}
		return id
	}
	return r.Key.Agent
}

// Named says whether anybody actually named this session, which is what an
// empty Name has meant since the agent-integrations stopped reporting their
// agents' placeholders (D-75). A display that wants to say "unnamed" out loud,
// or sort the named ones first, asks this rather than comparing DisplayName
// against a guess it would have to reconstruct.
func (r Record) Named() bool { return strings.TrimSpace(r.Name) != "" }

// idSuffix is `-8e`, or nothing at all when there is no id to take it from.
func (r Record) idSuffix() string {
	id := strings.TrimSpace(r.Key.SessionID)
	if len(id) < enoughOfTheIDToTellThemApart {
		return ""
	}
	return "-" + strings.ToLower(id[:enoughOfTheIDToTellThemApart])
}

// State is the (kernel, detail) pair as one string, which is the form the
// user's glyph table is keyed on (§A5.3) and the form a person reads.
func (r Record) State() string {
	if r.Detail == "" {
		return string(r.Kernel)
	}
	return string(r.Kernel) + "/" + r.Detail
}

// ByUrgency orders records the way every display wants them: what your delay
// costs most, first.
//
// Ties break on how long the session has been in that state, oldest first,
// because between two sessions blocked on you the one that has been waiting
// longer is the one to answer. The name breaks the remaining ties so that the
// order is stable across reads — a list that reshuffles between two identical
// polls is a list nobody can click on.
func ByUrgency(records []Record) {
	slices.SortStableFunc(records, moreUrgent)
}

// moreUrgent is the one comparison. Ordering a list and picking the single most
// urgent out of it are the same question asked twice, and two spellings of it
// would eventually disagree about which of two equals comes first.
func moreUrgent(a, b Record) int {
	if order := cmp.Compare(b.Rank, a.Rank); order != 0 {
		return order
	}
	if order := a.StateSince.Compare(b.StateSince); order != 0 {
		return order
	}
	return cmp.Compare(a.DisplayName(), b.DisplayName())
}

// MostUrgent is the one record a single light is for: a zellij tab that holds
// four panes, a menu-bar LED, a dock badge (§A12.1).
//
// It returns the record rather than its rank because what a display paints is
// resolved from the whole record — the detail can carry its own glyph (§A5.3),
// and a rank cannot be asked for one.
func MostUrgent(records []Record) (Record, bool) {
	if len(records) == 0 {
		return Record{}, false
	}
	highest := records[0]
	for _, record := range records[1:] {
		if moreUrgent(record, highest) < 0 {
			highest = record
		}
	}
	return highest, true
}

// WantsYou reports whether arriving in this state is worth interrupting a
// person for.
//
// It is the single most consequential predicate in this system, and it is here
// because it had been written out three times instead, in three repositories
// that never compare notes — the menu bar, the notification banner and the bar
// chips each deciding for themselves what is allowed to take somebody's
// attention (D-64). Three copies of a rule with that consequence is three
// chances to disagree, and the disagreement is silent: a state added later
// interrupts you on your menu bar and says nothing on your phone, and nothing
// anywhere reports a problem.
//
// Anything more urgent than working, which is to say: everything an agent
// reports that is not "still going". `working` is deliberately not one — an
// agent getting on with it is the state a bar is in most of the day, and one
// that lit up for it would be lit up all day and mean nothing. Idle is less
// urgent still.
//
// It asks the RECORD's rank rather than the kernel's, and that is the whole
// reason a rank rides in every record (§A5.5): a state this build has never
// heard of arrives with the number the build that wrote it assigned, so a
// seventh state that deserves your attention gets it without a line changing
// here or in any display.
func (r Record) WantsYou() bool { return r.Rank > RankWorking }

// stampFields move on every write and mean nothing to any renderer, so a change
// in them alone is not worth waking anybody for — or offering around at all.
var stampFields = map[string]bool{"sequence": true, "updated_at": true}

// optInFields mean something, but they move as often as the agent thinks, so a
// display is woken for them only if it named them. `usage` is the whole of
// it: tokens move on every response, and broadcasting them would turn a
// fifty-tool-call turn into fifty renders on every display (§A7.4.3, R23).
//
// They are kept apart from the stamps rather than folded in with them because
// the two answer different questions. A stamp is worth nothing to anybody; an
// opt-in field is worth everything to whoever asked. Folding them together
// makes one gate answer both questions, and the answer it gives is the wrong
// one: a display's own `wake_on` disappears behind a default it never chose.
var optInFields = map[string]bool{"usage": true}

// Differs reports whether a display that cares about `fields` should be told
// about this change.
//
// With no fields named it means "anything but the stamps", which is what stops
// a fifty-tool-call turn becoming fifty renders when nothing a display can see
// actually moved (§A9.3). With fields named it compares only those: a pane
// renamer does not care when `message` changes, and "wake me only when `kernel`
// changes" is the common case (R23).
//
// It compares the encoded form rather than the struct, so that a field added to
// Record is compared automatically and this cannot quietly fall behind the
// thing it is comparing.
func Differs(previous, next Record, fields []string) bool {
	if len(fields) == 0 {
		return moved(previous, next, stampFields, optInFields)
	}

	was, err := fieldsOf(previous)
	if err != nil {
		return true
	}
	now, err := fieldsOf(next)
	if err != nil {
		return true
	}
	for _, name := range fields {
		if string(was[name]) != string(now[name]) {
			return true
		}
	}
	return false
}

// moved compares two records while ignoring whole classes of field.
func moved(previous, next Record, ignoring ...map[string]bool) bool {
	was, err := fieldsOf(previous)
	if err != nil {
		return true
	}
	now, err := fieldsOf(next)
	if err != nil {
		return true
	}
	for _, class := range ignoring {
		for name := range class {
			delete(was, name)
			delete(now, name)
		}
	}

	if len(was) != len(now) {
		return true
	}
	for name, value := range now {
		if string(was[name]) != string(value) {
			return true
		}
	}
	return false
}

func fieldsOf(record Record) (map[string]json.RawMessage, error) {
	encoded, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		return nil, err
	}
	return fields, nil
}

// Elapsed is how long this session has been in the state it is in.
//
// It is computed and never stored, which is R26: a fact every display can
// derive identically from the record it already holds is rendering, not state.
// Storing it would mean a write per second per session to keep it true, and
// every one of those writes would wake every display to say nothing.
func (r Record) Elapsed(now time.Time) time.Duration {
	if r.StateSince.IsZero() {
		return 0
	}
	if elapsed := now.Sub(r.StateSince); elapsed > 0 {
		return elapsed
	}
	return 0
}

// Total is everything this session has been charged for.
//
// It is computed rather than stored, which is R26: four numbers every
// display would add up the same way are not a fifth number the record has to
// carry and keep consistent. Reasoning is not in it — it is the share of Output
// that was thinking, and adding it would count those tokens twice.
func (u Usage) Total() uint64 {
	return u.Input + u.Output + u.CacheRead + u.CacheWrite
}

// Ago is how a person reads a duration on a bar, where there is room for three
// characters and no room for "1h23m45.6s".
//
// It is here so that every display rounds the same way (R24). The rounding is
// downward on purpose: a session that has been waiting 119 seconds has been
// waiting "1m", not "2m", because the number is a floor a person can trust
// rather than a value that reads as longer than the truth.
func Ago(elapsed time.Duration) string {
	switch {
	case elapsed < time.Second:
		return "now"
	case elapsed < time.Minute:
		return strconv.Itoa(int(elapsed.Seconds())) + "s"
	case elapsed < time.Hour:
		return strconv.Itoa(int(elapsed.Minutes())) + "m"
	case elapsed < 24*time.Hour:
		return strconv.Itoa(int(elapsed.Hours())) + "h"
	}
	return strconv.Itoa(int(elapsed.Hours()/24)) + "d"
}
