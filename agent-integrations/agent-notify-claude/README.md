# agent-notify-claude

Claude Code's agent-integration for [agent-notify](../../agent-notify): it turns
Claude's hooks into agent-notify's eight events, and does nothing else.

Everything peculiar to Claude lives in `mapping.go`. Everything after it is
`hook.Record`, which is identical for every agent. Core knows nothing about this
program and never will.

## Installing

```
agent-notify-claude install            # writes ~/.claude/settings.json
agent-notify-claude install --print    # show what it would write, change nothing
```

It is idempotent, it keeps a copy of what it replaced, and it refuses to touch a
settings file it cannot parse. Your own hooks, your model and your permissions
are round-tripped untouched.

## The mapping

| Claude's hook | becomes | detail | message |
| --- | --- | --- | --- |
| `SessionStart` (startup, resume, clear) | `session-started` | | |
| `SessionStart` (compact) | `agent-progressed` | | |
| `PreCompact` | `agent-progressed` | `compacting` | |
| `UserPromptSubmit` | `user-sent-prompt` | | the prompt |
| `PostToolUse` | `agent-progressed` | | |
| `SubagentStop` **with** an `agent_type` | `agent-progressed` | `subagent-finished` | |
| `SubagentStop` without one | *nothing* | | |
| `Stop` | `turn-finished` | | the answer |
| `StopFailure` | `turn-failed` | the error kind | the error |
| `Notification` that wants you | `blocked-on-human` | the kind | the text |
| `Notification` that does not | *nothing* | | |
| `SessionEnd` | `session-ended` | why | |

Four of those rows were decided by replaying real hook payloads — 7,980 of them
at first, 23,984 by 2026-09-22 — rather than by reading documentation:

- **A `SubagentStop` with no `agent_type` is not a subagent.** 413 of 416 real
  ones carry an empty kind, and 245 of those fire within two seconds of a
  `Stop`: it is Claude's own turn wrapper. Treating it as progress holds a
  finished turn at `working` while you are the one being waited on.
- **`SessionStart` is not one event.** A compaction keeps the session id and is
  the agent working mid-turn. Splitting on `source` is this program's job.
- **`notification_type` is a reliable discriminator** and the message text does
  not need parsing: every one of the 64 real permission prompts carried
  `permission_prompt`, and all 87 idle nudges carried `idle_prompt`.
- **`StopFailure` carries `error`, not `error_type`.** This read `error_type`
  and `error_message` for as long as it existed, inherited from the prior Rust
  adapter and marked `[assumed]` because no payload carrying them had ever been
  seen. None ever was. [verified 2026-09-22, 2.1.267] against the one real
  `StopFailure` in the corpus and against the binary, which builds the hook
  input as `error`, `error_details` and the same `last_assistant_message` a
  `Stop` carries — so every `turn-failed` ever written arrived with an empty
  detail and no message. The message is what the turn died partway through
  saying, and `error_details` when it died before saying anything.

`PreToolUse` is deliberately not subscribed: `PostToolUse` already says the
session is working, and subscribing to both would double the number of processes
Claude waits for on every tool call.

## The name and the directory

Every one of those reports also carries what Claude calls the session, which is
how a session gets a name at all since core stopped having a `name` command
(plan.md D-60). Claude puts neither the name nor the session's own directory in
the hook payload — every hook input is built from the same six fields, and only
the statusLine command is given `session_name` — so `sessionname.go` reads the
file Claude keeps for each live session instead:

```
~/.claude/sessions/<pid>.json
{"pid":38514,"sessionId":"4a152fd4-…","cwd":"/…/agent-notify",
 "name":"agent-notify-96","nameSource":"derived",…}
```

`CLAUDE_PID` names that file directly and is set in every process Claude
spawns, this hook included. The `sessionId` in it is checked against the
payload's regardless: pids are reused, and `/clear` replaces the session inside
a process without the file changing its name.

**That file is not the only place a name can come from, and on its own it is
usually the worst one.** `nameSource` says which of four things produced the
name, and two of the four are not names at all: `derived` and `collision` are
both `<cwd-basename>-<counter>` — `agent-notify-16` — which is what a session
is called when nobody has named it. So `sessiontitle.go` reads the head of the
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

