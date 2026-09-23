package main

import (
	"os"
	"testing"
)

// TestMain owns the main thread, which is the one thing AppKit will not do
// without — and, in this repository, the one thing the program itself got wrong
// for a day: it posted notifications perfectly and answered nothing, because
// `NSApp` was nil and the loop it thought it was running was a message to nil.
//
// So the tests run beside a real run loop, in the same shape `main` has, and
// that is what makes TestTheMainQueueIsDrained a test of the production
// arrangement rather than of a mock of it.
func TestMain(m *testing.M) {
	if !Available() {
		// No logged-in session: the pure half of this package is still worth
		// running, and the rest skips itself.
		os.Exit(m.Run())
	}

	// Answers false without a bundle, which is correct and not the point: what
	// matters is that it has created the application and set the policy.
	Start()
	finished := make(chan int, 1)
	go func() {
		finished <- m.Run()
		Stop()
	}()
	Run()
	os.Exit(<-finished)
}
