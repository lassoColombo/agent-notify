# Simplification

Agreed 2026-09-28, from the architecture review of the same day: the findings
under "simpler logic" and "easier to maintain", all of them. Recorded as D-84
in plan.md. Nothing here adds a feature and nothing merges a module: the eight
modules stay eight, because a window manager, a multiplexer or an agent
somebody else uses is a module somebody else writes.

Every step leaves all eight modules green. Code and comments are cut, not
rewritten: a comment survives only where the code cannot say the thing itself.

## Core

1. **One world.** `Core.WhatIsRunning` is what every reader is handed, the
   watcher's own renderers included. The watcher's `known` map, `snapshotFor`,
   `remember` and `forgetWhatLeft` go: a killed agent then shows `ended` in a
   render core runs and in a display that owns its process at the same moment.
2. **The wake channel coalesces.** `derive` writes a record from inside
   `reconcile` and that write wakes the watcher again. The channel holds one
   wake instead of sixty-four, so the self-wake costs one extra `reconcile`
   that finds nothing to derive, and `reconcile` says so in one sentence.
3. **Capabilities have one shape.** `session.Capabilities` is what is asked,
   what the report file carries and what `containers.Configured` and the
   renderers read. `methodsIn`, `methodsByIntegration` and the flattened
   `map[string][]string` go.
4. **An empty capture is stored.** `askEveryoneWhoCaptures` keeps `{}` under
   the integration's name, so `worthCapturing` compares who was asked with who
   answered and stops re-capturing on every hook the day a runnable
   integration has nothing to read. `derive` skips an empty capture.
5. **Sort where the view leaves.** `ByUrgency` runs in `ViewOf`, in
   `WhatIsRunning` and nowhere else on a path that already sorted.
6. **`Core` is opened once per process.** `subscribe.Integration` keeps the
   core it opened; `Settings`, `CoreBinary`, `Read`, `History` and `Run` share
   it. `Core` carries the configuration's problems, so `Settings` and `doctor`
   stops loading the file again to find them. `config.Load` runs in `OpenAt`,
   on reload, and in `doctor`, which reports the directories and the file as
   two separate checks and is the one caller that wants them apart.
7. **One way to a layout.** `watcher run` resolves its layout from the
   environment the spawner kept for it, so the three flags and the field-by-
   field patch go; `FromEnvironmentOrUnder` is inlined into its one caller;
   `layout.Create` runs once per open.
8. **Packages that carried no weight.** `capture` goes: the command word joins
   the other subcommand words in `session`, and `Reads` is the field on
   `Integration`. `session.Capabilities.Answer` goes. `internal/subcommand`
   becomes `tool.Ask`. `command/internal/exit` is `os.Exit` where a command
   has a code. `container` stays: it is the words a container answers in, and
   D-83 chose that on purpose.
9. **`internal/sessionwatcher` is the process and nothing else.** The lock and
   the spawn move to `internal/onewatcher`; the kqueue on the store directories
   moves to `internal/storewatch`. `hook` and `subscribe` import those and not
   the daemon.

## Maintenance

10. **The layering test keeps what it checks and drops what it measures.** The
    floor table and its test go; the doors test and the no-command-imports-a-
    command test stay.
11. **Build tags say what is true.** `//go:build unix` comes off files whose
    only implementations are darwin. The XDG branches in `paths` stay: M17 is
    still planned and they are tested.
12. **The prose describes the bus that exists.** Sockets, datagrams, pokes,
    subscribers and connect handshakes leave the comments; the tombstone for
    `WorthOfferingAround` goes; `tail` derives its completion from the record.
13. **The SDK refuses what it can.** `subscribe.Main` refuses a `WakeOn` naming
    no field and a `Named` subcommand that shadows a verb core runs.
14. **What every integration copied moves into core.**
    - `subscribe.Install`: the no-options check, the path of this binary, the
      lookup of the tool it drives, the table and the advice, once.
    - `subscribe.InstallBundle`: the two macOS displays' install, once.
    - `hook.Main`: an agent-integration's dispatch, once.
    - `hook.LastResponses`: reading a transcript backwards for what the last
      responses cost, once.
    - `session.FieldsRenderedButNotWokenFor`: the wake-on test's body, once.
    The copies, and their copied tests, go.
15. **Settings are read one way.** Every tool-integration has `Read(me)
    (Settings, error)`, read lazily so that `install` still runs against a
    broken file. The picker keeps drawing its complaints in the header, which
    §A14 asked for; it reads them the same way.

## Record

16. D-84 in plan.md, the README's library section, `todo.md`.
