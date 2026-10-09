// Package session is the vocabulary: what a session is, what it can be doing,
// and how that is written down.
//
// It is the floor of this module. Every other package speaks it and it speaks
// nothing — no package here imports anything else of ours, which is what lets
// it be imported by all of them. A display, an adapter, a container and the
// command line all name the same six states and the same record, so a tab
// title and a menu bar cannot disagree about what is happening.
//
// It is also most of the public surface. The rest of the module is behind an
// internal/, where it can change without breaking a repository we do not
// control. That boundary is deliberate — see plan.md §A10.4 — and it is what
// makes this package's shape worth thinking about at all.
//
// Three kinds of thing live here, and the floor is why all three do (D-96):
// the vocabulary itself — [Record], [Kernel], [Event], [Reduce], [Key], the
// bounds; the rules every display would compute identically and so computes
// once (R24, D-30) — [DisplayName], [ByUrgency], [Palette], [Differs], [Ago];
// and the words core and a tool-integration exchange — [Capabilities], [View],
// [Change] and [LastShown]. The last could read as the SDK's, but the
// session-watcher and the commands speak them too, and the SDK imports core's
// internals, so putting them under subscribe would have an internal package
// import a door. The one place both sides can reach is here.
//
// Nothing here is promised to survive [D-77]. Every integration in this system
// is built from this source and released with it, so a symbol that stops being
// useful is deleted rather than kept for a reader that does not exist. That is
// a decision about today and not a property of the design: §A10.5 held the
// opposite rule until agent-notify was published, and publishing it is what
// would bring the rule back.
//
// Two things here are not about a session and stay anyway, because a package
// for two symbols is worse than two symbols in the wrong package: [Version] is
// what this build of agent-notify calls itself, and [Duration] is how long,
// spelled the way the configuration file spells it (D-80).
package session

// Version is what this build of agent-notify calls itself. Every integration
// announces it at the handshake and the session-watcher logs it; nothing
// arbitrates on it (D-77).
//
// It is a constant only until M18 teaches the build to stamp it.
const Version = "0.0.0-dev"