**The placeholder is not reported, and that is the point.** `agent-notify-16`
arrived looking exactly like a name somebody chose, so no display could tell
the two apart — and every session in the repository was already called
something like it, which is why they all got renamed by hand. What core
receives now is an empty name, which means *nobody has named this*, and core
does the guessing for every agent at once: `agent-notify-8e`, the directory
plus two characters of the session id, in `Record.DisplayName` where all seven
displays already read it (§A7.4.2, D-75, R24). `Record.Named()` is how a
display asks which it is holding.

**The first prompt is deliberately not read.** It is always there, it says
exactly what the session is about, and it is still not a name: it is prose, it
would have to be cut to fit a tab, and reporting it would take the one thing
this change bought — an empty name meaning nobody named it — straight back
again.

**The title is the one you want and it is not being written.** [verified
2026-09-22, 2.1.267] 26 transcripts on this machine hold an `ai-title`; the
newest was written on 2026-08-27 under 2.1.231 and none of the 222 written
since has one. The generator is still in the binary — `querySource:
"generate_session_title"`, a JSON-schema call returning `{title}` — and the SDK
still documents a custom title as one that "skips automatic title generation",
so this is a path that stopped firing rather than a field that went away. It is
read anyway: it costs one open and one read of a file `usage.go` has just read
from the other end, and the day it comes back every unnamed session is better
named for nothing.

**The head is read once, forwards, and 64 KiB of it.** That is the opposite of
`usage.go`, and for the opposite reason: the title is written at the beginning
and never rewritten, while the numbers are only ever at the end. [verified
2026-09-22] It lands between 16.8 KiB and 23.7 KiB in 18 of the 26 transcripts
that have one. The other 8 sit between 316 KiB and 2.2 MB in, at no fixed
distance from either end, and they are deliberately not chased: the scan to
reach them is unbounded and would run on every hook of every untitled session,
which today is all of them.

**The `cwd` comes from that file too, and not from the payload.** The payload's
is the directory the tool ran in, so it follows the agent's `cd` around and
changes several times a turn — and a display falling back to the last component
of it renames the session on every one of them. That is also why `CwdChanged`
is not subscribed: with the directory read from the file, the event carries
nothing, and it fired on every `cd`.

Nothing here can fail in a way that matters. An empty name means "leave the
stored one alone", the payload's cwd is the fallback when Claude is too old to
keep a file, and a session with nothing to read at all — no file, no transcript,
no prompt yet — shows the cwd, exactly as it did before this existed.

## The tokens and the model

What the session has spent is read out of Claude's own transcript on every hook,
in `usage.go`, and reported beside the name. Core adds up the responses it has
not counted before, so the same read on twenty hooks in a row costs nothing.

The transcript is the only place the numbers are. The hook payload carries none,
`~/.claude/sessions/<pid>.json` carries none, and the dollars and context
percentage on the status line are handed to the statusLine command alone — a
slot that belongs to you.

**One response is written as several lines**, one per content block, each
repeating the whole `usage`. In one 51 MB transcript that is 3401 assistant
lines carrying 1869 distinct `requestId`s, so a reader that adds up lines
reports 2.11× the output tokens actually spent. The `requestId` goes with every
response for that reason, and core counts each one once.

How full the window is used to be reported alongside, and is not any more. The
transcript cannot say how big the window is — a session on the 1M context
records `"model":"claude-opus-5"`, the same string a 200k one records — so every
Claude record carried a level with no limit beside it and no display could turn
that into a percentage. It went with `quota` and `agent_data` in D-76.

Claude's subagents are missing too: they are given their own transcript, named
in `SubagentStop`'s `agent_transcript_path`, and following it is not in this.

**The model comes off the same lines.** The hook payload does not carry one, so
this program's model had been writing an absent key into the agent's own section
of the record since the day it was added. It is read off the newest assistant
line instead — which is also the only one still true after a `/model` halfway
through a session.

The **branch** this program deliberately does not report: it is core's, read
from the checkout at the session's directory, because Claude puts `gitBranch` on
every transcript line and on a workspace that merely contains checkouts it says
`"HEAD"` for a repository sitting on `main`.

## Building

Go 1.26. Core is not published yet, so `go.mod` has a `replace` pointing beside
this repository; that line comes out when core is.

If your shell exported `GOROOT` from an outer context it overrides the toolchain
pinned in `.tool-versions` — `env -u GOROOT go build ./...` is the fix.
