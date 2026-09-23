package main

import (
	"strings"
	"testing"
	"time"
)

// timeout is long enough that a machine under load does not fail this, and
// short enough that a broken callback does not hang the suite.
func timeout() <-chan time.Time { return time.After(2 * time.Second) }

// These run against the real macOS on this machine. There is no fake worth
// writing: everything this file believes is a fact about UNUserNotificationCenter
// and a stub would only agree with whatever this program already assumed.
//
// The test binary is NOT in a bundle, which is the interesting half: every one
// of these is about what happens when macOS is asked something it will not
// answer, and the failure mode being guarded against is not a wrong answer but
// a dead process.

// TestAskingWithoutABundleAnswersNoRatherThanDying is the measurement this whole
// repository is shaped around. `UNUserNotificationCenter` without a bundle
// identifier does not return an error, it raises inside a `dispatch_once` and
// terminates the process — so the guard has to come before the question, and a
// test that merely called Start and read the result would pass by not crashing.
// That is exactly what this is.
func TestAskingWithoutABundleAnswersNoRatherThanDying(t *testing.T) {
	if RunningIn() != "" {
		t.Skipf("this test binary is somehow bundled as %q", RunningIn())
	}
	if Start() {
		t.Errorf("a bare binary said it could ask for permission")
	}
}

// TestStatusWithoutABundleIsTheThirdAnswer (R27). "" is not "denied": it is
// "nobody could be asked", and a program that collapsed the two would tell
// somebody their notifications are off when what is wrong is the bundle.
func TestStatusWithoutABundleIsTheThirdAnswer(t *testing.T) {
	if RunningIn() != "" {
		t.Skipf("this test binary is somehow bundled as %q", RunningIn())
	}
	if got := Status(); got != "" {
		t.Errorf("Status() = %q for a bare binary, want the unknown answer", got)
	}
	if got := HowBannersWillArrive(Sound{Plays: true}); !strings.Contains(got, "would not say") {
		t.Errorf("Notified() = %q", got)
	}
}

// TestPostingWithoutABundleIsAlsoSurvivable. Nothing is delivered and nothing
// dies: the session-watcher restarting this program in a loop would be a worse
// failure than silence.
func TestPostingWithoutABundleIsAlsoSurvivable(t *testing.T) {
	Post(Notice{Key: "test", Title: "agent-notify", Body: "this should go nowhere"}, "",
		Sound{Plays: true})
}

// TestThereIsASessionToNotifyInto, which is asked before anything AppKit is
// touched because AppKit's answer to being started without one is to abort.
func TestThereIsASessionToNotifyInto(t *testing.T) {
	if !Available() {
		t.Skip("no logged-in session here, which is a legitimate place to run tests")
	}
}

// TestTappingABannerHandsTheSessionBack is the one direction across the
// boundary that is not a posting: the key a notification was posted with is the
// key `focus-session` is given.
//
// It enters one line below the C rather than at it, because cgo is not allowed
// in the test file of a package that exports anything — so `aBannerWasTapped`
// exists as
// the seam, and this is what it is for.
func TestTappingABannerHandsTheSessionBack(t *testing.T) {
	handed := make(chan string, 1)
	whatToDoWhenABannerIsTapped = func(key string) { handed <- key }
	t.Cleanup(func() { whatToDoWhenABannerIsTapped = nil })

	aBannerWasTapped("mac/claude/alpha")
	select {
	case got := <-handed:
		if got != "mac/claude/alpha" {
			t.Errorf("focused %q", got)
		}
	case <-timeout():
		t.Fatal("tapping a banner focused nothing")
	}
}

// TestTappingWithNobodyListeningIsNotACrash. whatToDoWhenABannerIsTapped is
// nil until the program is wired up, and macOS can deliver a tap for a notification posted by a
// previous run at any moment.
func TestTappingWithNobodyListeningIsNotACrash(t *testing.T) {
	whatToDoWhenABannerIsTapped = nil
	aBannerWasTapped("mac/claude/alpha")
}

// TestTheMainQueueIsDrained is the regression test for the worst bug this
// repository has had, and the one the tests could not see.
//
// The program never called `[NSApplication sharedApplication]`. `NSApp` was
// therefore nil, `[NSApp run]` was a message to nil and returned at once, and
// the main queue was never drained. Everything kept working: permission was
// granted, banners arrived, `check` was green — because posting is XPC and does
// not need a run loop. Only the way back was dead, so clicking a banner was
// answered by macOS with "the application is not responding properly" while it
// launched a second copy of the app to try to find somebody at home.
//
// Every callback there is arrives on the main queue. This asks whether anything
// put on it is ever picked up, which is the one question that fails when that
// happens.
func TestTheMainQueueIsDrained(t *testing.T) {
	if !Available() {
		t.Skip("no run loop here")
	}
	if !Answers(3 * time.Second) {
		t.Fatal("nothing on the main queue is being looked at: this process can post " +
			"notifications and cannot answer a tap on one")
	}
}
