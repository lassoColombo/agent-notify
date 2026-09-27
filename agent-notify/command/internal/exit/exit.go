// Package exit is how a command that has an exit code of its own ends.
//
// The three-valued answers in this system — `focused` says yes, no, or nobody
// can tell — are exit codes and not errors, and a RunE that returned one would
// have cobra print it as a failure. So a command with something to say beyond
// success says it here.
//
// It is a package of its own, rather than two lines in each of the nine
// command packages, for the reason R24 gives about displays and applies just as
// well here: a rule written nine times is a rule that will eventually disagree
// with itself about what zero means.
package exit

import "os"

// TheProcessWith ends the process, unless the code says nothing went wrong —
// in which case it returns and lets cobra finish normally.
func TheProcessWith(code int) {
	if code != 0 {
		os.Exit(code)
	}
}
