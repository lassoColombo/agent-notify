# The store is the bus

Agreed and built 2026-09-28, from the architecture review of the same day.
Five of its six findings; the one left out is one Go module (2). Recorded as
D-83 in plan.md.

Every step leaves all nine modules green. Code and comments are cut, not
rewritten: a comment survives only where the code cannot say the thing itself.

## 1. Both sockets go; the store directory is the wake-up

Since D-82 the poke carries nothing and the stream carries whole worlds that
every reader diffs for itself. Both say "something in `state/sessions/` or
`state/ended/` changed", and the filesystem already says that: every record
write is a rename into one of those two directories, which a kqueue
`EVFILT_VNODE` watch on the directory reports.

- `internal/sessionwatcher/dirwatch_darwin.go` — a kqueue on the two
  directories, `Changed(within)` blocks until one moves. Darwin only, like
  `exits_darwin.go`.
- The session-watcher wakes on it instead of on a datagram. `reconcile` is
  unchanged. It no longer serves anything: `subscribers.go`, `poke.go`,
  `Listen`, `Serve`, the `connected` union in the report go.
- The hook stops poking. `wake` is `StartIfNobodyIs`, which already probes the
  lock rather than a socket.
- `subscribe.Run(ctx, integration, onChange)` is the resident loop: watch the
  directories, read the store through `WhatIsRunning`, `LastShown.Replace`,
  hand over a view when something the integration asked about moved. It starts
  a session-watcher if none holds the lock, once. `tail` calls it; the two
  macOS displays call it instead of forking `agent-notify tail --json` and
  parsing its stdout. `RunThroughTheCLI`, `internal/subscriber` and
  `session/protocol.go` go.
- `record`/`replay` go: they were a fake socket server, and a display test is a
  view piped to `render`.
- `paths` loses the socket paths and the length checks; `doctor` loses the
  `sockets` line and the socket probe.
- Tests: the socket tests are rewritten as tests of `subscribe.Run` against a
  temporary store, keeping what they pinned — the first view carries no
  changes, `wake_on` filters, an ended session leaves a view that did not ask
  for one, a change carries what it moved from. The root tests keep working
  unchanged.

Amends §A13, §A9.2 (sockets unlinked and rebound), §A12.2, D-19, D-82; retires
R15. Recorded as D-83.

## 3. The display SDK the handshake plan promised

- `subscribe.Main(integration, args)` dispatches `capabilities`
  (`render` iff `Render != nil`, plus `WakeOn`/`WantEnded`),
  `capture-environment` (`Reads`, nil answers `{}`), `render` (the view on
  stdin, or the store), then `Commands`, then `Default`. The four hand-written
  switches become one call each.
- `Integration.Focus(key)` runs `agent-notify focus-session --quiet`; both
  macOS displays had a copy.
- `Integration.PrintTable(out, problems, table)` is the D-66 sentence, once.
- `bundle.go` moves to `subscribe/bundle.go` with `Program`, `Identifier` and
  `Icon` as fields; the generic bundle tests move with it, the icon tests stay
  where the drawing is.
- `tool.AbsolutePath(key, given)` replaces `TheZellijToRun` ×2 and
  `TheAerospaceToRun`.
- `subscribe.Commands` carries the container verbs too, so `container.Main`
  and `container.Integration` went; `container` is the vocabulary only. One
  SDK, which is what made 4 a move rather than a merge.

## 4. One zellij program

`agent-notify-zellij` answers `render`, `interpret-environment`, `focus` and
`focused` from one capture. Of D-37's four reasons for two programs, two died
with D-81 (nothing is supervised, nothing is retired), one with the monorepo
(a shared library is a package), and the last — wanting titles without focus —
is a line in `[container] order` rather than an install. The config table is
`[integration.zellij]` and the order names `zellij`.

## 5. The hook reads the config, not the report

Every integration with a `binary` answers `capture-environment`, so the report
buys the hook nothing. `TheIntegrationsToAsk` goes; the hook asks
`config.Config.Runnable()`. `focus` keeps reading the report, with its
fallback; `doctor` keeps showing it.

## 6. The small cuts

- `Exits.watched` is touched from two goroutines: a mutex.
- `watcher status` goes; `doctor` says the same.
- `config.removedKeys` and its tests go.
- `session.Apply` stops stamping `Key`, `Sequence` and the times; the store
  does, on every write. The reducer tests that read them now read them off the
  store.
- `prune` runs before `reconcile` on the tick, so the extra draw goes.
- claude reads the event from `hook_event_name` like codex; its `install`
  writes one command with no event word.
- `hook.ReadBackwards` replaces the three copies of the block-wise reverse
  JSONL reader in the two agent-integrations.
- Dead: `session.Urgency`, `Capabilities.Answers`, `Exits.Forget`,
  `Exits.Watching`, the bar's `theValueOrTheFallback`, claude's
  `Payload.Model`, codex's pointer `LastAssistantMessage`,
  `subcommand.Summarise`.
- `rows` uses `session.Ago`.
- Comments that describe a daemon, a supervisor, a socket, M8/M9, `schema`, a
  `MarshalJSON` that does not exist, or `enabled = false`, are deleted or
  corrected.
