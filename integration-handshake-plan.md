# One handshake, one interface, no long-lived integrations

Agreed 2026-09-27, implemented 2026-09-28 over eight steps, and recorded as
**D-81** in plan.md, which is now the place to read it. This file is the working
plan it was built from, kept for the reasoning behind each step and the order
they had to happen in. Every claim marked **[verified]** was
checked against the code, with the file and line that shows it.

References were re-grounded after `499d7c9 refactor!: lay the core module out as
its dependency graph`, which moved the root package into `session/`, the
commands into `command/<name>/`, and `internal/watcher` into
`internal/sessionwatcher`. Paths below are against that layout.

## Why

`supervisable()` at `internal/sessionwatcher/supervise.go:102` decides whether an
integration is a daemon by asking whether it is **absent from `[container]
order`**. That key exists to say how shells nest, which §A11.2 calls the one
thing only the user knows. So a property of the *program* is being read out of
the *user's file*, which is what §A10.3 ("capabilities are declared, not
configured") and D-66 ("an integration states what it needs; it does not write
it") both exist to prevent.

§A10.3's declaration channel is the connect handshake, and that channel is only
available to something that already decided to connect. Lifecycle is the one
fact needed *before* that decision exists. The fix is a declaration reachable
without connecting.

## The decisions

1. **One handshake for every tool-integration**, over a `capabilities`
   subcommand that core execs. Agent-integrations are out: they are one-shot
   hook processes that link the library and never talk to the session-watcher.
2. **Capabilities are methods, not roles.** A method list is what core
   dispatches on; a role is a category a human infers and then writes down
   twice. "Is a container" becomes "answers `focus`".
3. **Every integration is short-lived.** One interface: core execs the program
   with a subcommand, JSON on stdin, JSON on stdout. There is no `lifecycle`
   field because there is only one lifecycle.
4. **A surface that must own a process stops being an integration and becomes a
   client of the CLI** — started by launchd, consuming
   `agent-notify tail --json --wake-on …`. §A10.2 already blesses both halves:
   unsolicited connections are first-class, and "the CLI is a client, never a
   second implementation".
5. **`capture-environment` is universal.** Every integration core knows how to
   run is asked; no one declares it; the SDK answers `{}` for an integration
   that reads nothing; core records no entry for an empty answer. This removes
   `capture-environment` from the user's config entirely.
6. **`binary` in a table means "core may run this."** A client keeps its table
   for settings and has no `binary`.
7. **The display verb is `render`**, one name for all displays, replacing
   zellij-display's private `repaint`.

### Agreed after the codebase sweep

8. **`render` is handed the view on stdin**, like every other subcommand, rather
   than reading the store itself. A forked render has no memory of what it
   painted and the cold read returns live sessions only, so a render that reads
   the store can never give a pane back — `repaint`'s own comment
   (`agent-notify-zellij-display/main.go:103`) says exactly that, and the
   subscription sets `WantEnded: true` (`main.go:88`) for exactly that reason.
   The consequence: the "since the last call" memory that `subscribers.go` keeps
   per connection moves into the session-watcher, keyed by integration name,
   where it survives the display crashing.
9. **The handshake has four fields, not three.** `want_ended` is behavioural
   (`session/protocol.go:53`, "a bar says no and a picker says yes") and once
   core composes the payload rather than a subscriber filtering it, core has to
   be told.
10. **The discovered capabilities live in `integrations.json`**, the file the
    supervisor writes today (`internal/paths/paths.go:216`: "what the supervisor
    knows about its children, written for doctor to read from another process",
    in the runtime directory so it cannot outlive a reboot). It is repurposed,
    not deleted. This is forced: the focus path is **not** in the session-watcher
    — `containers.FocusSession` and `IsFocused` are called from the CLI
    (`command/focus/focus.go:58` and `:133`), and both macOS displays shell
    `agent-notify focus-session --quiet <key>` on a click
    (`agent-notify-macos-bar/main.go:394`,
    `agent-notify-macos-notifications/main.go:352`), so a short-lived process on
    an interactive path needs the answer without forking every container to get
    it. **The hook reads the same file**, which is what makes universal capture
    safe (see the pitfall below).
11. **`tail --json` emits one object per view** — `{"why":…, "sessions":[…],
    "changed":[…]}` — instead of one line per changed record
    (`command/tail/tail.go:68-75`). A notifier client wants `changed`; a bar
    client needs `sessions`, and cannot have them today because `changed` is
    empty on the opening snapshot by contract. This makes `tail` the single
    serialisation of a `View`.

### The handshake

