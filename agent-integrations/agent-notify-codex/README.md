<div align="center">
  <h1>agent-notify-codex</h1>
  <p><strong>Codex CLI's agent-integration for <a href="../../agent-notify">agent-notify</a></strong></p>
  <p>
    It turns codex's hooks into agent-notify's nine events<br>
    It reads the thread's name and its tokens out of codex's own files<br>
    It does nothing else
  </p>
</div>

---

- [What it is](#what-it-is)
- [Installation](#installation)
  - [Build it](#build-it)
  - [Put it on PATH](#put-it-on-path)
  - [Register it with codex](#register-it-with-codex)
  - [Trust it — the one step that is yours](#trust-it--the-one-step-that-is-yours)
  - [Tell core what a codex process looks like](#tell-core-what-a-codex-process-looks-like)
  - [Check it](#check-it)
  - [Uninstalling](#uninstalling)
- [Configuration](#configuration)
  - [`install`'s own flags](#installs-own-flags)
  - [Codex's `config.toml`](#codexs-configtoml)
  - [agent-notify's `config.toml`](#agent-notifys-configtoml)
  - [Environment](#environment)
  - [Files this program reads](#files-this-program-reads)
- [What it reports](#what-it-reports)
  - [The mapping](#the-mapping)
  - [The name](#the-name)
  - [The tokens](#the-tokens)
  - [What it does not report](#what-it-does-not-report)
- [Two things this program must never do](#two-things-this-program-must-never-do)
- [What codex taught the design](#what-codex-taught-the-design)
- [What is unfinished](#what-is-unfinished)
- [Building and testing](#building-and-testing)

---

## What it is

An agent-integration is the half of agent-notify that speaks one agent's
language. This one speaks codex's: it is registered as a hook in codex's
configuration, codex runs it with a JSON payload on stdin whenever something
happens in a thread, and it writes one record into agent-notify's store saying
what that thread is now doing.

The result is a codex thread sitting on the same bar, in the same vocabulary, as
every Claude session beside it:

```
$ agent-notify list
30  Rework the picker keybinding  blocked-on-you/permission-prompt  1m12s  needs approval: rm -rf target
20  agent-notify-codex            working                           4s     Reading usage.go
10  lenny-load                    finished-a-turn                   2h15m  No. Tre cose nel worktree…
```

Everything peculiar to codex lives in `mapping.go`, `threadname.go` and
`usage.go`. Everything after them is `hook.Record`, which is identical for every
agent. Core knows nothing about this program and never will — it does not know
where codex keeps its configuration, what codex calls its hooks, or that
`session_index.jsonl` exists.

It is a one-shot process. Codex starts it, it reads one payload, it writes one
record, it exits — there is no daemon here and nothing to keep running.

## Installation

Four steps, and the fourth one codex insists on doing with you rather than for
you.

### Build it

agent-notify is a monorepo: core, the agent-integrations and the
tool-integrations are one git repository and one clone. Nothing is published to
a remote yet, so this module's `go.mod` carries a `replace` pointing at core's
directory beside it — which means a clone of the whole repository builds and a
copy of this directory alone does not.

```sh
git clone git@github.com:lassoColombo/agent-notify.git
cd agent-notify/agent-integrations/agent-notify-codex
go build ./...
```

Go 1.26 or newer; `.tool-versions` pins 1.26.2. If your shell exported `GOROOT`
from an outer context it overrides that pin, and `env -u GOROOT go build ./...`
is the fix.

### Put it on PATH

The program has to be on your PATH under exactly the name
**`agent-notify-codex`**, because that is the name `agent-notify install codex`
looks for — core's `install` is a dispatcher that execs `agent-notify-<name>
install` and knows nothing else about you.

```sh
go build -o /opt/homebrew/bin/agent-notify-codex .
```

Install core the same way while you are here, if you have not:

```sh
cd ../../agent-notify && go build -o /opt/homebrew/bin/agent-notify .
```

### Register it with codex

```sh
agent-notify install codex          # appends to ~/.codex/config.toml
agent-notify install codex --print  # show what it would add, change nothing
```

`agent-notify install codex` and `agent-notify-codex install` are the same
command: the first execs the second, replacing the process, so the flags, the
output and the exit code are this program's.

What it writes is seven hook blocks, all running the same absolute path — the
path of the binary you ran it from, resolved at that moment, because a hook's
PATH is not your shell's PATH:

```toml
# agent-notify: tells agent-notify what this codex session is doing.
# It never writes to stdout and never exits non-zero, so it cannot
# block a tool, decide a permission request, or fail a turn.

[[hooks.SessionStart]]
[[hooks.SessionStart.hooks]]
type = "command"
command = "/opt/homebrew/bin/agent-notify-codex"

[[hooks.UserPromptSubmit]]
[[hooks.UserPromptSubmit.hooks]]
type = "command"
command = "/opt/homebrew/bin/agent-notify-codex"

# … PostToolUse, PermissionRequest, Stop, Interrupt, SessionEnd, the same
```

It **appends**. Your model, your sandbox policy, your MCP servers and your
comments survive, because round-tripping a file a person wrote through a TOML
encoder to add seven blocks would hand it back stripped and reordered.
Appending at the end is safe for one reason worth stating: a table header ends
the table before it, and nothing follows what we add.

Three behaviours follow from that, and all three are deliberate:

- It **refuses a file it cannot parse** and changes nothing. Appending to a
  broken file turns one problem into two, and the second one looks like ours.
- It is **idempotent**: a file that already names `agent-notify-codex` anywhere
  is left alone, and it says so. There is no merge and no update of a stale
  path — if you moved the binary, edit the file.
- **Hooks somebody else put there are left alone.** Codex allows several
  handlers per event, so your own logger and this program coexist.

### Trust it — the one step that is yours

Codex asks you to trust a hook the first time it runs one, and records the
answer in that same `config.toml`:

```toml
[hooks.state."/Users/you/.codex/config.toml:post_tool_use:0:0"]
trusted_hash = "sha256:e2c77aeb8bbf2d74b5debf25f4950ba0b165b8275841616f11befbf77793dce1"
```

This installer deliberately does not write those entries. The hash is codex's
own, and writing one would be forging your consent to run a program on every
event. So start a codex thread, answer the prompt, and the hooks begin firing.

### Tell core what a codex process looks like

One line in agent-notify's own configuration, and without it the session is
recorded but never judged dead:

```toml
# ~/.config/agent-notify/config.toml
[agent.codex]
binary = "codex"
```

On every hook, core walks the process ancestry from this program upwards
looking for a process that matches `binary` — the short name the kernel
reports, the full path, or the basename of that path, so
`binary = "/opt/homebrew/bin/codex"` works too when you have two codexes. What
it finds is the PID and start time that let the session-watcher notice when a
thread's codex is gone. With no `[agent.codex]` table nothing matches, the hook
logs a warning, and the record carries no process.

This is also the table that makes `agent-notify doctor` stop listing
`agent-notify-codex` as "on your PATH and not mentioned here".

### Check it

```sh
agent-notify-codex install --print   # what would be added
agent-notify doctor                  # where everything resolves to, and what is wrong
codex                                # start a thread, say something
agent-notify list                    # it should be there, working
```

`doctor` deliberately does not check whether an agent's hooks are installed —
it says so in its own output — so the honest check is a real thread and
`agent-notify list`. When a thread does not appear, the log is the place to
look: `~/Library/Application Support/agent-notify/agent-notify.log` on macOS,
`$XDG_STATE_HOME/agent-notify/agent-notify.log` otherwise. Everything a hook
cannot do goes there, because nothing a hook does may reach the agent.

### Uninstalling

Delete the blocks from `~/.codex/config.toml` by hand, and the `[hooks.state]`
entries beside them if you want codex to forget it trusted the program. There
is no `uninstall` subcommand: the same reasoning that makes `install` append
rather than rewrite makes deleting your lines from your file your business.

## Configuration

There are two files, and this program's own knowledge is in neither of them.
Codex's `config.toml` says that the hooks exist; agent-notify's `config.toml`
says what a codex process is called. Nothing configures the mapping, the
events, the field names or where codex keeps its rollouts — those are facts
about codex that the program already holds, and a second copy in a file you
edit would be a copy that can disagree with the first.

In particular: **an agent-integration has no `[integration.codex]` table and no
`settings`.** That table is for the long-lived tool-integrations core
supervises. This program is started by codex, not by core.

### `install`'s own flags

```
agent-notify-codex install [--print] [--config PATH]
```

| Flag | Type | Default | What it does |
| --- | --- | --- | --- |
| `--print` | switch | off | Write the hook block to stdout and change nothing. Everything else — reading, parsing, appending — is skipped. |
| `--config` | path | `$CODEX_HOME/config.toml`, else `~/.codex/config.toml` | The file to append to. It is created, with its parent directory at mode 0700, when it does not exist. |

Run with any other argument, the program prints its usage to stderr and exits
1. Run with no arguments at all, it is a hook: it reads one payload on stdin.

### Codex's `config.toml`

`~/.codex/config.toml`, or `$CODEX_HOME/config.toml` when that variable is set.
What `install` puts there is described above; what matters if you edit it by
hand is the shape:

| Key | Type | Value | What it changes |
| --- | --- | --- | --- |
| `[[hooks.<Event>]]` | array of tables | one entry per handler | Which of codex's twelve events call out at all. Seven are subscribed; see [the mapping](#the-mapping). |
| `[[hooks.<Event>.hooks]]` | array of tables | one entry per command | The handlers for that event, in order. Several may coexist. |
| `type` | string | `"command"` | The only handler kind this uses. |
| `command` | string | absolute path | The program to run. An absolute path, because a hook's PATH is not your shell's PATH. |
| `[hooks.state."<file>:<event>:<i>:<j>"]` | table | `trusted_hash` | Codex's record of your consent. **Codex writes this, never `install`.** |

The same command string serves all seven subscriptions, and that is not
laziness: codex writes `hook_event_name` into every payload, so the program
takes the event from the body rather than from its arguments and no argument
can disagree with the key it was registered under.

### agent-notify's `config.toml`

`~/.config/agent-notify/config.toml`, or `$XDG_CONFIG_HOME/agent-notify/config.toml`,
or `$AGENT_NOTIFY_ROOT/config.toml`. There is no file by default and running
without one is supported — but the one key below is worth having.

| Key | Type | Default | What it changes |
| --- | --- | --- | --- |
| `agent.codex.binary` | string | none | What to look for when climbing this hook's ancestry to find codex itself. Matched against the kernel's command name, the executable path, or that path's basename. Unset means no process is recorded and liveness has nothing to judge. |

Everything else in that file — `keep-ended-sessions`, the integration tables,
`container.order` — belongs to core and to the tools, and this program neither
reads nor writes any of it. Core's [README](../../agent-notify/README.md) has
the rest.

### Environment

| Variable | Read by | Effect |
| --- | --- | --- |
| `CODEX_HOME` | this program | Relocates the whole of codex's home. It decides where `install` writes (`$CODEX_HOME/config.toml`) and where the thread name is read from (`$CODEX_HOME/session_index.jsonl`). Unset ⇒ `~/.codex`. |
| `AGENT_NOTIFY_ROOT` | core, on the hook path | Moves agent-notify's state directory, runtime directory and configuration file beneath one root. One variable, so that running an isolated instance is one step. |
| `XDG_CONFIG_HOME` | core | Where `agent-notify/config.toml` is looked for, when `AGENT_NOTIFY_ROOT` is unset. |
| `HOME` | both | The fallback for both of the above. |
| `GOROOT` | the Go toolchain | Nothing at run time; at build time an inherited value overrides `.tool-versions`. |

A hook inherits codex's environment, not your shell's, so a variable you export
in a running shell reaches nothing already started.

### Files this program reads

| Path | When | Why |
| --- | --- | --- |
| stdin | every hook | The payload: `hook_event_name`, `session_id`, `cwd`, `model`, `transcript_path`, and whatever else that event carries. |
| `~/.codex/session_index.jsonl` | every hook | The thread's name. See [The name](#the-name). |
| the payload's `transcript_path` | every hook | The thread's rollout, for what it has spent. Codex calls it a transcript; it is the rollout under `~/.codex/sessions/<yyyy>/<mm>/<dd>/rollout-<timestamp>-<thread-id>.jsonl`. |

Both of the last two are read backwards from the end, 64 KiB at a time, and
both are bounded at 4 MiB of reading. Nothing here is fatal: a file that is
missing, truncated, half-written or enormous yields nothing at all, which core
reads as "leave what is stored alone".

## What it reports

One `session.Report` per hook, and these are its fields:

| Field | Where it comes from |
| --- | --- |
| `Key.Agent` | the constant `codex`, which is also what your `[agent.codex]` table is keyed on |
| `Key.SessionID` | the payload's `session_id`, codex's thread id. No id, no report — nothing can be written about a session nobody can name |
| `Event` | the payload's `hook_event_name`, through [the mapping](#the-mapping) |
| `Detail` | the mapping too: `permission-prompt`, `interrupted`, `exited`, `superseded` |
| `Message` | the prompt, the last assistant message, or the sentence describing what needs approving. Blank text is reported as nothing rather than as an empty string, because nothing leaves the previous message in place and an empty string erases it |
| `Name` | `~/.codex/session_index.jsonl`. See [The name](#the-name) |
| `Cwd` | the payload's `cwd` |
| `Model` | the payload's `model`, in codex's own spelling, on every hook |
| `Spent` | the rollout's `token_usage_record` lines. See [The tokens](#the-tokens) |

The name, the model and the spending ride along on every hook rather than on
some special one. The record is being written anyway, so they cost no extra
write, and a thread codex renames mid-session lands on the next tool call
instead of waiting for a hook that happens to be the right kind.

### The mapping

| codex's hook | becomes | detail | message |
| --- | --- | --- | --- |
| `SessionStart` | `session-started` | | |
| `UserPromptSubmit` | `user-sent-prompt` | | the prompt |
| `PostToolUse` | `agent-progressed` | | |
| `PermissionRequest` | `blocked-on-human` | `permission-prompt` | the command awaiting approval |
| `Stop` | `turn-finished` | | the answer |
| `Interrupt` | `turn-interrupted` | `interrupted` | |
| `SessionEnd` | `session-ended` | `exited`, or `superseded` for `clear`/`resume` | |

Seven of codex's twelve. The five that are not subscribed are not oversights:

- **`PreToolUse`.** `PostToolUse` already says the session is working, and
  subscribing to both would put a "working" write in flight at the moment a
  `PermissionRequest` is writing "blocked on you" — **49 milliseconds apart** on
  a real run, which is well inside the window where two short-lived processes
  can reach the lock out of order. The one that arrives second wins, and losing
  that race paints over a permission prompt.
- **`SubagentStart` and `SubagentStop`.** Neither fired once in the recorded
  traffic, so there is nothing to map them against — and the one agent where
  this hook *was* observed turned out to fire it for its own turn wrapper,
  which had to be discarded. An unverified mapping of a hook that can knock a
  finished turn back to `working` is not worth the guess; a subagent's own tool
  calls already mark the parent working.
- **`PreCompact` and `PostCompact`.** A compaction mid-turn is already
  `working`, so they add nothing; a compaction you asked for from an idle
  session would leave `PostCompact`'s "working" sitting there with nothing
  running behind it, because codex fires nothing afterwards. Measured:
  `PreCompact`, `PostCompact` 19s later, then silence for two minutes. A state
  that is right for ten seconds and wrong for ten minutes is worse than no
  state at all.

`PermissionRequest` is the one hook whose message has to be composed rather
than copied. Claude hands over a finished sentence; codex hands over a tool name
and that tool's own arguments, so the program takes `command` if there is one,
`description` if there is not, and the bare tool name otherwise. `command`
beats `description` because the three real permission prompts recorded here all
carried "Do you want to allow writing the requested file outside the
workspace?" as their description — the same sentence whichever file it was —
while the command named the file.

### The name

Core stopped having a `name` command, so a session gets a name only if its
agent has one to give. Codex names its own threads, but not in the hook
payload, so `threadname.go` reads the index codex keeps beside its rollouts:

```
~/.codex/session_index.jsonl
{"id":"01a0a71a-…","thread_name":"Run exact shell command","updated_at":"2026-09-15T22:05:33.009492Z"}
```

The name itself lives in `threads.name` in `~/.codex/state_5.sqlite`, and this
file is an exact projection of it — same ids, same strings, every one. The
projection is what gets read, for three reasons that point the same way: the
database's filename carries its schema generation (`state_5`, beside `logs_2`
and `memories_1`) and so is guaranteed to move; `name` is one of the columns
migrations bolted on late; and reading it means a sqlite driver in every
adapter binary and a WAL-mode open of somebody else's database while they are
writing to it. A two-field text file with a stable name is the better door to
the same string.

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
before it, up to 4 MiB back. The first read is a shortcut, not the extent of
the search, and a line split across two reads is put back together rather than
skipped.

Two threads it will not name, both correctly: a headless `codex exec` run,
whose row is written with a null name and never reaches this file, and a thread
whose first turn is not four seconds old yet. An empty name means "leave the
stored one alone", so both show the working directory exactly as they did
before this existed.

### The tokens

What the thread has spent is read out of codex's own rollout on every hook, in
`usage.go`, and reported beside the name:

```
~/.codex/sessions/2026/09/19/rollout-<timestamp>-<thread-id>.jsonl
{"type":"token_usage_record","payload":{"response_id":"resp_0d6a…",
  "usage":{"input_tokens":14335,"cached_input_tokens":14080,
    "cache_write_input_tokens":0,"output_tokens":30,
    "reasoning_output_tokens":0,"total_tokens":14365},
  "thread_token_usage":{…}}}
```

The read is the same shape as the name's: backwards from the end, 64 KiB at a
time, stopping at eight responses or 4 MiB. Eight is generous — a hook fires on
every tool call, so one new response since the last read is the ordinary case —
and the overlap is free, because core counts only the responses it has not
counted before. The same read on twenty hooks in a row costs nothing.

Three decisions are worth naming.

**Codex does the adding itself**, in `thread_token_usage` and
`total_token_usage`, and neither is read. What is reported is the per-response
`usage` beside them, because core adds for every agent or for none: a rule that
trusted one agent's running total would have to decide, per agent, what a
resumed thread's total means.

**One shape has to be converted.** What codex calls `input_tokens` includes what
it read from the cache, and core's four counters do not overlap, so the cached
share is subtracted out rather than counted twice. Reasoning tokens are left as
a share of output, which is what core's `reasoning` means.

**The `token_count` line is no longer read at all**, and half this file went
with it. Codex writes its accounting twice — a `token_usage_record` per
response and an `event_msg` of type `token_count` beside it — and the second
carried the two things codex alone could answer: `model_context_window`, the
size of the window, and the account's `rate_limits`. Being the only agent that
can answer is exactly why they went: a percentage one agent of two can show is
a branch every display has to write and one of them draws. Both are still in
the rollout, in the same place, if a second agent ever starts reporting them.

### What it does not report

- **The branch.** Core reads it from whatever is checked out at `cwd`, on every
  hook, for every agent. Codex records `git.branch` once, in `session_meta` at
  the head of the rollout, which is the branch as of session start and stays
  that way however many times you check out something else.
- **`source` on `SessionStart`** — `startup` or `resume` in everything observed.
  Claude's `SessionStart` carries one too and sets no detail from it, and two
  agents must be indistinguishable in kind on one bar: a codex session reading
  `idle/startup` beside a claude one reading `idle` would be a difference with
  nothing behind it.
- **Where the session is.** Which pane, which window, which tab is read by the
  `capture-environment` of whatever tool-integrations you have enabled, as
  children of this hook. This program does not answer that subcommand and does
  not need to.
- **`turn-failed`.** Codex has no hook for it. The event exists in core and
  another agent may use it; nothing here produces it.
- **The context window and the rate limits**, as above.

## Two things this program must never do

Both matter more for codex than for any agent before it:

- **Never exit non-zero.** Codex reads exit code 2 as "block this operation". A
  payload that does not even parse is not an error here: codex sends a different
  shape per hook and adds fields between releases, so the decode error is
  discarded and whatever did decode is used.
- **Never write to stdout.** `PermissionRequest` is synchronous — codex reads a
  handler's stdout as a verdict on the tool it is asking about, and waits.
  Saying nothing is how a handler declines to decide.

A notifier that denies your agent a tool call, or silently approves one, is far
worse than a notifier that does not notify. Everything that goes wrong goes to
agent-notify's log instead.

The same rule is why nothing on the hook path may block. Every file read here
is bounded, and `hook.Record` bounds the one thing it spawns.

## What codex taught the design

Three things, all from replaying the 90 real payloads this machine recorded.
`TestReplayOfRealCaptures` does it on every `go test`, and skips itself
everywhere but the machine that made them.

- **`Interrupt` needed a ninth event.** `Stop` and `Interrupt` are mutually
  exclusive paths in codex: an interrupted turn never reaches the `Stop` call,
  so ignoring it leaves a session reading `working` for as long as nobody
  touches it — measured, an interrupt then silence then `SessionEnd` two
  minutes later. Mapping it to `turn-finished` would promise an answer that does
  not exist and make a notifier announce "your agent finished" a tenth of a
  second after you pressed Esc; mapping it to `turn-failed` would be loud about
  something you chose. `turn-interrupted` → `idle` is the honest one, and it is
  now in core for every agent.
- **A permission request clears itself.** Approving runs the tool and the
  `PostToolUse` behind it says working again; interrupting it resolves it too.
  Nothing has to remember that a prompt was outstanding.
- **The event arrives in the payload, not in the arguments.** Codex writes
  `hook_event_name` into every one, so a single command string serves all seven
  subscriptions and nothing can disagree with the key it was registered under.
  Claude's equivalent names one per line and can.

## What is unfinished

- **Nothing is published.** Core's `go.mod` coordinates are not on a remote, so
  the `replace` line in this module's `go.mod` is load-bearing and a clone of
  this directory alone does not compile. That line comes out when core is
  published, and the version every integration handshakes against is still the
  constant `0.0.0-dev` rather than something the build stamps.
- **There is no `uninstall`**, and `install` will not update a stale path it
  finds. Both are edits to a file you own, and it says what to change rather
  than changing it.
- **`SubagentStart`, `SubagentStop`, `PreCompact` and `PostCompact` are
  unmapped** on the evidence described above rather than on principle. If codex
  starts firing subagent hooks for real work, that decision deserves revisiting
  against a new recording.
- **`turn-failed` is never produced**, because codex offers nothing to produce
  it from. A thread that dies without a `SessionEnd` is noticed by core's
  liveness check, not by this program.
- **Linux is untested.** Nothing here is macOS-specific — it is file reads and
  a JSON decode — but the process ancestry core walks on the same hook has only
  been verified on macOS.
- **The replay corpus is one machine's.** Ninety payloads from
  `~/.codex/hooks/events.d`, recorded by a shell hook that is not part of this
  repository, so `TestReplayOfRealCaptures` skips everywhere else and the
  fixtures in `mapping_test.go` are what actually guards the mapping in CI.

## Building and testing

```sh
go build ./...
go test ./...

# if your shell exported GOROOT from an outer context
env -u GOROOT go test ./...
```

The test files divide the way the source does: `mapping_test.go` on
the pure translation, `threadname_test.go` and `usage_test.go` on the two
backwards file reads with their fragments and truncations, `install_test.go` on
appending to a config file that already has somebody else's hooks in it, and
`replay_check_test.go` on the real corpus when it is there.
