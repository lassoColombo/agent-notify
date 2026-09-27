package session

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

// The closed vocabularies are each written down twice: once as a table keyed on
// the name, and once as the enumeration a display asks for. These are the tests
// that stop the two halves drifting apart.
//
// They live in package session rather than beside the others, which are all
// package session_test and therefore cannot see the tables at all. That is
// why the drift went unnoticed: a seventh kernel added to `kernels` and left
// out of Kernels(), or a tenth event added to `events` and left out of
// Events(), passed the entire suite.

func TestEveryKernelIsEnumerated(t *testing.T) {
	enumerated := Kernels()
	if len(enumerated) != len(kernels) {
		t.Errorf("Kernels() lists %d of the %d states in the table", len(enumerated), len(kernels))
	}
	for kernel := range kernels {
		if !slices.Contains(enumerated, kernel) {
			t.Errorf("%q is in the table and Kernels() does not list it", kernel)
		}
	}
	for _, kernel := range enumerated {
		if !kernel.Known() {
			t.Errorf("Kernels() lists %q, which the table has never heard of", kernel)
		}
	}
}

func TestEveryEventIsEnumerated(t *testing.T) {
	enumerated := Events()
	if len(enumerated) != len(events) {
		t.Errorf("Events() lists %d of the %d events in the table", len(enumerated), len(events))
	}
	for event := range events {
		if !slices.Contains(enumerated, event) {
			t.Errorf("%q is in the table and Events() does not list it", event)
		}
	}
	for _, event := range enumerated {
		if !event.Known() {
			t.Errorf("Events() lists %q, which the table has never heard of", event)
		}
	}
}

// TestKernelsIsSortedByUrgency pins what Kernels() promises its caller, now
// that the order is derived rather than written out by hand.
func TestKernelsIsSortedByUrgency(t *testing.T) {
	enumerated := Kernels()
	if !slices.IsSortedFunc(enumerated, func(a, b Kernel) int { return b.Rank() - a.Rank() }) {
		t.Errorf("Kernels() = %v, which is not most urgent first", enumerated)
	}
}

// TestKernelsHandsBackACopy: a display that sorts what it is given must not be
// able to reorder the table for every later caller.
func TestKernelsHandsBackACopy(t *testing.T) {
	slices.Reverse(Kernels())
	if first := Kernels()[0]; first != BlockedOnYou {
		t.Errorf("after a caller reversed its slice, Kernels() starts at %q", first)
	}
}

// TestRecordFieldsIsExactlyTheStruct. recordFields is the vocabulary a
// subscriber's wake-on list is checked against (wakeon.go), so the two ways it
// can be wrong are both silent and both bad: a field missing from it is a field
// nobody is allowed to wake on, and a name in it that is not a field is a
// wake-on that is accepted and then never fires, which is D-70 exactly.
func TestRecordFieldsIsExactlyTheStruct(t *testing.T) {
	shape := reflect.TypeOf(Record{})
	var declared []string
	for i := range shape.NumField() {
		field := shape.Field(i)
		if !field.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			t.Errorf("Record.%s has no json name", field.Name)
			continue
		}
		declared = append(declared, name)
	}

	for _, name := range declared {
		if !slices.Contains(recordFields, name) {
			t.Errorf("%q is a field of Record and recordFields does not list it", name)
		}
	}
	for _, name := range recordFields {
		if !slices.Contains(declared, name) {
			t.Errorf("recordFields lists %q, which Record does not have", name)
		}
	}
}
