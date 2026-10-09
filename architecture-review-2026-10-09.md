# Architecture review, 2026-10-09

A read of all seven modules for separation of concerns, abstraction and
correctness, after M17. The bus (D-83) and the layering (D-80) hold; what is
left is three bugs, a handful of places that do one job twice, and one package
that has become the default home for anything exported. Nothing here reverses
a decision in `agent-notify/plan.md` §A19.

## Fixed in the same change

- **`agent-notify watcher run` failed on a fresh root.** `sessionwatcher.Start`
  took the lock before anything had created the runtime directory the lock
  lives in. It now calls `layout.Create()` first, as `watcher start` already
  did.
- **`integrations.json` was rewritten every 5s.** The answers it carries are
  asked at startup and on reload and nowhere else, so it is written at those
  two moments and the `written` stamp means something again. `doctor` says so.
- **Two spellings of the detail namespace.** claude's `kebab` lowercased and
  swapped underscores; codex's also split camelCase. §A5.8 says details are one
  namespace across agents, so the rule is `hook.Detail`, once, and both
  adapters call it.
- **One failing capture voided every container's coordinates** (D-90). The
  hook asks only the integrations with no entry, and `session.Apply` merges
  what they answer under their own names, voiding only what was derived from
  those. A new process still replaces the whole capture. One rule,
  `Record.CaptureIsStale`, decides which case both are in.
- **`interpret-environment` held every draw** (D-91). `reconcile` ran it
  synchronously, 5s per (session, container), so an exit or a finished turn
  waited behind zellij. It now runs on its own goroutine with a one-slot ask
  channel, writes through the store, and the write wakes the loop.
  `TestASlowContainerDoesNotDelayADraw` fails on the old loop.
- **Liveness was computed N+1 times per change** (D-92). `reconcile` probed
  every pid, then each renderer read the store and probed again through
  `WhatIsRunning`. `Core.WhatIsRunningAndWhatEnded` is now the one read and
  the one decision, `process.Ended`, for the watcher, `list` and the SDK
  alike; the watcher reads once per wake and hands the world to every
  renderer, which drops ended sessions unless it asked for them.
- **Core captured a process chain nobody read** (D-93). The chain now has
  its reader: `interpret-environment` is handed the record's
  `captured_context` narrowed to the container, its own entry and core's
  `ancestry`, and aerospace's walker, its `!darwin` stub, its walk test and
  its direct `x/sys` dependency are gone. `TestAContainerIsHandedItsOwnEntryAndTheChain`
  pins the shape, including that another integration's entry is not handed over.
- **The render decision was written twice** (D-94). `LastShown.Replace` now
  swaps the world in and answers with the view to hand over, or nothing;
  `HasSeenAView` and `ViewOf` are gone, and both callers are one line. A
  test in `session` pins the four outcomes where the rule lives.
- **Integrations opened the log twice** (D-95). `Integration.Logger()` on
  the SDK returns the logger core already opened for the process; zellij and
  the notifier use it, `logs.Open` is gone, and `logs` lives under
  `internal/`, so the layering test's doors are five.
- **Dead since D-87.** `JustArrived`, `Arrival`, `Announced`, `Same` and
  `moreRecent` in `session/display.go`, with `arrival_test.go`, had no caller
  once the menu bar went. `plural` in the notifier's `preview.go` likewise.
  Removed, and the README's list of display rules updated.

## Remaining

Ordered by what they cost, most first. Each names the code and the shape of
the fix; none is started.

### 1. `session` carries three vocabularies

- The package is documented as the floor that "speaks nothing", and that is
  true of its imports. It now exports about 140 symbols of three kinds: the
  vocabulary (`Kernel`, `Event`, `Record`, `Reduce`, `Apply`, `Key`, the text
  bounds), the display rules R24 puts in one place (`DisplayName`, `ByUrgency`,
  `Palette`, `Differs`, `Ago`), and SDK machinery — `LastShown` (stateful),
  `View` and `Capabilities` (the protocol between core and a tool-integration),
  and `EachFieldMoved`, a reflection-driven mutator that exists to feed one
  test in each display.
- Anything exported lands here because `layering_test.go` allows six doors and
  this is the one with no dependencies. The protocol and memory types read more
  honestly under `subscribe`; the test helper under a `sessiontest` package so
  its purpose is in its path. This is a relocation, not new weight, and it is
  the lowest-value item here.

### 2. `hook.Main` asks for the payload twice

- `hook.Main(arguments, usage, named, payload any, translate func() (Report,
  bool))` takes a pointer to decode into and a closure that captures the same
  variable. Both adapters' `main` do the dance.
- `Main[P any](arguments, usage, named, translate func(P) (session.Report,
  bool))` decodes into a fresh `P` and hands it over.

### 3. Housekeeping

- Four stale git worktrees under `.claude/worktrees/` and
  `agent-notify/.claude/worktrees/` hold older trees, including sketchybar and
  the menu bar that CLAUDE.md says are gone. They inflate a naive line count
  from 27k to 146k and mislead any tool that walks the directory.
  `git worktree remove` each, or `git worktree prune` after deleting them.
- **Fixed:** D-84 pointed at `simplification-plan.md` at the repository root,
  removed in `2c3f843`; it now says so and that its bullets are what the file
  listed.
- **Fixed:** `TestAnIntegrationIsAskedWhatItAnswers` in `internal/sessionwatcher`
  and `TestStdinReachesTheProgram` in `tool` failed under load: two
  `go test ./...` of core run at once made a stub shell script take longer
  than the 2s `CapabilitiesTimeout` and the test's 1s. The watcher's timeout
  is a var the tests' pace helper lengthens, as it already shortened the
  sweep, and the tool tests that only need a script to run give it ten
  seconds. Two concurrent runs of core's tests now pass. What the watcher
  does with a timed-out `capabilities` in production is still the design
  point: it records the problem and never asks again until a reload, so a
  slow first start costs a display until somebody runs `watcher reload`.
