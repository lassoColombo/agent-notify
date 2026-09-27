package main

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lassoColombo/agent-notify/session"
)

// These run against the real menu bar on this machine. There is no fake worth
// writing: everything this file believes is a fact about AppKit, and a stub
// would only agree with whatever this program already assumed.
//
// The item goes up under a title nobody would mistake for the real display's,
// and comes down again in TestMain.

// TestMain owns the main thread, which is the one thing AppKit will not do
// without.
//
// `go test` runs each test on its own goroutine, and NSApplication refuses to
// be driven from any thread but the first. So the run loop stays here and the
// tests run beside it — which is the same shape main.go has, for the same
// reason, and is worth knowing before reading either.
func TestMain(m *testing.M) {
	if !Available() {
		// No window server: the pure half of this package is still worth
		// running, and the rest skips itself below.
		os.Exit(m.Run())
	}

	Start("")
	finished := make(chan int, 1)
	go func() {
		finished <- m.Run()
		Stop()
	}()
	Run()
	os.Exit(<-finished)
}

// onTheBar waits for what was drawn to arrive, because Draw hands the work
// to the main queue and returns. A test that looked immediately would be
// reading the bar as it was before it asked.
func onTheBar(t *testing.T, want string) []string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var last []string
	for {
		titles, err := OnTheBar()
		if err != nil {
			t.Skipf("%v", err)
		}
		last = titles
		for _, title := range titles {
			if title == want {
				return titles
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the menu bar says %v, want an item saying %q", last, want)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func applied(t *testing.T, sessions []session.Record) {
	t.Helper()
	if !Available() {
		t.Skip("there is no menu bar here")
	}
	whatToShow, _ := Render(bar(), sessions)
	if err := Draw(whatToShow); err != nil {
		t.Fatalf("Apply: %v", err)
	}
}

// TestTheItemIsReallyOnTheRealMenuBar. The whole of M15a's premise, checked the
// only way it can be: an NSStatusItem's window is hosted out of process and
// never appears in CGWindowListCopyWindowInfo, so what the WindowServer knows
// about this process is nothing at all [verified 2026-09-18, macOS 26.6.2]. The
// accessibility API is the one thing that can see it.
func TestTheItemIsReallyOnTheRealMenuBar(t *testing.T) {
	applied(t, []session.Record{
		aSession("one", session.Working, nine),
		aSession("two", session.Working, nine),
		aSession("three", session.BlockedOnYou, nine),
	})
	// The item is a picture and has no title at all, so what the accessibility
	// API has to go on is the label the paint gave it — which is also what a
	// person hovering it is told, and what VoiceOver reads out.
	onTheBar(t, "agent-notify — 1 blocked on you, 2 working")
}

// TestTheItemChangesWhenTheWorldDoes.
func TestTheItemChangesWhenTheWorldDoes(t *testing.T) {
	applied(t, []session.Record{aSession("one", session.Working, nine)})
	onTheBar(t, "agent-notify — 1 working")

	applied(t, nil)
	onTheBar(t, "agent-notify — nothing running")
}

// TestAnAnnouncementReachesTheBar: the name of the session that just changed,
// after the counts it did not disturb.
func TestAnAnnouncementReachesTheBar(t *testing.T) {
	if !Available() {
		t.Skip("there is no menu bar here")
	}
	board := bar()
	board.Now = time.Now()
	whatToShow, announcement := Render(board, []session.Record{
		aSession("alpha", session.BlockedOnYou, board.Now.Add(-time.Second)),
	})
	if !announcement.Announced() {
		t.Fatal("nothing was announced")
	}
	if err := Draw(whatToShow); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	onTheBar(t, gap+"alpha")
}

// TestChoosingARowHandsTheSessionBack is the other direction across the
// boundary: the key a row was painted with is the key `focus-session` is given.
//
// It enters one line below AppKit rather than at it, and for two reasons.
// Pressing a real menu item means opening a real menu, which takes the keyboard
// of whoever is running the tests; and cgo is not allowed in the test file of a
// package that exports anything, so the C string cannot be built here. What is
// left untested is `C.GoString`.
func TestChoosingARowHandsTheSessionBack(t *testing.T) {
	heard := make(chan string, 1)
	was := whatToDoWhenAMenuRowIsChosen
	whatToDoWhenAMenuRowIsChosen = func(key string) { heard <- key }
	t.Cleanup(func() { whatToDoWhenAMenuRowIsChosen = was })

	whatToShow, _ := Render(bar(), []session.Record{aSession("alpha", session.Working, nine)})
	row := find(t, whatToShow, "alpha")

	aMenuRowWasChosen(row.Key)
	select {
	case got := <-heard:
		if got != row.Key {
			t.Errorf("the menu handed back %q, want %q", got, row.Key)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("choosing a row reached nothing")
	}
}

// TestEveryColourThisShipsWithCanActuallyBeDrawn. The defaults are AppKit's own
// names, which means they are strings that have to match something, and a name
// that matches nothing would be a state drawn in the ordinary menu colour with
// nobody the wiser.
func TestEveryColourThisShipsWithCanActuallyBeDrawn(t *testing.T) {
	for kernel, name := range DefaultColours {
		if !ColourKnown(name) {
			t.Errorf("%s is drawn in %q, which AppKit does not know", kernel, name)
		}
	}
}

// TestAColourCanAlsoBeWrittenTheWayEveryOtherDisplayWritesOne.
func TestAColourCanAlsoBeWrittenTheWayEveryOtherDisplayWritesOne(t *testing.T) {
	for _, spelling := range []string{"0xffeb6f92", "0XFFEB6F92"} {
		if !ColourKnown(spelling) {
			t.Errorf("%q is not accepted, and it is what the sketchybar display takes", spelling)
		}
	}
	for _, nonsense := range []string{"", "red", "0xff", "#eb6f92"} {
		if ColourKnown(nonsense) {
			t.Errorf("%q was accepted as a colour", nonsense)
		}
	}
}

// TestTheAccessibilityAnswerIsThreeValued. Not being able to ask is not the
// same as the item being absent, and a test that read it as one would pass on a
// machine where this display does not work at all (R27).
func TestTheAccessibilityAnswerIsThreeValued(t *testing.T) {
	titles, err := OnTheBar()
	switch {
	case err != nil:
		if !strings.Contains(err.Error(), "accessibility") {
			t.Errorf("the error does not say what is missing: %v", err)
		}
	case !Available():
		t.Skip("there is no menu bar here")
	case len(titles) == 0:
		t.Errorf("accessibility answered, and says this process has no menu bar item")
	}
}