```json
{
  "version": "0.0.0-dev",
  "methods": ["render"],
  "wake_on": ["kernel", "detail", "rank", "name", "cwd", "state_since"],
  "want_ended": true
}
```

A container's is `{"version": …, "methods": ["interpret-environment", "focus", "focused"]}`.

- `version` — the core it was built against. Reported by `doctor`, branched on by
  nothing (D-77). It is carried **because** it does nothing yet: M18 is when
  versions can differ, and by then the integrations you need it from were built.
  Its first real job is the reinstall message in the pitfall below.
- `methods` — **what is worth calling, not what will not error.** A container
  that implements `focused` but can never answer should not list it. Derived
  from which functions the author filled in, never written down.
- `wake_on` — unchanged in meaning, larger in consequence: it stops gating a
  socket write and starts gating a fork.
- `want_ended` — whether a render wants ended sessions in its payload.
- **No `name`** — core already knows it from the config key.
- **No `roles`**, **no `lifecycle`**.

## What the fields do today [verified]

- **`Roles` is decorative.** Zero branches: `command/doctor/doctor.go` prints it,
  `internal/sessionwatcher/subscribers.go` logs it and copies it into the
  Listener, `supervise.go:78` copies it into the report. Deleting it changes no
  behaviour.
- **`WakeOn` is the only field with behaviour**: `subscribers.go:181`,
  `if !session.Differs(previous, next, one.hello.WakeOn)` → skip. Validated by
  `session.ReasonTheseFieldsCannotBeWokenOn` (`session/wakeon.go`, D-70),
  applied client-side in `subscribe.go`, passed through by `tail.go`.
- **`Version` is announced and never negotiated** — `session/protocol.go:44-47`
  says so, citing D-77.
- **`CaptureEnvironment`** is `internal/config/config.go:132`, single consumer
  `hook/hook.go:210` inside `wouldBeAskedNow` (`hook.go:204`).
- **`WantEnded`** is `session/protocol.go:53`; zellij-display asks for it so it
  can give a pane back.

## Facts that shaped this [verified]

- **The hook does not capture per event.** `hook.go:189` gates it behind
  `worthCapturing(previous, wouldBeAskedNow(settings), report.Process)` —
  nothing stored, different process, or the asked-set changed. Steady state
  spawns nothing.
- **Captures run concurrently under one shared `capture-timeout`**
  (`hook.go:35`), so N integrations cost one timeout, not N.
- **`hook.go:270` already refuses to record a nil answer.** Its comment's stated
  reason is stale — `worthCapturing` now takes the asked list — so rewrite it:
  an entry that says nothing is not evidence of anything.
- **`capture.Main` treats a nil `Reads` as failure** (stderr, exit 1). Becomes:
  answer `{}`, exit 0.
- **`capture.Command`'s doc comment is stale** (`capture/capture.go:33`) — says
  `install` writes `capture-environment = true`; D-66 made install print, and
  this change removes the key.
- **An unimplemented container verb costs a fork and lies.**
  `container.answer` returns `errUnimplemented` → exit 1 → `containers.Focused`
  (`internal/containers/containers.go:110`) turns it into `CannotTell` with the
  error as detail, so "never implemented" reads exactly like "the tool could not
  tell" — and `IsFocused` needs unanimous yes, so one container that never
  implemented it leaves every session permanently unsure.
- **`containers.Configured`** (`containers.go:53`) derives containers from
  `[container] order`, reports two problem classes, and **does not check the
  binary exists**. Callers: `internal/sessionwatcher/watcher.go:326` (problems to
  the log) and the two CLI focus paths. `doctor` never calls it.
- **`whoIs`** (`supervise.go:194`) exists only to guess which socket connection
  belongs to the child core spawned — by peer pid (`peer_darwin.go`) or by name.
- **zellij-display already has the one-shot path**: `repaint` (`main.go:39`),
  no socket, idempotent — but it cannot un-paint (decision 8).
- **`agent-notify tail --json --wake-on` already exists**, so the client path
  needs a payload change and nothing built.
- **macos-bar genuinely cannot be short-lived**: `NSStatusItem` dies with its
  process, `NSApplication` insists on the main thread (`main.go:38`) and a run
  loop.
- **macos-notifications needs residency only for the click-back.** Posting is
  XPC and needs no run loop; without one, a click is answered by macOS with "the
  application is not responding properly".
- **`exits_darwin.go` stays.** It watches processes *we did not start* — agent
  liveness — not children, and nearly got written off with the supervisor.
