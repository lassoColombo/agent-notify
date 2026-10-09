# Architecture review, 2026-10-09

A read of all seven modules for separation of concerns, abstraction and
correctness, after M17. The bus (D-83) and the layering (D-80) hold; what is
left is one bug, a handful of places that do one job twice, and one package
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
- **Dead since D-87.** `JustArrived`, `Arrival`, `Announced`, `Same` and
  `moreRecent` in `session/display.go`, with `arrival_test.go`, had no caller
  once the menu bar went. `plural` in the notifier's `preview.go` likewise.
  Removed, and the README's list of display rules updated.

## Remaining

Ordered by what they cost, most first. Each names the code and the shape of
the fix; none is started.

### 1. One failing capture voids every container's coordinates

- `hook.worthCapturing` (`agent-notify/hook/hook.go`) recaptures whenever the
  set of integrations that answered differs from the set that would be asked.
  `session.Apply` then replaces `CapturedContext` whole and sets
  `DerivedContext` to nil.
- So aerospace timing out once makes the next hook capture zellij again and the
  session-watcher run `zellij action list-panes` for it again, up to
  `attemptsBeforeGivingUp` times, for a session whose zellij answer never
  changed.
- Both maps are already keyed per integration. The fix is to replace
  `CapturedContext.By[name]` and void `DerivedContext[name]` per key, and to
  decide "worth capturing" per integration rather than per record. The
  invariant §A7.4.1 wants — a derived entry never outlives the capture it came
  from — is kept per key just as well.

### 2. `interpret-environment` runs on the control loop

- `Watcher.derive` is called inside `reconcile`, synchronously, with a 5s
  timeout per (session, container) and up to three attempts. While it runs,
  exit notifications and store wakes sit in the one-slot channel and nothing
  is drawn.
- The loop already wakes itself when it writes a record ("a record it writes
  itself wakes it once more"), so derivation can run on its own goroutine and
  write through the store with no new mechanism. The refusal counter it keeps
  already lives behind `w.mu`.

### 3. Liveness is computed N+1 times per change

- `reconcile` judges every live pid through `process.Ended`, then each renderer
  calls `Core.WhatIsRunning` (`internal/sessionwatcher/render.go`), which lists
  the store again and probes every pid again. `subscribe.Run` does the same
  for a display that owns its process, which is right for that one.
- D-84 chose `WhatIsRunning` so that every reader sees one world. That stays
  true if `reconcile` reads the world once (ended included) and hands it to the
  renderers, each of which drops ended sessions unless it asked for them.

### 4. Core captures a process chain nobody reads

- `hook.look` walks the ancestry and stores it as `captured_context.ancestry`
  (`session/record.go`). Outside tests the only reference to the field is
  `Record.Clone`.
- aerospace walks the tree a second time in its own capture
  (`tool-integrations/agent-notify-aerospace-container/ancestry.go`), with its
  own `commandName`, a bound of 12 against core's 10, and its own `x/sys`
  dependency, because `interpret-environment` is handed only the integration's
  own blob.
- Two ways out, and either is better than both walkers: hand core's chain to
  `Interpret` beside the blob and delete the aerospace walker; or stop storing
  a chain on every record and let aerospace keep its own. The first keeps
  §A8.4's chain where `doctor` could one day show it; the second is less code.

### 5. The render decision is written twice

- `subscribe/run.go` and `internal/sessionwatcher/render.go` both do
  `LastShown.Replace`, then "first view, or something changed, or something
  departed", then `ViewOf`. Eight lines each, and the exact case R24 names.
- One method on `LastShown` — replace the world and answer with the view to
  hand over, or nothing — serves both.

### 6. Integrations open the log twice

- zellij's `main.go` and the notifier's `main.go` and `alerter.go` call
  `logs.Open(Name)` while `subscribe.Integration.core()` has already opened a
  logger for the same component on the same file.
- An `Integration.Logger()` on the SDK removes the second open, and `logs`
  stops being a door: the layering test's list shrinks by one and the
  variable that sets the level is reached through `subscribe` like everything
  else.

### 7. `session` carries three vocabularies

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

### 8. `hook.Main` asks for the payload twice

- `hook.Main(arguments, usage, named, payload any, translate func() (Report,
  bool))` takes a pointer to decode into and a closure that captures the same
  variable. Both adapters' `main` do the dance.
- `Main[P any](arguments, usage, named, translate func(P) (session.Report,
  bool))` decodes into a fresh `P` and hands it over.

### 9. Housekeeping

- Four stale git worktrees under `.claude/worktrees/` and
  `agent-notify/.claude/worktrees/` hold older trees, including sketchybar and
  the menu bar that CLAUDE.md says are gone. They inflate a naive line count
  from 27k to 146k and mislead any tool that walks the directory.
  `git worktree remove` each, or `git worktree prune` after deleting them.
- D-84 points at `simplification-plan.md` at the repository root, which no
  longer exists.
- `TestAnIntegrationIsAskedWhatItAnswers` in `internal/sessionwatcher` failed
  once under a whole-repository `make test` (the 20s wait for the report
  expired) and passed on every run since, alone and in the package. Worth
  watching; it may be the full run contending for the stub binaries.
