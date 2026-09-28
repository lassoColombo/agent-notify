# One payload shape: collapsing the socket protocol onto the render path

D-81 made every integration core knows about a program core RUNS, handed a
`session.View` on stdin. It left the subscribers socket speaking a different
language to the same audience — `hello`, `welcome`, `snapshot`, `delta`,
`gone`, `resync` — for the benefit of clients that no longer exist.

This plan finishes the move. After it, core hands out exactly one shape, on
three surfaces that are now the same surface:

| surface | carries |
| --- | --- |
| `render`'s stdin | a `View` |
| `agent-notify tail --json` stdout | a `View` per line |
| the subscribers socket, after the welcome | a `View` per line |

## What the evidence was

- **`render.go` already does the whole job in 171 lines** — a cap-1 channel,
  `LastShown.Replace(theWorldNow(wantEnded), wakeOn)`, hand it over. The socket
  does the same job in 843 (`subscribers.go` 427 + `subscriber.go` 267 +
  `protocol.go` 149).
- **The only thing the extra 670 lines buy is `Change.Event`, and nothing reads
  it.** Its sole consumer is `replay`, which records it and plays it back into
  a `Fake` so it comes out of another delta. No integration reads it;
  `macos-notifications` reasons from `PreviousKernel`. Renderers never get one
  at all, because `Replace` has none to give — core's two display paths already
  disagree about whether "why" exists and nobody noticed.
- **`Gone` is already structural.** `LastShown.Replace` deletes any key missing
  from the fresh world.
- **`Resync` is dead.** Nothing sends it; its doc justifies it with
  `sketchybar --reload`, and sketchybar went in D-79.

## Three commits

### 1 — Delete what is dead on its own

Nothing here depends on the collapse, and none of it is behaviour.

- `Poke.Sequence`: set by `PokeFor`, read by nobody.
- `peerPID`, `peer_darwin.go`, `peer_other.go`, `Listener.PID`, `Listener.Since`.
  The comment at `subscribers.go:118` justifies the syscall by naming the
  supervisor, which D-81 deleted; the residue is a line of doctor output.
  Doctor keeps the name and loses the pid.
- `Subscribers.Connected()` and `Listening()` merge into `Connected()`.
- Doc drift that now misleads: `subscriber.go`'s `Refused` doc claims a version
  mismatch, but nothing refuses on version (D-77 — announced, never negotiated);
  `logs.go:75` describes core pointing an integration's stderr at the log, which
  `tool.go` has not done since D-81; plus the plain supervisor references in
  `subscribers.go`, `paths.go:214` and `doctor.go:287`.

`Refused` itself **stays**. It is unreachable from the only client, but it is
the server's one way to say why it declined a hello, and a silent close is a
worse answer to a client that gets the handshake wrong.

### 2 — Delete the `Change.Event` slice

A field that travels from the hook's datagram to a display's JSON and is read
by nobody at the far end. It goes as one vertical cut:

