// Package agentnotify is the entire public surface of agent-notify.
//
// Everything an integration needs lives here and nothing else does: the rest of
// the module is under internal/, where it can change without breaking a
// repository we do not control. That boundary is deliberate — see plan.md
// §A10.4 — and it is what makes this package's shape worth thinking about at
// all.
//
// Nothing here is promised to survive [D-77]. Every integration in this system
// is built from this source and released with it, so a symbol that stops being
// useful is deleted rather than kept for a reader that does not exist. That is
// a decision about today and not a property of the design: §A10.5 held the
// opposite rule until agent-notify was published, and publishing it is what
// would bring the rule back.
//
// The package is named agentnotify while the module is agent-notify, because a
// Go package name cannot carry a hyphen.
package agentnotify

// Version is what this build of agent-notify calls itself. Every integration
// announces it at the handshake and the session-watcher logs it; nothing
// arbitrates on it (D-77).
//
// It is a constant only until M18 teaches the build to stamp it.
const Version = "0.0.0-dev"
