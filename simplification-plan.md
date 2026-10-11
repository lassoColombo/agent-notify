# Simplification, second pass

Agreed 2026-10-11, from the architecture review of the same day. It becomes
D-98 onward in plan.md §A19 as each step lands, and this file goes the way the
last one did (`2c3f843`) when every step is done.

Three points were the user's to decide, and were decided the same day:

- **Step 3 is worth a package.** The handshake is a concept with a file
  format and three readers, and the import arrow from `command/` into the
  daemon goes.
- **The package is `internal/handshake`**, the word `session/capabilities.go`
  already uses for the exchange, and not `capabilities`, which would read
  `capabilities.Capabilities`.
- **Step 4 is done**, not optional.

Nothing here adds a feature and nothing merges a module: the seven modules stay
seven. Every step leaves all seven green under the pinned toolchain
(`.tool-versions`: go 1.26.2; `go test ./...` and `go vet ./...` in each module,
`TestOnlyTheTopLevelIsImportable` and `TestNoCommandImportsAnotherCommand`
included). Code and comments are cut, not rewritten.

Two findings of the review are withdrawn here rather than planned, and the
reason is recorded so they are not raised a third time:

- **`View`, `Capabilities` and `LastShown` in `session`, and the wake-on sweep
  behind `FieldsRenderedButNotWokenFor`.** D-96 (2026-10-09) decided both:
  the three types stay because the doors an internal may import are the ones
  that import no internal, and the sweep stays private in `session` rather
  than earning a `sessiontest` package. Nothing has changed since. What the
  review did find is that `session.go:20-23` compresses D-96's argument to
  "an internal package import a door", which `internal/containers` already
  does; step 9 says it the way D-96 says it.
- **`containers.Configured` "resolved three times per focus".** It is resolved
  once per process: `focus-session` calls `FocusSession`, `focused` calls
  `IsFocused`, and the session-watcher calls `resolveContainers`, each in a
  process of its own. The review miscounted. Nothing to do.

## The read path

1. **Every reader of the store goes through the one cold read.**
   `command/internal/find` reads `Store.List()` and `Store.ListEnded()` raw
   (`find.go:29-35`, `find.go:82`), so it does not apply liveness and does not
   settle a session that is in both directories. Two consequences a person
   can see: `agent-notify focus-session <TAB>` offers a killed agent glossed
   `blocked-on-you` while `list` beside it says `ended`; and a key caught in
   the live/ended window matches twice, and `focus-session` refuses it as
   ambiguous. `running.go:17-20` names this drift as the reason
   `WhatIsRunning` exists.
   - `find.Session` reads `openedCore.WhatIsRunning(true)`: a session that
     has ended can still be focused (its pane may be there) and annotated.
   - `find.TheSessionsThatAreRunning` reads `openedCore.WhatIsRunning(false)`,
     which is what `list` prints.
   - Test, in `completion_test.go` beside the one that is there: start the
     pretend agent, `pretend.kill()`, complete `focus-session ""`, and the
     session is either absent (the session-watcher filed it) or glossed
     `ended`; it is never `blocked-on-you`. The two outcomes are the ones
     `TestAKilledAgentIsRenderedAsEnded` already accepts.
   - `doctor` keeps reading `List` and `ListEnded` apart: it counts the two
     directories as two checks and judges liveness itself, on purpose.

2. **The store settles a session that is in two files, in one place.**
   `core.WhatIsRunningAndWhatEnded` (`running.go:50-62`) keeps the higher
   sequence of a live and an ended record for one key; `SessionStore.Read`
   (`sessionstore.go:69-77`) applies the same rule to the same pair. One rule,
   written twice, in two packages.
   - `SessionStore.ListIncludingEnded()` lists both directories and keeps the
     newer of a pair by sequence, named after `subscribe.ReadIncludingEnded`
     so the vocabulary stays one. `List` and `ListEnded` stay for `doctor`.
   - `WhatIsRunningAndWhatEnded` calls it when `includeEnded` and drops its
     thirteen lines.
   - Test, in `sessionstore_test.go` beside `TestDuplicateFilesResolveBySequence`:
     a key written to both directories is listed once, with the higher
     sequence.
   - M17's bullet in plan.md says "core now keeps the higher sequence of the
     two"; it becomes the store, in the D-98 entry rather than by editing a
     done milestone.