`Poke.Event` → `Watcher.caused` (the map, its lock sites, `PokeFor`'s argument)
→ `publish`'s event lookup → `Subscribers.Publish`'s third parameter →
`Delta.Event` → `session.Change.Event` → `Fake.Reported` (which collapses into
`Fake.Publish`) → `replay.RecordedChange.Event`.

This is the one judgement call rather than a mechanical deletion: it is
speculative capability for a notifier that might one day want to be told "turn
finished" instead of inferring it from the kernel. Agreed to delete under "no
concept without weight". `PreviousKernel` stays — it has a real reader.

### 3 — Collapse the protocol

**Server.** Each connection keeps what a renderer keeps: a `session.LastShown`
and a `wake chan struct{}` of one. On wake it does what `renderOnce` does —
`Replace(snapshot(wantEnded), wakeOn)`, skip if not the first view and nothing
moved, otherwise write `ViewOf(changed)` as one line.

**Client.** The switch collapses. Before the welcome, dispatch on kind; after
it, every line is a view.

**Deleted:** `Snapshot`, `Delta`, `Gone`, `Resync` and their kinds; the
per-subscriber `pending`/`order`/`overflowed`/`resync` queue and `bound`; the
`subscriber-queue` config key; `LastShown.Applied` and `LastShown.Forget`;
`Subscribers.Forget`; the `WantEnded` delta special case in `subscriber.go`
(nine lines of comment explaining that without it D-26 is "a convention rather
than a mechanism" — with the server filtering once, there is nothing to say).

**Also deleted, and worth naming because they are not obvious:**

- **`session.WorthOfferingAround`.** It is the generous gate the watcher asks
  before asking each subscriber the narrow one. With per-subscriber diffing the
  narrow question is the only question, asked in the one place that knows the
  answer. The outcome is identical: a usage-only write reaches exactly the
  subscribers that named `usage`, because `Differs(_, _, nil)` ignores
  `usage` and `Differs(_, _, ["usage"])` does not.
- **`View.Why`.** Documented as `"snapshot"` or `"delta"`, which after this
  names two things that do not exist — and the renderer already passes
  `"render"`, which was never in the set. `tail`'s human mode branches on it to
  choose between a table and a row per change; it branches on
  `len(view.Changed) == 0` instead, which is the same question asked of the
  data.
- **`publishDepartures`' store round-trip.** It reads the store to tell "ended
  and filed" from "pruned" so it can send a delta or a `Gone`. With whole views
  the distinction is already in `snapshotFor`: a filed session is in
  `ListEnded()` and reaches a `WantEnded` subscriber, a pruned one is in
  neither and vanishes for everybody. It reduces to dropping the key from
  `w.known`.

**Kept, deliberately:** `View` gains `Kind: "view"`, stamped in `ViewOf` so all
three surfaces carry it. It is 15 bytes a line and it keeps R12 — every message
carries `kind` and a reader dispatches on it — which a positional
"everything after the welcome is a view" stream would quietly drop.

## What must not change

The behaviour these already pin, which is why most of the existing tests should
pass untouched:

- The first view carries no changes (`TestTheFirstViewCarriesNoChanges`).
- A reconnection is not a change (`TestAReconnectionIsNotAChange`) — `LastShown`
  outlives the connection, which is already true on both sides.
- `Changed` compares on the fields the subscriber asked for, not on everything
  (`TestASnapshotComparesOnlyTheFieldsAskedFor`).
- An ended session leaves the view of a subscriber that did not ask for one
  (`TestAnEndedSessionLeavesAViewThatDidNotAskForOne`). Today this is the delta
  special case; afterwards the session is simply not in the fresh world.
- A slow subscriber ends up correct, never wrong
  (`TestASubscriberIsCorrectAfterOverflowing`). The mechanism changes from
  "throw the queue away and send a snapshot" to "the one slot holds the newest
  world", so the test keeps its name's promise and loses its knowledge of
  queues.

## Risks

- **Cost per reconcile.** Every subscriber now diffs the whole world on every
  reconcile instead of being handed the records that moved. With tens of
  sessions and two or three connections this is a map compare that renderers
  already pay unconditionally on the same tick.
- **Latency is unchanged.** Both paths are driven from `reconcile`; the poke
  replaces the publish at the same point in the same function.
- **`replay` recordings lose a field.** Anything recorded before this that
  carried `event` still decodes — it is `omitempty` on a struct that no longer
  reads it.

---

# What was actually built, 2026-09-28

All three steps landed, all nine modules green, and the code is 441 insertions
against 677 deletions. Two things went differently from the plan above.

## The socket carries a `Snapshot`, not a `View`

The plan opened with a table saying all three surfaces would carry a `View`.
They do not, and writing it that way would have broken a property the tests pin
deliberately.

A `View` is the world **plus what changed in it since you last looked**. Only
something with a memory can say the second half, and the two ends have memories
with different lifetimes:

- A display core RUNS is a fresh process per render. It remembers nothing, so
  core keeps its `LastShown` and fills in `Changed` for it.
- A client on the socket outlives its own connection *and* the session-watcher.
  It keeps its own `LastShown` and fills in `Changed` for itself.

Move that job to the server and it dies with the process that knew it:
`TestASnapshotStillSaysWhatASessionMovedFrom` stops the watcher, starts another,
changes a session, and requires the client to still be told what it moved from.
A server-side `LastShown` cannot answer that — it was born empty a moment ago.

So the wire carries the world, and a view is made at whichever end survives.
`View.Kind` was therefore not needed and was not added.

The server does keep a second, narrower `LastShown` per connection, and it
answers only "is this worth writing". Without it `wake_on` means nothing on the
wire — verified live: a `tail --json --wake-on kernel` and a plain `tail --json`
watching the same session through four events saw 4 and 5 worlds, the picky one
skipping `sequence: 3`, the message-only change, entirely.

## It found a bug that predates it

A session LEAVING the world is not a change to any record, because there is no
record any more. `LastShown.Replace` only ever reported records present in the
world it was given, so a display woken by a prune compared everything it held
against everything it was given, found them identical, and drew nothing.

That was live on the renderer path before any of this: `prune` wakes every
display with a comment explaining that this change "comes through as a record
ceasing to exist", and `renderOnce` then decided nothing had happened. A bar
kept the row of an agent that finished ten minutes ago.

`Replace` now returns `(changed, departed)` and both display paths ask about
both. `TestADisplayIsDrawnAgainWhenASessionLeaves` covers the renderer path and
fails without the fix; `TestAnEndedSessionLeavesAViewThatDidNotAskForOne`
already covered the socket path and caught it during this work.

## Smaller notes

- `Refused` stayed, as planned, with its doc corrected: it is unreachable from
  the only client there is, but it remains the server's one way to say why it
  declined a hello, and a silent close is a worse answer.
- `View.Why` went. `tail`'s human mode now branches on `len(view.Changed) == 0`,
  which is the same question asked of the data rather than of a label.
- Two writes in the same instant to different sessions used to be two messages
  and are now one world carrying two changes. `TestTenLineSubscriberSeesChangesLive`
  had assumed one message per write and now waits for the first to land.
- Noticed and not fixed, because it is out of scope: `agent-notify watcher start`
  resolves the binary from `PATH` and ignores `agent-notify-binary`, where the
  hook path honours it. It cost an hour here — the live check silently ran the
  installed pre-handshake watcher against a freshly built client.
