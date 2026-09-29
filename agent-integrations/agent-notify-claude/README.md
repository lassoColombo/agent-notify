<div align="center">
  <h1>agent-notify-claude</h1>
  <p><strong>Claude Code's agent-integration for <a href="../../agent-notify">agent-notify</a></strong></p>
  <p>
    Nine of Claude's hooks, translated into agent-notify's event vocabulary<br>
    The session's name, directory, model and token spend, read out of Claude's own files<br>
    One <code>install</code> that writes itself into Claude's settings and leaves everything else alone
  </p>
</div>

---

- [What this is](#what-this-is)
- [What it reports](#what-it-reports)
  - [What it deliberately does not report](#what-it-deliberately-does-not-report)
- [Installation](#installation)
  - [1. Clone the monorepo and build](#1-clone-the-monorepo-and-build)
  - [2. Register it with Claude Code](#2-register-it-with-claude-code)
  - [3. Tell core what a Claude process looks like](#3-tell-core-what-a-claude-process-looks-like)
  - [4. Check it worked](#4-check-it-worked)
  - [Moving the binary, and uninstalling](#moving-the-binary-and-uninstalling)
- [Configuration](#configuration)
  - [Claude's settings.json](#claudes-settingsjson)
  - [agent-notify's config.toml](#agent-notifys-configtoml)
  - [Environment variables](#environment-variables)
  - [The files it reads](#the-files-it-reads)
- [The mapping](#the-mapping)
  - [Four rows decided by replaying real payloads](#four-rows-decided-by-replaying-real-payloads)
- [The name and the directory](#the-name-and-the-directory)
- [The tokens and the model](#the-tokens-and-the-model)
- [Command line](#command-line)
- [Building and testing](#building-and-testing)
- [What is unfinished](#what-is-unfinished)

---

## What this is

Claude Code fires a hook, this program translates it, and agent-notify's core
writes the record that every display reads:

```
claude ── SessionStart ──▶ agent-notify-claude ── hook.Record(Report) ──▶ core ──▶ your bar, your tabs,
                                                                                  your notifications
```

That is the whole of it. Everything peculiar to Claude lives in `mapping.go`;
everything after it is `hook.Record`, which is identical for every agent. Core
knows nothing about this program and never will — it does not know where Claude
keeps its settings, what its hooks are called, or what a `SubagentStop` is, and
[plan.md](../../agent-notify/plan.md) R10 says it must not. Claude's own
`settings.json` names this binary, so it works the moment it exists, and
`install` is what writes that block.

The four things it is responsible for, and they are the only four:

- **Translating**, from Claude's hook vocabulary into the nine events. It decides
  *what happened* and *the detail*, never what state the session is now in —
  that answer depends on the previous state, which a process that exits in
  milliseconds does not have.
- **Naming**, by reading the best name Claude has for the session out of two of
  Claude's own files, or reporting nothing when Claude has none.
- **Counting**, by reading the tail of Claude's transcript for the responses core
  has not added up yet.
- **Installing itself**, into Claude's `settings.json`, without losing anything a
  person put there.

It never writes to the store directly, never opens the socket, never learns the
protocol. `hook.Record(report)` is the entire interface, and it returns no error
on purpose: an agent interprets its hooks, so there is nothing useful a caller
could do with a failure except make things worse.

## What it reports

Every hook this program answers writes one `session.Report`, and every
report carries the whole of the following — not just the fields the event is
about. The record is being written anyway, so a name or a token count costs no
extra write, and a `/rename` halfway through a session lands on the next tool
call instead of waiting for a hook that happens to be special.

| Field | Where it comes from | Notes |
| --- | --- | --- |
| `key.agent` | this program | always `claude`; it is what your config matches on |
| `key.session_id` | the payload's `session_id` | no id, no report — nothing can be written about a session nobody can name |
| `key.host` | core | |
| `event` | the hook name, and sometimes `source` / `agent_type` / `notification_type` | one of the nine, see [the mapping](#the-mapping) |
| `detail` | this program | lowercase-kebab, by the shared convention of §A5.8 |
| `message` | `prompt`, `last_assistant_message`, `message` or `error_details` | blank is not a message: it is reported as nothing, which means "leave the stored one alone" |
| `name` | `~/.claude/sessions/<pid>.json`, then the transcript's `ai-title` | the best of three, or nothing at all |
| `cwd` | that same session file, falling back to the payload | the session's own directory, not the one the tool ran in |
| `model` | the newest assistant line of the transcript | the payload does not carry one |
| `spent` | every response in one read of the tail of the transcript | oldest first; core adds up what it has not counted |
| `process`, `captured_context` | core, by walking this hook's own ancestry | only a child of the agent can see them |
| `branch` | core, from the checkout at `cwd` | deliberately not reported here — see below |

A real record, as `agent-notify list --json` prints it, with the long fields cut:

```json
{
  "key": { "host": "COLOMBOSRNBX", "agent": "claude",
           "session_id": "099ccac2-c26e-4064-8bbc-c09019ef28e4" },
  "kernel": "finished-a-turn",
  "state_since": "2026-09-23T18:21:58.085016Z",
  "name": "lenny-load",
  "cwd": "/Users/colombos/projects/work/cybergon/lenny",
  "branch": "main",
  "model": "claude-opus-5",
  "message": "No. Tre cose nel worktree non sono mie, e sono tutte della …",
  "usage": {
    "input": 2948, "output": 1895427,
    "cache_read": 438096474, "cache_write": 5698697, "reasoning": 802279,
    "counted_through": "req_011CfLqQtmu6TjnZMKaWpzkL"
  },
  "process": { "pid": 23055, "started_at": "2026-09-23T18:20:11.192072Z" }
}
```

### What it deliberately does not report

Each of these was reported at some point, or looked like it should be, and each
is absent for a reason rather than because nobody got to it.

- **The placeholder name.** A session nobody has named is called
  `agent-notify-16` by Claude — the directory's basename and a counter — and it
  arrived looking exactly like a name somebody chose, so no display could tell
  the two apart. An empty name now means *nobody has named this*, and core does
  the guessing for every agent at once (§A7.4.2, D-75).
- **The first prompt.** It is always there and it says exactly what the session
  is about, and it is still not a name: it is prose, it would have to be cut to
  fit a tab, and reporting it would take back the one thing an empty name buys.
- **The branch.** Core reads it from the checkout at the session's directory,
  because Claude puts `gitBranch` on every transcript line and on a workspace
  that merely contains checkouts it says `"HEAD"` for a repository sitting on
  `main`.
- **How full the context window is.** The transcript cannot say how big the
  window is — a session on the 1M context records `"model":"claude-opus-5"`, the
  same string a 200k one records — so the level had no limit beside it and no
  display could turn it into a percentage. It went with `quota` and `agent_data`
  in D-76.
- **Subagent token spend.** Claude gives a subagent its own transcript, named in
  `SubagentStop`'s `agent_transcript_path`, and following it is not in this.
- **`PreToolUse`.** `PostToolUse` already says the session is working, and
  subscribing to both would double the number of processes Claude waits for on
  every tool call — 3,017 `PreToolUse` against 2,915 `PostToolUse` in two days of
  real use. A `working/running-<tool>` detail is what it would buy.
- **`session_name`, on the status line.** Claude hands it to the `statusLine`
  command alone, and that is a slot you own.

## Installation

### 1. Clone the monorepo and build

agent-notify is one repository with three directories — `agent-notify/` (core),
`agent-integrations/` and `tool-integrations/` — and nothing is published
anywhere yet. `make install` at the root builds every module into
`$(go env GOPATH)/bin`, which needs to be on your `PATH`: `agent-notify install
claude` finds this program there by name, and so does `agent-notify doctor`.

```sh
git clone git@github.com:lassoColombo/agent-notify.git ~/projects/agent-notify
cd ~/projects/agent-notify
make install
```

### 2. Register it with Claude Code

Claude runs a hook because `settings.json` names it, and `install` is what
writes that block. Either spelling does the same thing — core's runs this
program and hands it every option after the name:

```sh
agent-notify install claude          # core's front door
agent-notify-claude install          # the same program, called directly
```

Look before you leap: `--print` renders the file it would write, to stdout, and
changes nothing.

```sh
agent-notify install claude --print
```

```json
{
  "hooks": {
    "SessionStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "\"/Users/you/go/bin/agent-notify-claude\" SessionStart"
          }
        ]
      }
    ],
    "…": "one such entry for each of the nine hooks"
  },
  "model": "opus[1m]"
}
```

Then do it for real. It names every hook it touched, and the file it wrote:

```
$ agent-notify install claude
added SessionStart
added UserPromptSubmit
added PostToolUse
added SubagentStop
added PreCompact
added Stop
added StopFailure
added Notification
added SessionEnd
wrote /Users/you/.claude/settings.json
wrote /Users/you/.config/agent-notify/conf.d/claude.toml
```

```
$ agent-notify install claude
already installed in /Users/you/.claude/settings.json
```

It is idempotent, and it is conservative on purpose — a person's settings file
holds their own hooks, their model and their permissions, and losing any of that
to an installer would be unforgivable:

- Everything that is not ours is round-tripped through `json.RawMessage` and
  written back exactly as it came. Your `env` block, your `model`, your
  `permissions`, your own `SessionStart` hook and your `PreToolUse` matcher all
  survive verbatim.
- A settings file that does not parse as JSON is **refused**, with a message
  saying so, rather than overwritten.
- What was there is copied to `settings.json.before-agent-notify` (mode 0600)
  before anything is written, and the write itself goes to
  `settings.json.writing` and is renamed into place.
- An absent settings file is an empty one. Installing on a machine that has
  never configured Claude is the ordinary case.
- The path written is this binary's own absolute path, as `os.Executable()`
  reports it, quoted. The entries carry no `matcher`, so they fire for every
  tool.

Claude reads `settings.json` at startup, so **restart Claude Code**, or start a
new session, before expecting anything.

### 3. Core is told what a Claude process looks like

Core walks the hook's ancestry to find which process the session *is*, and
without a binary to match it has nothing to judge liveness by: a session whose
Claude has died would sit on your bar forever. The name of that process is
Claude's, so `install` files it, in `conf.d/claude.toml` beside your
`config.toml`:

```toml
[agent.claude]
binary = "claude"
```

The value is matched against three spellings of each ancestor — the short name
the kernel reports, the full path, and that path's basename. With two claudes
to tell apart, write `binary = "/opt/homebrew/bin/claude"` under
`[agent.claude]` in your own `config.toml`, which wins over the drop-in.

### 4. Check it worked

```sh
agent-notify doctor
```

```
config       ok    2 agent(s), 6 integration(s), keep-ended-sessions 168h0m0s
store        ok    3 live session(s), 74 ended and still resumable, 0 forgotten this run
liveness     ok    boot 027BA0B8-… — 3 running, 0 gone but not yet ended, 0 cannot tell
watcher      ok    pid 86777, version 0.0.0-dev, since 2026-09-22T22:15:40Z
```

`doctor` does **not** check whether Claude still runs the hooks; `install` is
safe to run again and says whether they are there. What it does check is that
every agent with sessions in the store has its `[agent.<name>]` table.

Then start a Claude session, type something, and watch:

```
$ agent-notify list
30  lenny-load         finished-a-turn  2h15m    No. Tre cose nel worktree non sono mie, e sono tutte della …
30  lenny-integration  finished-a-turn  2h12m    Quinto punto: **la granularità.** Cosa deve rappresentare u…
20  agent-notify-95    working          32s      Ten agents are running in the background, one per module — …
```

`agent-notify tail` shows it change as you use the session, which is the fastest
way to see the mapping below actually firing.

### Moving the binary, and uninstalling

Rebuilding in place needs nothing. **Moving** the binary needs `install` run
again: it recognises an entry that names `agent-notify-claude` at any path and
rewrites that path in place, so you get `updated the path for SessionStart`
rather than a second entry firing a binary that no longer exists.

`agent-notify uninstall claude` takes every entry naming this program out of
`settings.json`, a group left empty with it, and leaves everybody else's hooks
byte for byte; then it removes `conf.d/claude.toml`. The copy at
`settings.json.before-agent-notify` is rewritten by every install or uninstall
that changes something, so it is the state before the *most recent* one.

## Configuration

There are two files and a handful of environment variables, and they belong to
two different programs. Claude's `settings.json` says *when* this runs;
agent-notify's `config.toml` says what core does with what it reports.

### Claude's settings.json

Written by `install`, at `~/.claude/settings.json` unless you say otherwise.

| | |
| --- | --- |
| **Path** | `~/.claude/settings.json`, and `--settings PATH` overrides it |
| **Format** | Claude's own JSON; only the `hooks` key is touched |
| **Keys written** | `hooks.<HookEventName>`, one group per hook, each holding `{"type": "command", "command": "\"<path>\" <HookEventName>"}` |
| **Hooks subscribed** | `SessionStart`, `UserPromptSubmit`, `PostToolUse`, `SubagentStop`, `PreCompact`, `Stop`, `StopFailure`, `Notification`, `SessionEnd` |

Two flags, and that is the whole command line of `install`:

| Flag | Type | Default | What it changes |
| --- | --- | --- | --- |
| `--print` | switch | off | Renders the merged file to stdout and writes nothing, not even the backup. |
| `--settings` | path | `$CLAUDE_CONFIG_DIR/settings.json` when that is set, else `$HOME/.claude/settings.json` | Which file to merge into. A project-level `.claude/settings.json`, or a scratch file you want to inspect, is what this is for. |

The default follows `CLAUDE_CONFIG_DIR`, so a relocated configuration directory
needs no flag. `--settings` is for a file that is neither of those.

### agent-notify's config.toml

At `~/.config/agent-notify/config.toml`, or `$XDG_CONFIG_HOME/agent-notify/`.
There is no file by default and running without one is supported — but the
`[agent.claude]` table is the one thing this integration genuinely wants there.

| Key | Type | Default | What it changes |
| --- | --- | --- | --- |
| `agent.claude.binary` | string | `"claude"`, filed by `install` in `conf.d/claude.toml` | What core matches against the hook's process ancestry to decide which process the session is. Without it, nothing records the pid, liveness has nothing to judge, and a dead session is never noticed as dead. Accepts the kernel's short name, a full path, or that path's basename; a value in your `config.toml` wins. |
| `agent-notify-binary` | path | filed by `agent-notify install` in `conf.d/agent-notify.toml` | Where the `agent-notify` binary lives, for the one job that needs it: a hook that finds no session-watcher has to start one. It exists because a hook's `PATH` is not your shell's, and because inside `agent-notify-claude` "this executable" is the wrong answer. |
| `keep-ended-sessions` | duration, `"168h"` spelling | `"168h"` (7 days) | How long an ended session's record survives, so that resuming it is recognised as a return rather than a birth. Core's, not this program's, but it is what decides whether your resumed Claude session keeps its history. |

**There is no `[integration.claude]` table, and there is not meant to be.** That
table is for the programs core runs — displays and containers — and for
the tools that answer `capture-environment`. An agent-integration is neither: it
is `exec`ed by the agent, it exits in milliseconds, and core never starts it. The
act that installs it is writing Claude's hooks, not adding a table.

Nothing this integration *knows* belongs in that file either — which files Claude
writes, which hooks exist, which notification types want you. A key whose correct
value is a fact about a program is that program's business, and a second copy in
a file you edit is a copy that can disagree with the first, silently (D-57,
D-66).

### Environment variables

| Variable | Read by | Effect |
| --- | --- | --- |
| `CLAUDE_PID` | this program | Names `~/.claude/sessions/<pid>.json` directly. Claude sets it in every process it spawns, this hook included; the file is opened by name, the session id inside is checked against the payload's, and that is one open of one small file. Unset, the whole directory is scanned instead — a readdir and a few hundred bytes per live session. |
| `CLAUDE_CONFIG_DIR` | this program | Relocates the whole of `~/.claude`: both where the live-session files are looked for and, because `install` reads it too, which `settings.json` the hooks are written into. |
| `AGENT_NOTIFY_ROOT` | core, on the hook path | Moves the state directory, the runtime directory and the configuration file beneath one root. One variable, so that running an isolated instance is one step — useful for trying this out without touching the agent-notify you already run. |
| `XDG_CONFIG_HOME` | core | Where `agent-notify/config.toml` is looked for. |
| `XDG_STATE_HOME`, `XDG_RUNTIME_DIR` | core | Where the store and the sockets live, on platforms where they apply. `agent-notify doctor` prints what all of these resolved to. |
| `GOROOT` | the Go toolchain | Only at build time, and only as a hazard: exported from an outer context it overrides `.tool-versions`. `env -u GOROOT go build ./...` is the fix. |

The transcript is **not** located through any of these: its path arrives in the
payload, as `transcript_path`, and is used exactly as given.

### The files it reads

Three, on every hook, and none of them is written by this program.

| Path | Read for | When it is missing |
| --- | --- | --- |
| `~/.claude/sessions/<pid>.json` | `name`, `nameSource`, `cwd`, and the `sessionId` that proves the file is about this session | The name and directory are reported as nothing, and the payload's `cwd` is the fallback. Claude may simply be too old to keep one. |
| `~/.claude/projects/<slug>/<session-id>.jsonl`, the first 64 KiB | the `ai-title` line | No title, which today is the ordinary case. |
| the same transcript, the last 64 KiB and up to 4 MiB back | `requestId` and `usage` per response, and the model off the newest assistant line | Nothing is reported, and core leaves the stored totals alone. |

Nothing here can fail in a way that matters. Every one of these is a best-effort
read whose failure mode is *say less*, and core treats an empty field as "leave
the stored one alone".

## The mapping

| Claude's hook | becomes | detail | message |
| --- | --- | --- | --- |
| `SessionStart` (`startup`, `resume`, `clear`) | `session-started` | | |
| `SessionStart` (`compact`) | `agent-progressed` | | |
| `PreCompact` | `agent-progressed` | `compacting` | |
| `UserPromptSubmit` | `user-sent-prompt` | | the prompt |
| `PostToolUse` | `agent-progressed` | | |
| `SubagentStop` **with** an `agent_type` | `agent-progressed` | `subagent-finished` | |
| `SubagentStop` without one | *nothing* | | |
| `Stop` | `turn-finished` | | the answer |
| `StopFailure` | `turn-failed` | the error kind | the answer it died partway through, or `error_details` |
| `Notification` that wants you | `blocked-on-human` | the kind | the text |
| `Notification` that does not | *nothing* | | |
| `SessionEnd` (`clear`, `resume`) | `session-ended` | `superseded` | |
| `SessionEnd` (`other`, `prompt_input_exit`) | `session-ended` | `exited` | |

A hook this program was not written for — which includes every hook Claude adds
after this was written — is *nothing*, silently.

`superseded` is worth the row it takes: resuming a session produces a decoy
session with a fresh id that lives for about three seconds and then ends with
reason `resume`, and filing it as superseded is what lets the picker leave it out
of the sessions it offers.

### Four rows decided by replaying real payloads

The rig that records every hook payload this machine sees is how the questions
above were answered — 7,980 payloads at first, 23,984 by 2026-09-22 — and
`replay_check_test.go` runs the whole corpus back through the mapping and the
reducer. It skips itself everywhere but the machine that made the captures; the
fixtures in `mapping_test.go` are what it chose.

- **A `SubagentStop` with no `agent_type` is not a subagent.** 413 of 416 real
  ones carry an empty kind, and 245 of those fire within two seconds of a `Stop`:
  it is Claude's own turn wrapper. Treating it as progress knocks a correctly
  finished turn back to `working` and holds it there while you are the one being
  waited on, which is the exact failure this system exists to prevent. If the
  discriminator is ever wrong it is wrong in the safe direction — a missed
  `agent-progressed` costs nothing, because the parent's next `PostToolUse` says
  working anyway.
- **`SessionStart` is not one event.** A compaction keeps the session id and is
  the agent working mid-turn, not a session beginning. Splitting on `source` is
  this program's job, not core's (D-31).
- **`notification_type` is a reliable discriminator** and the message text does
  not need parsing: every one of the 64 real permission prompts carried
  `permission_prompt`, and all 87 idle nudges carried `idle_prompt`. The kinds
  that want you are `permission_prompt`, `elicitation_dialog`,
  `elicitation_url_dialog` and `agent_needs_input`; a kind nobody has heard of is
  ignored, because the kinds that do not want you outnumber the kinds that do,
  and a payload with no kind at all is let through, because that is what a
  permission prompt looked like before the field existed.
- **`StopFailure` carries `error`, not `error_type`.** This read `error_type` and
  `error_message` for as long as it existed, inherited from the prior Rust
  adapter and marked `[assumed]` because no payload carrying them had ever been
  seen. None ever was. [verified 2026-09-22, 2.1.267] against the one real
  `StopFailure` in the corpus and against the binary, which builds the hook input
  as `error`, `error_details` and the same `last_assistant_message` a `Stop`
  carries — so every `turn-failed` ever written arrived with an empty detail and
  no message.

## The name and the directory

Claude puts neither the session's name nor the session's own directory in the
hook payload — [verified 2026-09-19, 2.1.236] every hook input is built from the
same six fields — so `sessionname.go` reads the file Claude keeps for each live
session instead:

```
~/.claude/sessions/<pid>.json
{"pid":38514,"sessionId":"4a152fd4-…","cwd":"/…/agent-notify",
 "name":"agent-notify-96","nameSource":"derived","status":"busy",…}
```

Claude rewrites that file as the session runs, so a `/rename` halfway through is
picked up by the next hook without anything having to be told about it. The
`sessionId` inside is checked against the payload's regardless of how the file
was found: pids are reused, and `/clear` replaces the session inside a process
without the file changing its name.

**That file is not the only place a name can come from, and on its own it is
usually the worst one.** `nameSource` says which of four things produced the
name, and two of the four are not names at all: `derived` and `collision` are
both `<cwd-basename>-<counter>`. So `sessiontitle.go` reads the head of the
transcript as well, where Claude writes down what the session is about:

```
~/.claude/projects/<slug>/<session-id>.jsonl
{"type":"ai-title","aiTitle":"Fix Sketchybar display on external monitors",…}
```

and the report carries the best of three, or nothing at all:

| | where | what it looks like |
| --- | --- | --- |
| a person's `/rename` | `nameSource: user` | `an-picker` |
| Claude's title for the session | `ai-title` in the transcript | `Fix Sketchybar display on external monitors` |
| Claude's label for the session | `nameSource: auto` | `session-naming` |
| **nobody has named it** | — | **nothing is reported** |

**The title is the one you want and it is not being written.** [verified
2026-09-22, 2.1.267] 26 transcripts on this machine hold an `ai-title`; the
newest was written on 2026-08-27 under 2.1.231 and none of the 222 written since
has one. The generator is still in the binary — `querySource:
"generate_session_title"`, a JSON-schema call returning `{title}` — and the SDK
still documents a custom title as one that "skips automatic title generation",
so this is a path that stopped firing rather than a field that went away. It is
read anyway: it costs one open and one read of a file `usage.go` has just read
from the other end, and the day it comes back every unnamed session is better
named for nothing.

**The head is read once, forwards, and 64 KiB of it.** That is the opposite of
`usage.go`, and for the opposite reason: the title is written at the beginning
and never rewritten, while the numbers are only ever at the end. [verified
2026-09-22] it lands between 16.8 KiB and 23.7 KiB in 18 of the 26 transcripts
that have one. The other 8 sit between 316 KiB and 2.2 MB in, at no fixed
distance from either end, and they are deliberately not chased: the scan to reach
them is unbounded and would run on every hook of every untitled session, which
today is all of them.

**The `cwd` comes from that session file too, and not from the payload.** The
payload's is the directory the tool ran in, so it follows the agent's `cd` around
and changes several times a turn — and a display falling back to the last
component of it renames the session on every one of them. That is also why
`CwdChanged` is not subscribed: with the directory read from the file, the event
carries nothing.

One field has since half changed. [verified 2026-09-22, 2.1.267] Claude began
sending `session_title` in the payload on 2026-09-20, and every one of the 225
values seen is a `/rename` somebody typed — but it rides on `UserPromptSubmit`
and `SessionStart` alone, two of the nine hooks subscribed here, and it is not a
source for `nameSource`. The file answers on all nine, so the file is still what
is read; the payload agrees with it wherever both speak.

## The tokens and the model

What the session has spent is read out of Claude's own transcript on every hook,
in `usage.go`, and reported beside the name. Core adds up the responses it has
not counted before, so the same read on twenty hooks in a row costs nothing.

```
~/.claude/projects/<slug>/<session-id>.jsonl
{"type":"assistant","requestId":"req_011Cf…","message":{"model":"claude-opus-5","usage":{
  "input_tokens":2,"cache_read_input_tokens":75042,
  "cache_creation_input_tokens":1572,"output_tokens":366,
  "output_tokens_details":{"thinking_tokens":91}}}}
```

The transcript is the only place the numbers are. The hook payload carries none,
`~/.claude/sessions/<pid>.json` carries none, and the dollars and context
percentage on the status line are handed to the `statusLine` command alone.
There is no running total in the file either: [verified 2026-09-20] a whole
transcript grepped for a cost, a context size or a rate limit finds none of the
three, so what a session has spent is the sum of the per-response usage — which
is why core adds rather than this, a hook being a process that exits before the
record it is about to write exists.

The read works **backwards** from the end, 64 KiB at a time, stopping at eight
responses or 4 MiB, whichever comes first. Backwards, because the newest are the
ones core has not counted and a session an hour in is fifty megabytes. Bounded,
because a block can easily hold no responses at all — a tool result is a line
too, and reading a file writes a megabyte of one — and an unbounded scan sits on
a path the agent is waiting on. A window that no longer reaches the cursor
undercounts by whatever fell off the end, which is the failure worth having:
double-counting makes a total that grows on its own and no later read can bring
back down.

**One response is written as several lines**, one per content block, each
repeating the whole `usage`. In one 51 MB transcript that is 3,401 assistant
lines carrying 1,869 distinct `requestId`s, so a reader that adds up lines
reports 2.11× the output tokens actually spent. The `requestId` rides along with
every response for that reason, core counts each one once, and the duplicate
blocks are dropped here as well so that "eight responses" means eight responses
and not eight lines.

`thinking_tokens` is reported as `reasoning`, which core means as *a share of
output*, never an addend beside it. An assistant line with no usage at all is not
a response that was charged for, whatever else it is, and `isSidechain` lines are
skipped — a guard rather than a filter, since Claude gives a subagent its own
file.

**The model comes off the same lines**, and specifically off the newest one,
which is the only one still true after a `/model` halfway through a session. The
hook payload does not carry it, so this program's `model` field had been writing
an absent key into the record since the day it was added.

## Command line

```
agent-notify-claude <HookEventName>          # read a payload on stdin, report, exit 0
agent-notify-claude install [--print] [--settings PATH]
```

The first form is what Claude runs and is not meant for you, though it is
perfectly testable by hand:

```sh
echo '{"session_id":"abc","cwd":"/tmp","source":"startup"}' \
  | agent-notify-claude SessionStart
agent-notify list        # the session is there
```

**It never exits 2, whatever happens.** Claude reads exit code 2 from a hook as
"block this", feeds stderr back into the session and reads stdout into the
conversation — and a notifier that stops your agent working is far worse than one
that does not notify. So the hook path is silent on both streams and exits 0
from every path, including a payload that does not parse, a missing transcript,
and a store it cannot open. Diagnostics go to agent-notify's log, which
`agent-notify doctor` prints the path of (R2).

`install` is run by a person, so it may speak and it may fail: 0 when it wrote or
had nothing to write, 1 when it could not read or write the settings file or
could not find its own path, 2 for a flag it does not know. Called with no
arguments at all, the program prints its usage to stderr and exits 1.

## Building and testing

Go 1.26; `.tool-versions` pins `golang 1.26.2`.

```sh
cd ~/projects/agent-notify/agent-integrations/agent-notify-claude
env -u GOROOT go build ./...
env -u GOROOT go test ./...
```

The tests are worth knowing the shape of, because the design is arranged around
them. `Translate` is **pure** — what Claude knows about the session and what it
has spent are parameters rather than lookups — so the whole of the mapping is
tested with no store, no hook and no agent. `install_test.go` builds the real
binary and runs it against a settings file shaped like a real one, and is mostly
about what `install` leaves alone. `replay_check_test.go` replays every payload
this machine has captured under `~/.claude/hooks/events.d`, and skips itself
where there are none, which is everywhere else.

## What is unfinished

- **Nothing is published.** `go.mod` has `replace github.com/lassoColombo/agent-notify => ../../agent-notify`, so this builds only inside the monorepo. Milestone M18 is where that line comes out, and it is also where versioning, releases and "somebody who is not us installs it" live.
- **There is no `uninstall`.** Removing the hooks is a hand edit, or the backup file.
- **`ai-title` has not been written by Claude since 2026-08-27**, so in practice every session reported today is named by a `/rename` or by nothing at all, and core's `<directory>-<two characters of the session id>` is what you see.
- **Subagent token spend is not followed**, though `SubagentStop` names the transcript that holds it.
- **`turn-interrupted` is never reported.** Claude has no hook that says you pressed Esc, so one of the nine events goes unproduced here and an interrupted turn arrives as whatever Claude fires next. `context-changed` is unused too, for the happier reason that every report already carries the metadata it would have carried.
