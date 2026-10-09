package session

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"
)

// What a display may ask to be woken for, and how it can find out what it
// actually needs rather than declare it and hope. A name that is not a field
// wakes a display never (D-70); a field the renderer reads and the declaration
// omits wakes it never for that change (D-72). Neither produces an error, so
// both are checked here.

// settledFields do not move once a session exists, so nothing can be woken by
// one of them.
//
// `key` is the session's identity — a different key is a different session,
// not a changed one. `created_at` is what it says.
//
// They are real fields and naming one is not refused: a list that also names
// something live is perfectly reasonable, and refusing would need a guess
// about intent. They are excluded from [eachFieldMoved] because a sweep that
// included them would ask every display to wake on `key`.
var settledFields = map[string]bool{"key": true, "created_at": true}

// ReasonTheseFieldsCannotBeWokenOn names any of them that is not a field of a
// record, and is nil when they are all real.
//
// [Differs] matches by string, so a name that matches nothing matches nothing
// forever: the display paints once, correctly, and is never woken again, with
// nothing in any log. Every likely mistake is fatal that way — `Kernel`,
// `kernal`, `state`, `status` — which is why this is an error and not a note.
// It does not judge a name that is real and inert — see [settledFields].
func ReasonTheseFieldsCannotBeWokenOn(fields []string) error {
	var unknown []string
	for _, name := range fields {
		if !slices.Contains(recordFields, name) {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) == 0 {
		return nil
	}

	said := make([]string, 0, len(unknown))
	for _, name := range unknown {
		// The commonest mistake by far is the Go field name, which differs
		// from the real one only in case. Saying so is worth more to the
		// person reading this than the whole list underneath it.
		meant := ""
		for _, real := range recordFields {
			if strings.EqualFold(name, real) {
				meant = real
				break
			}
		}
		if meant == "" {
			said = append(said, fmt.Sprintf("%q", name))
			continue
		}
		said = append(said, fmt.Sprintf("%q (did you mean %q?)", name, meant))
	}

	plural := "fields"
	if len(unknown) == 1 {
		plural = "a field"
	}
	return fmt.Errorf("wake-on names %s a record does not have: %s\na record's fields are: %s",
		plural, strings.Join(said, ", "), strings.Join(recordFields, ", "))
}

// FieldsWorthWakingFor is every field a display may name, without the ones
// that never move.
func FieldsWorthWakingFor() []string {
	var worth []string
	for _, name := range recordFields {
		if !settledFields[name] && !stampFields[name] {
			worth = append(worth, name)
		}
	}
	return worth
}

// FieldsRenderedButNotWokenFor is every field the render reads and wakeOn
// does not name: the test every display runs (D-73). It moves one field of
// base at a time and renders again; a render that came out different reads
// that field. Empty means the declaration is complete.
func FieldsRenderedButNotWokenFor(base Record, wakeOn []string, render func(Record) string) []string {
	var missing []string
	was := render(base)
	for field, moved := range eachFieldMoved(base) {
		if render(moved) != was && !slices.Contains(wakeOn, field) {
			missing = append(missing, field)
		}
	}
	slices.Sort(missing)
	return missing
}

// eachFieldMoved is one copy of base per field, each differing from base in
// exactly that one field. It is the sweep [FieldsRenderedButNotWokenFor] is
// built on, and that function is the whole of what a display sees of it: a
// display DISCOVERS what its renderer reads rather than declaring it from
// memory, because every renderer in this system is a pure function of
// records and "does my output depend on this field" is answerable by
// rendering twice. That test is the one that would have caught D-72, and it
// catches the next one without anybody having to think of it.
//
// It is core's rather than each display's for the usual reason. Producing a
// value that is genuinely different for every field of a record — a time, a
// kernel that is still a real kernel, a map, a raw JSON message — is fiddly
// enough that four repositories would get it four subtly different kinds of
// wrong, and a mutation that failed to move anything would read as "my
// renderer does not use this field" and pass. Its contract has tests of its
// own, which is why export_test.go lends it a public name.
//
// [settledFields] are left out: they cannot move, so nothing can be woken by
// them.
func eachFieldMoved(base Record) map[string]Record {
	moved := make(map[string]Record, len(recordFields))
	shape := reflect.TypeOf(Record{})
	for i := range shape.NumField() {
		name, _, _ := strings.Cut(shape.Field(i).Tag.Get("json"), ",")
		if name == "" || name == "-" || settledFields[name] {
			continue
		}
		one := base.Clone()
		if nudge(reflect.ValueOf(&one).Elem().Field(i), name) {
			moved[name] = one
		}
	}
	return moved
}

// nudge changes one field into something a record could really hold, and says
// whether it managed to.
//
// "Could really hold" is the constraint that makes this worth writing rather
// than setting everything to a random string. A kernel has to stay a kernel a
// palette can resolve: turning `working` into `working-moved` produces an
// unknown state, which a display renders from its rank — possibly to the very
// same glyph, which would read as a renderer that does not look at the kernel.
// The mutation has to be plausible or the test it feeds is weaker than it
// appears.
func nudge(field reflect.Value, name string) bool {
	switch name {
	case "kernel":
		// A different REAL state, so that what comes out the other side is
		// something a display has an opinion about.
		next := Working
		if field.String() == string(Working) {
			next = BlockedOnYou
		}
		field.SetString(string(next))
		return true
	case "rank":
		// Sparse ranks, so +1 is a rank no state holds and a display keyed on
		// the number still sees a move (§A5.5).
		field.SetInt(field.Int() + 1)
		return true
	}

	switch value := field.Interface().(type) {
	case time.Time:
		field.Set(reflect.ValueOf(value.Add(time.Hour).UTC()))
		return true
	case json.RawMessage:
		field.Set(reflect.ValueOf(json.RawMessage(`{"moved":true}`)))
		return true
	}

	switch field.Kind() {
	case reflect.String:
		field.SetString(field.String() + "-moved")
		return true
	case reflect.Bool:
		field.SetBool(!field.Bool())
		return true
	case reflect.Int, reflect.Int64:
		field.SetInt(field.Int() + 1)
		return true
	case reflect.Uint64:
		field.SetUint(field.Uint() + 1)
		return true
	case reflect.Map:
		if field.IsNil() {
			field.Set(reflect.MakeMap(field.Type()))
		}
		// Entries that are ALREADY there are changed, rather than a stranger
		// being added beside them. A renderer usually reads one key by name —
		// zellij-display reads only its own entry of `captured_context.by`,
		// which is where the pane it paints is recorded — and a new key
		// appearing next to that one changes nothing it looks at. Adding
		// instead of changing made this sweep pass for a display that reads
		// the field, which is the failure a sweep must not have.
		for _, key := range field.MapKeys() {
			next := reflect.New(field.Type().Elem()).Elem()
			next.Set(field.MapIndex(key))
			if nudge(next, "") {
				field.SetMapIndex(key, next)
			}
		}
		if field.Len() == 0 {
			next := reflect.New(field.Type().Elem()).Elem()
			nudge(next, "")
			field.SetMapIndex(reflect.ValueOf("moved"), next)
		}
		return true
	case reflect.Slice:
		field.Set(reflect.Append(field, reflect.New(field.Type().Elem()).Elem()))
		return true
	case reflect.Struct:
		// EVERY field inside, not the first one that works, and for the same
		// reason the map changes what is already there: what is being asked is
		// whether the renderer reads this field AT ALL, and a renderer that
		// reads the third thing in a struct must not be told no because the
		// first thing moved instead.
		moved := false
		for i := range field.NumField() {
			if inner := field.Field(i); inner.CanSet() && nudge(inner, "") {
				moved = true
			}
		}
		return moved
	}
	return false
}