## The handshake

3. **The handshake is a package, and the session-watcher exports only
   itself.** `focus.go:66` and `doctor.go:214,285` import `sessionwatcher` to
   reach `CapabilitiesByIntegration` and `ReadReport`: two commands depending
   on the daemon's package for a JSON file it writes. Both hand the answer
   straight to `containers`. D-80 laid the tree out as the dependency graph,
   and this is the one arrow in `command/` that points at a process rather
   than at what the command uses.
   - `internal/handshake` takes `capabilities.go` and `report.go` whole:
     `Ask(settings)` (today's `askEveryIntegrationWhatItAnswers`), the
     per-binary `ask`, `Timeout` (today's `CapabilitiesTimeout`), `Report`,
     `Integration`, `Write(layout, settings, answers, unanswered)`, `Read(layout)`
     and `ByIntegration(layout, settings)`. No new logic; the watcher's
     `askWhatEachIntegrationAnswers` stays as the method that logs and stores
     the answers under its mutex, and `writeReport` becomes a call.
   - The name is `handshake`, decided 2026-10-11: `session/capabilities.go:3`
     already calls it that, and `capabilities` would collide with the type
     `session.Capabilities` it carries.
   - `focus` and `doctor` import `handshake` and stop importing
     `sessionwatcher`. What the watcher exports afterwards is `Start`,
     `Watcher.Run`, `SweepInterval`, `WatchExits` and `Exits`: the process,
     and nothing a command needs.
   - Tests: `TestWithNoReportItAsksRatherThanSayingThereIsNoContainer` moves
     to `handshake_test`, since it never starts a watcher. The other three in
     `capabilities_test.go` drive a running watcher and read the report back
     through `reported`, which calls `handshake.Read`; they stay.
   - Recorded as D-99, amending D-80's tree (one more package under
     `internal/`) and naming where D-81's "what core knows is written down"
     lives.

## The session-watcher

4. **The deriving goroutine is handed the world the loop read.**
   `reconcile` reads the store once (D-92) and then asks the deriving
   goroutine to look, and `deriveWhenAsked` (`watcher.go:281`) lists the
   store again for itself: a second `ReadDir` and N `ReadFile`s per wake,
   for the same world. The renderers already have the shape this wants
   (`render.go:42-50`): the newest world under a mutex, a one-slot channel to
   say it moved, the consumer taking whatever is newest when it gets there.
   - `Watcher.toDerive []session.Record` under `w.mu`; `reconcile` sets it to
     `world` and signals `derivations` as it does now; `deriveWhenAsked`
     takes the newest under the lock and derives it.
   - `derive` skips a record whose kernel is not live: the world carries
     ended sessions when a renderer asked for them, and a dead agent the
     judgement has just corrected to `ended` is about to be filed, so neither
     is worth asking a container about. Today's `Store.List()` never showed
     derive either.
   - The guarantees D-91 names hold unchanged: a capture replaced while a
     derive ran is refused by the `Update` closure, which compares under the
     record's lock; and the hook's write that replaced it woke the loop,
     which hands over a newer world next. `TestASlowContainerDoesNotDelayADraw`
     and `TestAContainerIsHandedItsOwnEntryAndTheChain` cover both halves.
   - Recorded as D-100, amending D-91's "the goroutine reads the store".
   - This step is last among the core steps on purpose: the watcher's tests
     are the timing-sensitive ones (`593f61a`), so it lands on a tree where
     everything else is already green. It is done, decided 2026-10-11: a test
     that turns flaky under it is a test to fix, not a reason to drop it.

## Logic written twice, or carrying weight it does not need

5. **`session.Differs` is one comparison.** `display.go:184-231`: `Differs`
   with fields and `moved` without them both marshal both records to field
   maps and compare strings; the only difference is which names. One function:
   build `was` and `now`; with no fields named, the names are every key on
   either side minus `stampFields` and `optInFields`; compare the names.
   `moved` goes. The length check in `moved` is the union of keys, said
   differently. Covered by `usage_test.go:173-250`, `wakeon_test.go:57-64` and
   `model_branch_test.go:71`, which exercise both paths.

6. **`doctor` has one printer.** `report(label, ok bool, detail)` writes
   `ok  ` and `FAIL`; `reportIntegrations` and
   `reportWhatIsInstalledAndNotAskedFor` (`doctor.go:288-291,308,352`) write
   `ok`, `--` and a lowercase `fail` by hand, in a second column format.
   `report(label, mark, detail)` with three marks, `ok`, `FAIL` and `--`, and
   every line goes through it. The README's sample already shows `FAIL`; its
   widths do not move.

7. **`unmerge` finds "ours" once.** `claude/install.go:144-158` counts the
   commands that name this program in one loop, then runs `DeleteFunc` with
   the same predicate. `kept := slices.DeleteFunc(parsed.Hooks, ours)` and
   `removed += len(parsed.Hooks) - len(kept)`, with `ours` a local closure.
   `TestUninstallTakesOnlyItselfOut` covers it.

8. **zellij's `Verdict` uses `paneOf`.** `place.go:187-193` is the loop that
   `paneOf` (`place.go:231`) already is. `place_test.go`'s `Verdict` cases
   cover it.

9. **The notifier stops carrying weight for a one-shot process.** A render is
   a process that posts and exits (`main.go:51-53`), so:
   - `Banners` (`main.go:126-153`) goes; `paint` loops `Notifier.Fresh` and
     calls `Post` for each notice, which is all `Banners.Post` did.
   - `NewNotifier` (`notify.go:57`) goes; `Notifier` has two exported fields
     and a literal says the same thing. `notify_test.go:31` changes one line.
   - `InvaderPNGs` drops `mu` and `theDirectoryHasBeenMade`
     (`invaderpngs.go:40-41`): `os.MkdirAll` is idempotent and one call per
     banner costs a syscall. `Size` and `Directory` stay; the tests set them,
     and `TestTheInvaderPNGsShareOneFixedDirectory` is unchanged.
   - And the phrase in `session.go:20-23`, which is the comment D-96 wrote
     too short: "putting them under subscribe would have an internal package
     import a door that imports internals", so that `internal/containers`
     importing `container` and `tool` reads as the rule rather than an
     exception to it.

10. **picker nits.** `chooseOne` (`main.go:104`) shadows the package-level
    `me`, this integration, with `me, err := os.Executable()`; it is
    `program`, as every other integration calls it, and `Arguments`'s first
    parameter with it. `now()` (`main.go:34`) is `time.Now().UTC()` at its
    three sites.

## Left alone, and why

- `watcher stop` checks `onewatcher.Running` and then `onewatcher.Stop`
  checks it again: two honest lines, and the command wants exit 3 for the
  first.
- `tool.Summarise` cleans the output and then each line: the second pass is
  what bounds the line (`TestSummariseIsBounded`).
- `config.Load`'s four TOML passes: each buys a message a person needs, and
  the file is read once per process.
- The notifier's `Read` on every render: a render is a process, so once per
  render is once per process.

## Order, and what each unit is

Each unit leaves the tree green and is one commit's worth; nothing is
committed until asked.

1. Steps 1 and 2: `fix: every reader of the store goes through one read, and
   the store settles a session in two files`. The only behaviour change in
   this plan, and the one to land first.
2. Steps 5 to 10, in any order, together: `refactor: Differs is one
   comparison, doctor has one printer, and the review's small cuts`.
3. Step 3: `refactor: the handshake is a package, and the session-watcher
   exports only itself`.
4. Step 4: `refactor: the deriving goroutine is handed the world`.
5. The record: D-98, D-99 and D-100 in plan.md §A19, dated the day they land,
   each naming what it amends; `session.go`'s phrase; this file removed.

## Done when

- Every module's `go test ./...` and `go vet ./...` pass under go 1.26.2.
- `agent-notify focus-session <TAB>` after a kill never offers
  `blocked-on-you`, and `focus-session` never calls a session caught between
  `sessions/` and `ended/` ambiguous.
- `grep -rl internal/sessionwatcher command/` names only `command/watcher`.
- `deriveWhenAsked` lists nothing: the only `Store.List()` in
  `internal/sessionwatcher` is gone.
- `grep -n 'func moved\|NewNotifier\|type Banners\|theDirectoryHasBeenMade'`
  across the tree finds nothing.