- **`subscribe` is two packages wearing one name.** `settings.go` (118 lines:
  `Settings`, `ConfigFile`, `CoreBinary`) is used by all six integrations,
  containers included; `subscribe.go` (493) plus `fake.go` (133) plus the loop's
  tests (794) have three remaining callers, all inside core — `tail`, `replay`,
  `doctor`.

## Pitfalls, and what closes each

- **Universal capture would exec two GUI binaries that have no such subcommand.**
  Neither `macos-bar/main.go:41-56` nor macos-notifications has a
  `capture.Command` case; both fall to `default:` → usage on stderr → exit 1,
  logged on every qualifying hook event. **Closed by decision 10**: the hook asks
  whoever the cache says answers, and falls back to "everyone with a binary" only
  when there is no cache yet. That is why discovery ships *before* universal
  capture in the order below.
- **The picker is a TUI with a `binary`.** Its `main` runs `Apply(me)` and then
  rejects an unknown argument with exit 2 (`agent-notify-picker/main.go:58-77`),
  so under decision 6 the hook would fork a terminal UI on the agent's path.
  Same fix as the open question: either it answers `capabilities` with no
  methods, or it stops having `binary`.
- **Version skew stops being theoretical.** After universal capture, core assumes
  every configured integration answers `capabilities`; the binaries on your PATH
  are stale (see todo.md). An integration that does not answer is recorded as
  answering nothing, and `doctor` says "did not answer `capabilities` — reinstall
  it". This is the first real job `version` has.
- **A poke must follow the store commit.** Only matters if a render ever reads
  the store; decision 8 removes the question by handing the view over.
- **Three `install` tables and their tests assert the key that is going away** —
  `zellij-{display,container}/install.go:25-32`,
  `aerospace-container/install.go:25`, each with an `install_test.go:106`, plus
  `internal/config/config.go:251`'s migration message and
  `config_test.go:235`'s assertion of its wording.
- **The test diff is larger than the code diff at the end.**
  `supervise_test.go` is 360 lines about a supervisor being deleted, and
  `main_test.go:110,183,377` builds a fake container around the old contract.

## Implementation order

Changed after the sweep: discovery and the cache now come *before* universal
capture, because the hook reads the cache. Each step leaves the tree green and is
reviewable alone.

1. **The vocabulary, and the SDKs answer.** *(done, branch `worktree-handshake`)*
   `session/capabilities.go` — the `capabilities` subcommand, the four method
   names, the `Capabilities` type. `container.Main` answers it, derived from
   which functions are non-nil. Each display answers it from one declared value
   it also subscribes with, so the two cannot disagree. `capture.Main` answers
   `{}` for a nil `Reads`, **and the hook drops an empty answer in the same
   step** rather than in step 4: one without the other leaves a window where `{}`
   is stored as though it meant something. The three displays and the picker also
   answer `capture-environment` with `{}`, so universal capture is safe to switch
   on in step 4 rather than only after step 6 moves the macOS two out of reach.
   Otherwise additive: nothing consumes the handshake yet.
2. **Discovery and the cache.** *(done)* The session-watcher asks everyone at
   start and on `watcher reload`, concurrently, under one timeout, and writes the
   answers into `integrations.json` — the same report file, with an `answers`
   object per integration, and now listing **every** integration core can run
   rather than only the supervised ones. `doctor` prints what each answers and
   fails when one could not be asked. Nothing dispatches on it yet.
   `everyIntegrationCoreCanRun` arrives here and `supervisable` is rewritten in
   terms of it, so step 3 deletes a wrapper rather than untangling a predicate.
3. **Consume `methods`.** *(done)* `supervisable()` is now "answers `render`";
   a container is "answers `focus`" and `[container] order` keeps only its
   nesting meaning; the CLI focus path reads the report, and asks when there is
   none, so a focus still works on a machine where nothing is running;
   `IsFocused` counts only the containers that answer `focused`; `derive` skips
   one that does not answer `interpret-environment`. `Roles` is gone end to end.
   Two things fell out: the picker was being supervised as a daemon (it has a
   binary and was not in `[container] order`), and a container that answers
   `focus` but is in nobody's order is now reported instead of silently unused.
4. **Universal capture.** *(done)* The hook asks whoever the report says
   answered, falling back to everyone with a binary when there is no report —
   never asking, because R1 owns that path. `capture-environment` has left
   `config.Integration`, all three install tables, and the READMEs that told
   people to write it; `removedKeys` now explains it alongside `capture` and
   `focus`. `wouldBeAskedNow` is gone: the list is computed once and handed to
   both callers, so the two cannot disagree by construction rather than by
   comment. (The empty-answer drop and the stale comments went with step 1.)
