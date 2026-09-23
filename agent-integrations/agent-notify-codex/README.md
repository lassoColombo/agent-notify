# agent-notify-codex

Codex CLI's agent-integration for [agent-notify](../../agent-notify): it turns
codex's hooks into agent-notify's nine events, and does nothing else.

Everything peculiar to codex lives in `mapping.go`. Everything after it is
`hook.Record`, which is identical for every agent. Core knows nothing about this
program and never will.

## Installing

```
agent-notify install codex          # appends to ~/.codex/config.toml
agent-notify install codex --print  # show what it would add, change nothing
```

It appends — your model, your sandbox policy, your MCP servers and your comments
survive — refuses a file it cannot parse, and changes nothing the second time.
Hooks somebody else put there are left alone; codex allows several per event.

**One step is yours.** Codex asks you to trust a hook the first time it runs one
and records the answer in that same file. This installer deliberately does not
write that entry: the hash is codex's own, and writing one would be forging your
consent to run a program on every event.

## The mapping

| codex's hook | becomes | detail | message |
| --- | --- | --- | --- |
| `SessionStart` | `session-started` | | |
| `UserPromptSubmit` | `user-sent-prompt` | | the prompt |
| `PostToolUse` | `agent-progressed` | | |
| `PermissionRequest` | `blocked-on-human` | `permission-prompt` | the command awaiting approval |
| `Stop` | `turn-finished` | | the answer |
| `Interrupt` | `turn-interrupted` | `interrupted` | |
| `SessionEnd` | `session-ended` | why | |

Seven of codex's twelve. The five that are not subscribed are not oversights:

- **`PreToolUse`.** `PostToolUse` already says the session is working, and
  subscribing to both would put a "working" write in flight at the moment a
  `PermissionRequest` is writing "blocked on you" — **49 milliseconds apart** on
  a real run, which is well inside the window where two short-lived processes
  can reach the lock out of order. The one that arrives second wins, and losing
  that race paints over a permission prompt.
- **`SubagentStart` and `SubagentStop`.** Neither fired once in the recorded
  traffic, so there is nothing to map them against — and the one agent where
  this hook *was* observed turned out to fire it for its own turn wrapper, which
  had to be discarded. An unverified mapping of a hook that can knock a finished
  turn back to `working` is not worth the guess.
- **`PreCompact` and `PostCompact`.** A compaction mid-turn is already
  `working`, so they add nothing; a compaction you asked for from an idle
  session would leave `PostCompact`'s "working" sitting there with nothing
  running behind it, because codex fires nothing afterwards. Measured:
  `PreCompact`, `PostCompact` 19s later, then silence for two minutes.

## The name

Every one of those reports also carries what codex calls the thread, which is
how a session gets a name at all since core stopped having a `name` command
(plan.md D-60). It is not in the hook payload, so `threadname.go` reads the
index codex keeps beside its rollouts:

```
~/.codex/session_index.jsonl
{"id":"01a0a71a-…","thread_name":"Run exact shell command","updated_at":"2026-09-15T22:05:33.009492Z"}
```

The name itself lives in `threads.name` in `~/.codex/state_5.sqlite`, and this
file is an exact projection of it — same ids, same strings, every one. The
projection is what gets read: a hook on the agent's critical path has no
business opening somebody else's sqlite, in WAL mode, while they are writing to
it, and a driver is a dependency an adapter should not need in order to report
a name.

Driving a real codex and watching both, the timing comes out like this:

| when | what appears |
| --- | --- |
| prompt submitted | the thread's row |
| +0.8s | `title` — the first user message, verbatim |
| **+4.1s** | **`name` — generated, and the index line written** |

Four seconds in, mid-turn, so every hook after it carries the name. The line is
appended at that moment and never rewritten, which is what makes the end of the
file the right place to start: entries are in the order threads were *named*,
newest last.