5. **The payload, and the verb.** *(done)* `View` and `Change` moved from
   `subscribe` into `session` with JSON tags: they stopped being Go types the
   moment `render` reads one on stdin. `tail --json` emits whole views rather
   than only what changed (decision 11). zellij-display's `repaint` became
   `render`, taking the view on stdin and falling back to the store when there
   is nothing there — which folds the cold path into the same verb instead of
   keeping two.

   **The cutover is NOT in this step, and the reason is an ordering mistake in
   this plan.** Making "answers `render`" mean "exec on demand" forks every
   renderer per change, and two of the three are the macOS displays, which must
   own a process. Nothing in the handshake distinguishes them — deliberately, we
   removed `lifecycle`. What distinguishes them permanently is `binary`
   (decision 6), and they stop having one when they become clients. So the
   clients have to land first. Steps 6 and 7 below are the old step 5 split
   around that.
6. **macos-bar and macos-notifications become clients.** *(done)* Both run
   `agent-notify tail --json --wake-on …` themselves through
   `subscribe.RunThroughTheCLI`, restarting it when it stops, and neither has a
   `binary` — so core does not start them, does not ask them anything, and does
   not run them on the hook path. Both answer `capabilities` with no methods,
   which is the truthful form of "core runs nothing here". `install` prints a
   launch agent naming the binary inside the bundle, and never loads it (D-66).
   macos-notifications keeps click-to-focus: the tap is answered on its own main
   thread, so it needs the process it now has.
   The report gained a line shape for a table with no binary — "yours to start;
   core never runs it" — so a display that is configured and not running is
   visible in doctor as itself rather than as nothing.
7. **The cutover: core runs `render`.** *(done)* zellij-display is the only renderer with
   a binary by then, so "answers `render`" can mean "exec on demand" without
   qualification. The session-watcher keeps per-integration "last shown" memory
   — the `Changed` contract survives the fork, and survives the display crashing
   too, which it does not today — execs on a change that passes `wake_on`, with
   at most one render in flight and one pending (a buffered channel of one,
   which is both the coalescing and the serialisation) under a timeout, and
   renders once at startup for the cold path.
8. **What is left of the deletion.** *(done)* The child lifecycle, `whoIs`,
   `integration-tries`, the backoff and `supervise.go` itself went with step 7 —
   there was no way to render on demand and keep supervising the same program.
   What remains: split `subscribe` into a public `settings` package and an
   internal loop (`subscribe.Run` has no callers outside core now), write the
   decision entries in plan.md, and finish the READMEs.

## Simplifications that fall out

- **The version-refusal path on the socket loses its reason to exist.**
  `KindRefused`, the `Refused` error and §A10.5's "across majors nothing meets at
  all" guard a connection between separately released programs; after step 6 the
  only thing that connects is `tail`, which ships in the same binary.
- **`integration-tries` leaves the config** with the per-display failure counting
  and backoff. A one-shot render that fails is a fork that failed, retried on the
  next change that passes its `wake_on`, with no state to get stuck in.
- **`CannotTell` goes back to meaning what it says.**
- **Two SDKs with one shape**: `container.Main` and the display SDK, each a
  switch over `capabilities`, `capture-environment` and its own verbs, both
  dispatching capture through `capture.Main` (D-59's precedent).

## Decisions to record in plan.md

One entry covering it, amending **§A10.2** (two lifecycles), **§A10.3** (the
declaration channel), **D-6** (long-lived subscribers), **D-38** (containers are
not daemons — now declared, not inferred), **D-39** (the hook cannot ask, so
`capture-environment` is configured — now universal, so it need not ask), and
**D-66/§A14** where `capture-environment` was part of the printed table.

Also fold in the two findings from the `doctor` thread, which this supersedes:
`doctor` could not see a non-resident integration at all, and
`containers.Configured`'s problems never reached it.

## Still open

- ~~Where the picker's path lives~~ — answered 2026-09-28: in the keybinding,
  and nowhere else. The case for a second key was that other clients would want
  a path, and then both macOS displays became clients with no path in their
  tables at all — theirs is in the launch agent. So no new key: the picker's
  table is a heading and its settings, and the absence of `binary` is what says
  core must not run it. Its `enabled = false` went with it, which was never a
  statement but a workaround for a rule that said "started if enabled and has a
  binary".
- ~~Whether `macos-notifications` keeps click-to-focus~~ — answered 2026-09-28:
  it keeps it, as a launchd client, because the tap is answered on its own main
  thread and a one-shot would have banners nobody can click.