So the search runs **backwards from the end, 64 KiB at a time** — about six
hundred entries per read, and the first one almost always ends it. Almost
always is not always, and the gap matters: a session you leave open all week
keeps its place in the file while every thread named since is appended below
it, so the one session whose name is most worth having is the one that drifts
out of reach. A read that does not find the id therefore moves to the read
before it, up to a few megabytes back. The first read is a shortcut, not the
extent of the search, and a line split across two reads is put back together
rather than skipped.

Two threads it will not name, both correctly: a headless `codex exec` run,
whose row is written with a null name and never reaches this file, and a thread
whose first turn is not four seconds old yet. An empty name means "leave the
stored one alone", so both show the working directory exactly as they did
before this existed.

## What codex taught the design

Three things, all from replaying the 90 real payloads this machine recorded
(`TestReplayOfRealCaptures` does it on every `go test`):

- **`Interrupt` needed a ninth event.** `Stop` and `Interrupt` are mutually
  exclusive paths in codex: an interrupted turn never reaches the `Stop` call,
  so ignoring it leaves a session reading `working` for as long as nobody
  touches it. Mapping it to `turn-finished` would promise an answer that does
  not exist and make a notifier announce "your agent finished" a tenth of a
  second after you pressed Esc; mapping it to `turn-failed` would be loud about
  something you chose. `turn-interrupted` → `idle` is the honest one, and it is
  now in core for every agent (plan.md D-36).
- **A permission request clears itself.** Approving runs the tool and the
  `PostToolUse` behind it says working again; interrupting it resolves it too.
  Nothing has to remember that a prompt was outstanding.
- **The event arrives in the payload, not in the arguments.** Codex writes
  `hook_event_name` into every one, so a single command string serves all seven
  subscriptions and nothing can disagree with the key it was registered under.
  Claude's equivalent names one per line and can.

## Two things this program must never do

Both matter more for codex than for any agent before it:

- **Never exit non-zero.** Codex reads exit code 2 as "block this operation".
- **Never write to stdout.** `PermissionRequest` reads a handler's stdout as a
  verdict on the tool it is asking about, and this program has no business
  having one. Saying nothing is how a handler declines to decide.

A notifier that denies your agent a tool call, or silently approves one, is far
worse than a notifier that does not notify.

## The tokens

What the thread has spent is read out of codex's own rollout on every hook, in
`usage.go`, and reported beside the name. Core adds up the responses it has not
counted before, so the same read on twenty hooks in a row costs nothing.

Codex writes the accounting twice: a `token_usage_record` per response, and an
`event_msg` of type `token_count` beside it. Only the first is read. The
per-response `usage` is what is reported, and codex's own running totals in
`thread_token_usage` are deliberately not — core adds for every agent or for
none, and a rule that trusted one agent's total would have to decide, per agent,
what a resumed thread's total means.

One shape has to be converted: what codex calls `input_tokens` includes what it
read from the cache, and core's four counters do not overlap, so the cached
share is subtracted out rather than counted twice.

**The `token_count` line is no longer read at all**, and half this file went with
it. It carried the two things codex alone could answer — `model_context_window`,
the size of the window, and the account's `rate_limits` — and being the only
agent that can answer is exactly why they went (D-76): a percentage one agent
of two can show is a branch every display has to write and one of them draws.
Both are still in the rollout, in the same place, if a second agent ever starts
reporting them.

The **model** comes off the hook payload, which carries it on every hook. The
**branch** is core's,
read from the checkout at the thread's directory — codex records `git.branch`
once, in `session_meta` at the head of the rollout, which is the branch as of
session start and stays that way however many times you check out something
else.

## Building

Go 1.26. Core is not published yet, so `go.mod` has a `replace` pointing beside
this repository; that line comes out when core is.

If your shell exported `GOROOT` from an outer context it overrides the toolchain
pinned in `.tool-versions` — `env -u GOROOT go build ./...` is the fix.
