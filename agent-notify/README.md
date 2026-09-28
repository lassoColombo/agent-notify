<div align="center">
  <h1>agent-notify</h1>
  <p><strong>The semaphore over your coding agents</strong></p>
  <p>
    One answer, always up to date: which of your agents wants you<br>
    Painted wherever you happen to be looking — a menu bar, a tab title, a notification<br>
    In one vocabulary, whichever agent it came from
  </p>
</div>

---

- [What this module is](#what-this-module-is)
- [How the pieces fit](#how-the-pieces-fit)
  - [The integrations](#the-integrations)
- [Installation](#installation)
  - [What you need](#what-you-need)
  - [Clone the monorepo](#clone-the-monorepo)
  - [Build and install core](#build-and-install-core)
  - [Build the integrations](#build-the-integrations)
  - [Turn them on](#turn-them-on)
  - [Start the watcher](#start-the-watcher)
  - [Shell completion](#shell-completion)
- [Configuration](#configuration)
  - [Where the file is](#where-the-file-is)
  - [Environment variables](#environment-variables)
  - [The top-level keys](#the-top-level-keys)
  - [`[agent.<name>]`](#agentname)
  - [`[integration.<name>]`](#integrationname)
  - [`[integration.<name>.settings]`](#integrationnamesettings)
  - [`[container]`](#container)
  - [A complete file](#a-complete-file)
  - [What happens when the file is wrong](#what-happens-when-the-file-is-wrong)
  - [Re-reading it](#re-reading-it)
- [Where everything lives on disk](#where-everything-lives-on-disk)
- [Commands](#commands)
  - [`agent-notify list`](#agent-notify-list)
  - [`agent-notify tail`](#agent-notify-tail)
  - [`agent-notify focused`](#agent-notify-focused)
  - [`agent-notify doctor`](#agent-notify-doctor)
  - [`agent-notify version`](#agent-notify-version)
  - [`agent-notify focus-session`](#agent-notify-focus-session)
  - [`agent-notify annotate`](#agent-notify-annotate)
  - [`agent-notify install`](#agent-notify-install)
  - [`agent-notify watcher`](#agent-notify-watcher)
  - [`agent-notify record`](#agent-notify-record)
  - [`agent-notify replay`](#agent-notify-replay)
  - [`agent-notify report-event`](#agent-notify-report-event)
  - [`agent-notify completion`](#agent-notify-completion)
- [The library](#the-library)
  - [The vocabulary](#the-vocabulary)
  - [Writing an agent-integration](#writing-an-agent-integration)
  - [Writing a display](#writing-a-display)
  - [Writing a container](#writing-a-container)
  - [`capture-environment`](#capture-environment)
- [What is not finished](#what-is-not-finished)

---

## What this module is

This directory is the **core** module of agent-notify: one Go module that is both
a library and the `agent-notify` command.

Everything an agent tells it arrives as one of nine events, and everything a
display is told is one of six states. That is the whole point: a tab title, a
menu bar and a notifier all read the same record, and none of them knows whether
the session behind it is Claude Code or Codex.

```
$ agent-notify list
30  lenny-load         finished-a-turn  2h15m    No. Tre cose nel worktree non sono mie…
30  lenny-integration  finished-a-turn  2h12m    Quinto punto: la granularità…
20  agent-notify-95    working          38s      Ten agents are running in the background…
```

The first column is the rank — how much your delay costs, most urgent first. The
six states are `blocked-on-you` (50), `broke` (40), `finished-a-turn` (30),
`working` (20), `idle` (10) and `ended` (0), each named for what it demands of
you rather than for what the agent finds interesting about itself.

**[plan.md](plan.md) is the design.** Section A is what this is and the rules it
keeps; section B is the plan being followed and the honest answer to "how much
of this works". This README is for using it.

## How the pieces fit

Three kinds of program, and core is the middle one.

An **agent-integration** is a small program the agent runs as a hook. It
translates one of its agent's hooks into one of the nine events and calls
`hook.Record`, which takes the session's lock, reduces the event against what is
on disk, and writes one record. That path never speaks: an agent reads its
hook's exit code, so a hook that failed would reach back into the very session
it is describing. Everything it has to say goes to the log.

The **session-watcher** is the one long-lived process, and it is the only one.
It notices agents dying, sweeps up behind them, runs each display when something
that display watches moves, and hands anything that connects the current state
over a unix socket. There is one per machine and starting a second is refused by
the first. Nobody has to start it: the first hook that finds nobody listening
starts one.

A **tool-integration** either renders that state (a display) or acts on it (a
container: where is this session, and bring it to the front). Several displays
at once is the ordinary case. They never talk to each other — each connects on
its own, is handed the same state, and paints wherever it paints.

Reading does not need any of this. `agent-notify list` and `subscribe.Read` go
straight to the store with no watcher and no socket involved, which is what lets
an agent's own statusline poll three times a second for your *other* agents.

### The integrations

Each is its own Go module in this monorepo, one per agent and one per (tool,
role) — so a tool that both draws and navigates is two programs you install
separately.

| | role | |
| --- | --- | --- |
| [agent-notify-claude](../agent-integrations/agent-notify-claude) | agent | Claude Code's hooks |
| [agent-notify-codex](../agent-integrations/agent-notify-codex) | agent | Codex CLI's hooks |
| [agent-notify-macos-bar](../tool-integrations/agent-notify-macos-bar) | display | the semaphore on the macOS menu bar |
| [agent-notify-macos-notifications](../tool-integrations/agent-notify-macos-notifications) | display | a macOS notification when an agent wants you |
| [agent-notify-zellij-display](../tool-integrations/agent-notify-zellij-display) | display | pane and tab titles |
| [agent-notify-picker](../tool-integrations/agent-notify-picker) | display | every session in a terminal, and a way into one |
| [agent-notify-zellij-container](../tool-integrations/agent-notify-zellij-container) | container | placing and focusing a session |
| [agent-notify-aerospace-container](../tool-integrations/agent-notify-aerospace-container) | container | finding and raising the window a session is in |

## Installation

Nothing is published anywhere yet: there is no `go install` from a proxy, no
release, no tap. Everything below builds from this working copy, and the
integrations find core through a `replace` directive pointing at this directory.

### What you need

- **Go 1.26 or newer.** `.tool-versions` pins `golang 1.26.2`. If your shell
  exported `GOROOT` from an outer context it overrides that pin, and
  `env -u GOROOT go build ./...` is the fix.
- **macOS.** Process facts, exit watching and the peer-pid lookup have darwin
  implementations and no others: `GOOS=linux go build ./...` does not compile
  today. Linux is M17.
- `git`, for the branch a session is working on. Read on the hook path, from
  whatever is checked out at the session's working directory.

### Clone the monorepo

One repository, ten modules. It used to be ten repositories; it is not any more,
and nothing about the layout below is optional — the integrations' `go.mod`
files say `replace github.com/lassoColombo/agent-notify => ../../agent-notify`,
which is a path relative to where they sit.

```sh
git clone <your remote>/agent-notify.git ~/projects/agent-notify
cd ~/projects/agent-notify
ls
# agent-integrations  agent-notify  tool-integrations
```

### Build and install core

```sh
cd ~/projects/agent-notify/agent-notify

env -u GOROOT go build ./...                        # everything compiles
env -u GOROOT go test ./...                         # and passes

env -u GOROOT go build -o /opt/homebrew/bin/agent-notify .
agent-notify version
# 0.0.0-dev
```

`go install .` works too and puts the binary in
`$(go env GOPATH)/bin`. Either way the binary must be somewhere your **shell**
can find it *and* somewhere a program core starts can: see
[`agent-notify-binary`](#the-top-level-keys) below, which is what a hook's PATH
and a launchd child's PATH make necessary.

### Build the integrations

Each is a module of its own, built the same way. Install only the ones you want.

```sh
cd ~/projects/agent-notify

for module in agent-integrations/* tool-integrations/*; do
  (cd "$module" && env -u GOROOT go build -o "/opt/homebrew/bin/$(basename "$module")" .)
done
```

The naming convention is the only thing core knows about an integration: a
program called `agent-notify-<name>` on your PATH is what `agent-notify install
<name>` execs and what TAB offers you.

### Turn them on

Installing the program and asking for it are two acts, deliberately. The program
being on your PATH does nothing; **its table in the configuration file is what
says yes.**

```sh
agent-notify install claude       # an agent-integration writes its agent's hooks
agent-notify install macos-bar    # a tool-integration prints the table to add
```

`install` is a dispatcher and nothing more: it execs `agent-notify-<name>
install` and every option after the name belongs to that program. Core does not
know where Claude keeps its settings or which environment variables zellij puts
in a pane, and must not.

`doctor` names the programs that are on your PATH and nowhere in your
configuration, which is exactly the gap this split opens:

```
installed    --    1 on your PATH and not mentioned here: picker
                   `agent-notify install <name>` hands over to each: a tool-integration prints
                   the table for you to add, an agent-integration writes its agent's hooks.
```

### Start the watcher

You do not have to. The first hook that finds nobody listening starts one, and
so does any subscriber. Start it by hand when you want to watch it:

```sh
agent-notify watcher status
# not running

agent-notify watcher start
# running: pid 86777, version 0.0.0-dev, since 2026-09-22T22:15:40Z
```

Then check the whole thing over:

```
$ agent-notify doctor
agent-notify 0.0.0-dev   darwin/arm64

paths
  root        (default — AGENT_NOTIFY_ROOT is not set)
  state       /Users/you/Library/Application Support/agent-notify
  runtime     /var/folders/hg/sj58j9kx5sz6w73q4wf60dtm0000gn/T/agent-notify
  config      /Users/you/.config/agent-notify/config.toml
  log         /Users/you/Library/Application Support/agent-notify/agent-notify.log

directories  ok    present, mode 700
sockets      ok    longest path 82 of 103 bytes — …/T/agent-notify/session-changes.sock
config       ok    2 agent(s), 6 integration(s), keep-ended-sessions 168h0m0s
store        ok    3 live session(s), 74 ended and still resumable, 0 forgotten this run
liveness     ok    boot 027BA0B8-… — 3 running, 0 gone but not yet ended, 0 cannot tell
watcher      ok    pid 86777, version 0.0.0-dev, since 2026-09-22T22:15:40Z
                  the socket answers: 77 session(s) in its snapshot
integrations ok    reported by pid 86777 at 2026-09-23T20:38:09Z
             macos-bar              connected, pid 86843 [display]
             macos-notifications    connected, pid 86844 [display]
             zellij-display         connected, pid 86847 [display]

not checked here: which agents have their hooks installed. Each
agent-integration answers that with its own `install`.
```

### Shell completion

The command line is [cobra](https://github.com/spf13/cobra), and the reason is
one feature rather than the family of them: **it can answer with what is
actually happening.**

```
$ agent-notify focus-session <TAB>
lenny-load              finished-a-turn
lenny-integration       finished-a-turn
agent-notify-95         working
```

Those are the sessions that are running, most urgent first, each glossed with
what it is doing — asked of the program at the moment you press TAB, with no
cache and no spec to go stale. `install <TAB>` offers the integrations that are
on your PATH, which is the same question `install` asks a moment later, so it
cannot offer something that would then be refused. `--wake-on <TAB>` offers the
record fields worth waking for.

```sh
agent-notify completion zsh  > "${fpath[1]}/_agent-notify"
agent-notify completion bash > /etc/bash_completion.d/agent-notify
agent-notify completion fish > ~/.config/fish/completions/agent-notify.fish
```

**nushell** has no generated script and does not need one: it takes an external
completer, and cobra's `__complete` is a protocol any of them can speak — for
every cobra program you have, not just this one. Flags come along for free:
`--<TAB>` lists them with their help text, `--output=<TAB>` lists that flag's
values.

The part worth getting right is the `:N` line cobra closes with. It is a
bitfield saying what to do with the answer, and dropping it is the difference
between `kubectl apply -f <TAB>` offering your yaml files and offering you the
word "yaml".

```nu
# completers/cobra.nu — one export, and it is the completer itself, so no config
# has to write a wrapper closure of its own. A command handing out a closure
# rather than an exported value, because a closure is not a parse-time constant
# and `export const` will not hold one.
export def cobra-completer []: nothing -> closure {
  {|spans: list<string>|
    let answer = (do -i { ^($spans | first) __complete ...($spans | skip 1) } | complete)
    if $answer.exit_code != 0 { return null }

    # Nothing obliges a program to sign its answer, so the last line is checked
    # for the directive rather than assumed to be it.
    let lines = ($answer.stdout | lines)
    let signed = (($lines | is-not-empty) and (($lines | last) =~ '^:[0-9]+$'))
    let directive = (if $signed { $lines | last | str substring 1.. | into int } else { 0 })
    let offered = (if $signed { $lines | drop 1 } else { $lines }
      | where {|line| $line | str trim | is-not-empty })

    # 1 is ERROR. null hands the slot back to nushell; an empty list would
    # instead assert that nothing exists, and stop it trying anything else.
    if ($directive bit-and 1) != 0 { return null }

    # 8 is FILTER_FILE_EXT, 16 is FILTER_DIRS: the lines are not candidates at
    # all, they are a filter over the filesystem, and the completing is ours.
    if ($directive bit-and 24) != 0 {
      let partial = ($spans | last)
      let prefix = ($partial | str replace -r '[^/]*$' '')
      let dirs_only = (($directive bit-and 16) != 0)
      let hidden_too = ($partial | split row '/' | last | str starts-with '.')
      return (do -i { ls -a (if ($prefix | is-empty) { "." } else { $prefix | path expand }) } | default []
        | where {|e| $hidden_too or (not ($e.name | path basename | str starts-with '.')) }
        # Directories always survive: you cannot reach the file you want
        # without descending to it first.
        | where {|e| $e.type == "dir" or ((not $dirs_only) and (($e.name | path parse | get extension) in $offered)) }
        | each {|e| $prefix + ($e.name | path basename) + (if $e.type == "dir" { "/" } else { "" }) })
    }

    # 4 is NO_FILE_COMP. Without it, an empty answer means "I have nothing, let
    # the shell try files" — so it has to be null, not an empty list.
    if ($offered | is-empty) {
      return (if ($directive bit-and 4) != 0 { [] } else { null })
    }

    # 32 is KEEP_ORDER, and there is nothing to do: nushell leaves an external
    # completer's order alone under every algorithm and sort setting.
    $offered | each {|line|
      let halves = ($line | split row "\t")
      {value: ($halves | first), description: ($halves | skip 1 | str join " ")}
    }
  }
}
```

Then one arm covers every cobra program you name, with nothing per-program to
write:

```nu
use completers/cobra.nu *

match $spans.0 {
  agent-notify => (cobra-completer)
  _ => $carapace_completer
} | do $in $spans
```

## Configuration

One file, TOML, and **there is no file by default**. Running without one is the
supported case rather than a degraded one: states, counts, liveness and history
all work with nothing configured. What a file buys you is agents core can
recognise the processes of, and integrations it is allowed to start.

Nothing in the file is an integration's own knowledge. Everything in it is
something only you can decide — which agents you run, where the binaries are,
which tools you want, how long to remember an ended session. A key whose correct
value is a fact about a program does not belong here: it would only be a second
copy that can disagree with the first, and you would be the one who broke it.

### Where the file is

| Situation | Path |
| --- | --- |
| `AGENT_NOTIFY_ROOT` is set | `$AGENT_NOTIFY_ROOT/config.toml` |
| `XDG_CONFIG_HOME` is set | `$XDG_CONFIG_HOME/agent-notify/config.toml` |
| otherwise | `~/.config/agent-notify/config.toml` |

`agent-notify doctor` prints the one it resolved, which is the answer to use
when the two of you disagree.

### Environment variables

There are two variables agent-notify owns, and the rest are the platform's own,
read through the standard rules.

| Variable | What it does |
| --- | --- |
| `AGENT_NOTIFY_ROOT` | Moves everything beneath one directory: `<root>/state`, `<root>/run`, `<root>/config.toml`. One variable, so that running an isolated instance is one step. It is made absolute, so a relative value is resolved against the process's own working directory — which for a program core starts is not yours. |
| `AGENT_NOTIFY_LOG_LEVEL` | `debug`, `info`, `warn` or `error`. Absent means `info`, and so does a value it cannot read — which it says in the log, because a typo in the variable you set precisely to see more must not be the reason you see the same as before. It is a variable rather than a configuration key because the log is opened before the configuration is read, deliberately, so that complaints about the configuration have somewhere to go. Setting it once before starting anything sets it for everything. |
| `XDG_STATE_HOME` | Where records and the log go on Linux. Ignored on macOS, which uses `~/Library/Application Support`. |
| `XDG_RUNTIME_DIR` | Where sockets and locks go on Linux. |
| `TMPDIR` | The same, on macOS: under launchd it is a per-user `0700` directory. With none, `os.TempDir()` plus a uid suffix, because `/tmp` is writable by everyone and an unsuffixed name there is a name another user can take first. |
| `XDG_CONFIG_HOME` | Where the configuration file is, as above. |
| `PATH` | How `install` and `doctor` find `agent-notify-<name>` programs. |

Those are also almost the whole of what an integration inherits when core runs
it. The session-watcher hands it an explicit short list — `HOME`, `PATH`,
`TMPDIR`, `USER`, `LOGNAME`, the four XDG variables, `AGENT_NOTIFY_ROOT` and
`AGENT_NOTIFY_LOG_LEVEL` — and leaves everything else behind, because an agent's
environment holds API keys and a session-watcher would otherwise keep them in
memory for days.

### The top-level keys

| Key | Type | Default | What it changes |
| --- | --- | --- | --- |
| `keep-ended-sessions` | duration | `"168h"` (7 days) | How long an ended session's record survives, so that resuming it is recognised as a return rather than a birth. It is also the set `list --all` and a picker offer you. Must be positive. |
| `history-messages` | integer | `20` | How many of a session's messages are kept in its history file. `0` keeps none, which is the point of it being expressible. |
| `history-changes` | integer | `100` | How many state transitions are kept, same rules. |
| `agent-notify-binary` | string | empty, meaning "work it out" | Where the `agent-notify` binary is, for the one job that needs to start it: a hook whose poke found no session-watcher, and a display that offers a click. A hook's PATH is not your shell's PATH, and a click on a menu-bar item runs from launchd's, which is `/usr/bin:/bin` and nothing else. |

`keep-ended-sessions` is written the way a person writes a duration. It is
`time.ParseDuration`'s spelling and the only one in this system: core's file and
every integration's table read the same (D-78).

```toml
keep-ended-sessions = "24h"    # one day
keep-ended-sessions = "168h"   # the default week
keep-ended-sessions = "30m"    # half an hour, if you want the list short
```

A bare number is refused, and deliberately: there is no way to accept
`= 8` that does not also accept somebody who meant eight seconds writing what a
`time.Duration` reads as eight nanoseconds. The one number that survives is `0`,
which means the same however it is spelled. Getting it wrong costs the value and
not the file it is written in:

```
config       FAIL  ~/.config/agent-notify/config.toml: keep-ended-sessions = "7 days",
                   which is not a duration; write it as "168h" or "30m"; using 168h0m0s
                  running with: 2 agent(s), 6 integration(s), keep-ended-sessions 168h0m0s
```

Every duration that is not this one — timeouts, sweep intervals, lock waits — is
deliberately not configurable. Each was mechanism rather than preference, and a
person editing this file has no information with which to choose a better value
than the code does. They are named constants beside the code they bound.

### `[agent.<name>]`

One table per agent you run. The name is your word for it: it is the `--agent`
an agent-integration reports under and the middle third of every session key,
and it never appears in core's source.

```toml
[agent.claude]
binary = "claude"

[agent.codex]
binary = "codex"
```

| Key | Type | What it does |
| --- | --- | --- |
| `binary` | string | What to look for when climbing a hook's process ancestry to find the agent itself. Without it nothing records which process a session is, and liveness has nothing to judge — the session still exists, but nobody can say whether it is still alive. |

### `[integration.<name>]`

One table per integration, and **the table being there is how you ask for it**.
An integration is installed by adding its table and uninstalled by deleting it;
nothing else in the file changes, and no integration can be made to depend on
another being present.

```toml
[integration.zellij-display]
binary = "/opt/homebrew/bin/agent-notify-zellij-display"

[integration.picker]
```

| Key | Type | Default | What it does |
| --- | --- | --- | --- |
| `enabled` | boolean | absent means **yes** | `false` means "do not run this for me". Writing the table at all is how you ask for the integration, so absence means enabled. |
| `binary` | string | none | The program, and naming one means **core may run this**: the hook runs it on the agent's path, the session-watcher runs it to ask what it answers and then to render or to focus. Looked up on PATH unless it is an absolute path. Leaving it out is a decision rather than an omission — a table with no binary belongs to something core never runs, like a menu bar, which owns its own process and is started by launchd. |
| `settings` | table | empty | Handed to the integration verbatim. See below. |

### `[integration.<name>.settings]`

Everything under here belongs to that integration alone. Core never reads it,
never validates it, and must not know what the zellij display's glyph keys are
called.

The integration decodes it through the SDK, which refuses a key it never
declared **by name** — because a misspelled setting that changes nothing and
says nothing is the config bug people give up on.

```toml
[integration.zellij-display.settings]
zellij = "/opt/homebrew/bin/zellij"

[integration.macos-notifications.settings]
sign  = "agent-notify self-signed"
sound = false
```

What each integration accepts is in that integration's own README, and
`agent-notify install <name>` prints the table it wants.

The one shape core does define is the **palette**: a display that paints a glyph
or a colour per state resolves it most-specific-first, and the table is yours.

```toml
[integration.zellij-display.settings.glyphs]
"blocked-on-you" = "!"
"blocked-on-you/permission-prompt" = "?"
"working" = "~"
```

Keys are the `kernel` or the `kernel/detail` pair. A state this build has never
met falls back to the nearest known rank, which is what makes a display built
today survive a seventh state shipped tomorrow.

### `[container]`

```toml
[container]
order = ["aerospace-container", "zellij-container"]
```

| Key | Type | What it does |
| --- | --- | --- |
| `order` | list of integration names, **outermost first** | The order `focus-session` walks: raise the window, then focus the pane inside it. It is configured rather than discovered because nesting is not discoverable — zellij inside a window aerospace manages looks, from inside, exactly like zellij on its own. |

### A complete file

This is a working configuration: two agents, a multiplexer that both paints and
navigates, a window manager, a menu bar, notifications, and a picker that is
installed but never started.

```toml
# A hook's PATH is not your shell's PATH, and a menu-bar click runs from
# launchd's, which is /usr/bin:/bin and nothing else.
agent-notify-binary = "/opt/homebrew/bin/agent-notify"

[agent.claude]
binary = "claude"

[agent.codex]
binary = "codex"

[integration.zellij-display]
binary = "/opt/homebrew/bin/agent-notify-zellij-display"

[integration.zellij-display.settings]
zellij = "/opt/homebrew/bin/zellij"

[integration.zellij-container]
binary = "/opt/homebrew/bin/agent-notify-zellij-container"

[integration.zellij-container.settings]
zellij = "/opt/homebrew/bin/zellij"

[integration.aerospace-container]
binary = "/opt/homebrew/bin/agent-notify-aerospace-container"

[integration.aerospace-container.settings]
aerospace = "/opt/homebrew/bin/aerospace"

[container]
order = ["aerospace-container", "zellij-container"]

[integration.macos-bar]
binary = "/opt/homebrew/bin/agent-notify-macos-bar"

[integration.macos-bar.settings]
sign = "agent-notify self-signed"

[integration.macos-notifications]
[integration.macos-notifications.settings]
sign  = "agent-notify self-signed"
sound = false

# Run when you press a key. It needs a terminal, so it has no binary: the path
# lives in the keybinding, which is the only thing that runs it.
[integration.picker]
```

### What happens when the file is wrong

The rule that outranks every other rule: **a broken configuration must never
break the agent.** Loading it has no failure path at all — it returns something
usable and a list of complaints, and the caller decides how loudly to say them.
A hook logs and exits 0; `doctor` prints and exits 1.

Failure is graded, and the grades are not the same thing:

- **A file that is not TOML is refused whole**, and the defaults apply. Nothing
  in it is used, because a half-decoded file silently mixes your intent with
  defaults in a way nobody can see. The same goes for a value of the wrong type,
  like the `"24h"` above.
- **A key that is not recognised is ignored**, by name, and everything the file
  got right survives. The strict pass that finds it is a second decode purely to
  report typos: the first pass has to populate the configuration whatever
  happens, and a decoder that stopped at the first unknown key would decide how
  much of your file survives based on the order you happened to write it in.
- **A value that cannot be used is replaced by its default**, one at a time. A
  negative `history-messages` costs you that line and not the other twelve.
- **An enabled integration with no `binary` is a complaint**, because nothing
  will ever happen for it.
- Two keys that used to exist — `capture` and `focus` in an integration's table
  — get a sentence of their own rather than the generic "not recognised",
  because a display that quietly stopped placing sessions deserves to be told
  why.

```
$ agent-notify doctor
config       FAIL  /Users/you/.config/agent-notify/config.toml: 1 key(s) not recognised and ignored:
                  3| bogus-key = 1
                   | ~~~~~~~~~ unknown field
                  running with: 0 agent(s), 0 integration(s), keep-ended-sessions 24h0m0s
```

### Re-reading it

Nothing watches the file, deliberately: picking up a half-saved file mid-write
is a real failure and not a theoretical one. You ask.

```sh
agent-notify watcher reload
# asked pid 86777 to re-read /Users/you/.config/agent-notify/config.toml
```

Asking is also how you retry. An integration that was broken, misspelled or not
yet installed gets another go rather than being written off for the life of the
process. Values that cannot change under a running process — the socket paths,
the state directory — keep what they had; everything else takes effect now.

## Where everything lives on disk

Two directories, because one must survive a reboot and the other must not: a
socket or a lock that outlived the process holding it is a ghost that takes
manual cleanup, and a session record that vanished on reboot is a session you
can no longer resume. Everything is mode `0700`, files `0600`, because a record
carries the text of what an agent last said.

| | macOS | Linux | Under `AGENT_NOTIFY_ROOT` |
| --- | --- | --- | --- |
| state | `~/Library/Application Support/agent-notify` | `$XDG_STATE_HOME/agent-notify` | `<root>/state` |
| runtime | `$TMPDIR/agent-notify` | `$XDG_RUNTIME_DIR/agent-notify` | `<root>/run` |
| config | `~/.config/agent-notify/config.toml` | the same | `<root>/config.toml` |

Inside state: `sessions/<key>.json` for what is live, `ended/<key>.json` for what
is over and still resumable, `history/<key>.json` for the last few things each
session said, `locks/<key>.lock` one per session, and `agent-notify.log`, which
every process writes to and which is in state rather than runtime because a log
swept away by a reboot is a log missing exactly the failures worth reading about.
Inside runtime: `subscribers.sock` (the stream, watcher to displays),
`session-changes.sock` (the datagram, hook to watcher), `session-watcher.lock`
and `integrations.json` (what each integration said it answers, written so that
`doctor`, a focus off a keybinding and the hook can all read it without running
anything — and so you can `cat` it at three in the morning).

A session key is `host~agent~session-id`, percent-encoded so the encoding cannot
be ambiguous however strange an agent's session ids turn out to be, and capped
at 200 bytes because it becomes a filename.

**Socket paths are the one hard limit.** `sun_path` is 104 bytes on darwin and
108 on linux, so the usable maximum is 103 and 107. A long
`AGENT_NOTIFY_ROOT` therefore fails, and it fails by name rather than with a
bare `invalid argument` from `bind`:

```
sockets      FAIL  socket path is 143 bytes, over the 103 this platform allows: …/run/subscribers.sock
                      set AGENT_NOTIFY_ROOT to a shorter directory
```

## Commands

`agent-notify <command> --help` says the rest: every command carries its own
description and its own flags, so there is no second place where the two can
disagree.

| Command | Exit | Description |
| --- | --- | --- |
| [`list`](#agent-notify-list) | 0/1 | what is running, most urgent first |
| [`tail`](#agent-notify-tail) | 0/1 | watch sessions change, live |
| [`focused`](#agent-notify-focused) | 0/1/2 | are you already looking at it? |
| [`doctor`](#agent-notify-doctor) | 0/1 | where everything lives, and what is wrong with it |
| [`version`](#agent-notify-version) | 0 | what every integration handshakes against |
| [`focus-session`](#agent-notify-focus-session) | 0/1 | bring one session to the front |
| [`annotate`](#agent-notify-annotate) | 0/1 | write one owner's section of a record |
| [`install`](#agent-notify-install) | any | hand over to that integration to set itself up |
| [`watcher`](#agent-notify-watcher) | 0/1/3 | the one long-lived process |
| [`record`](#agent-notify-record) | 0/1 | write what happens, to replay into a display later |
| [`replay`](#agent-notify-replay) | 0/1/2 | play a recording, with no agents and no store |
| [`report-event`](#agent-notify-report-event) | **always 0** | what an agent-integration calls when its agent did something |
| [`completion`](#agent-notify-completion) | 0/1 | a completion script for your shell |

Anything else that fails exits 2. A bare `agent-notify` prints the help rather
than failing, because the first thing anybody types is the name of the program.

### `agent-notify list`

What is running, most urgent first. Aliased to `ls`.

This is the cold read path: it reads the store directly, with no session-watcher
and no socket anywhere. A session whose process is gone is shown as ended even
if nothing has got round to filing it, and **nothing is written** — a command a
statusline polls three times a second has no business writing.

| Flag | Type | Description |
| --- | --- | --- |
| `--json` | switch | the records themselves, for scripts and displays |
| `--all` | switch | include sessions that have ended and are still resumable |

```
$ agent-notify list
30  lenny-load         finished-a-turn  2h15m    No. Tre cose nel worktree non sono mie…
20  agent-notify-95    working          38s      Ten agents are running in the background…
```

Rank, name, state, how long it has been in it, and the last thing it said. The
name is the agent's own when it reported one, and otherwise the last component
of the working directory with two characters of the session id after it —
`agent-notify-95` — because otherwise the three sessions you have open in one
repository are all called the same thing. The state is `kernel` or
`kernel/detail`.

`--json` is the whole record, which is the same shape a display is handed:

```json
{
  "key": { "host": "your-mac", "agent": "claude", "session_id": "099ccac2-…" },
  "sequence": 2009,
  "kernel": "finished-a-turn",
  "rank": 30,
  "state_since": "2026-09-23T18:21:58.085016Z",
  "created_at": "2026-09-20T20:05:25.759763Z",
  "updated_at": "2026-09-23T18:21:58.085016Z",
  "name": "lenny-load",
  "cwd": "/Users/you/projects/lenny",
  "branch": "main",
  "model": "claude-opus-5",
  "message": "No. Tre cose nel worktree non sono mie…",
  "usage": {
    "input": 2948, "output": 1895427,
    "cache_read": 438096474, "cache_write": 5698697, "reasoning": 802279,
    "counted_through": "req_011CfLqQtmu6TjnZMKaWpzkL"
  },
  "process": { "pid": 23055, "started_at": "…", "boot_id": "027BA0B8-…" },
  "captured_context": { "captured_at": "…", "ancestry": [], "by": {} },
  "derived_context": {},
  "annotations": {}
}
```

The four token counters are disjoint and their sum is the total, which is the
property you need to price them: a cache read costs a tenth of a fresh input
token and a cache write more than one. `reasoning` is the exception — it is the
part of `output` that was thinking, not a fifth addend. Nothing here is in
currency: `model` sits beside the counters and pricing is yours.

### `agent-notify tail`

Watch sessions change, live. It connects as a display would and prints every
view it is handed, which makes it the way to find out what a display is being
told before blaming the display.

| Flag | Type | Description |
| --- | --- | --- |
| `--json` | switch | print each changed record as JSON, one per line |
| `--wake-on` | strings | a record field worth waking for; repeat it, or separate them with commas. TAB completes the fields |
| `--all` | switch | include sessions that have ended |

```
$ agent-notify tail --wake-on kernel
── 21:04:11: 3 session(s) ──
30  lenny-load         finished-a-turn  2h15m
20  agent-notify-95    working          38s
21:04:26  30  agent-notify-95   finished-a-turn   Done — the README is written.
```

A name that is not a record field is refused before anything connects, and the
error suggests what you meant. That check is in the SDK rather than the watcher
on purpose: a wake-on typo produces no error anywhere else, and what you are
left chasing is "it is right when I start it and stale an hour later".

### `agent-notify focused`

Are you already looking at it? Yes (exit 0), no (exit 1), or nobody can tell
(exit 2).

The third answer is not a failure: it is the state of a machine with no
container installed, and anything that treats it as "no" will interrupt somebody
who is already looking at the thing it wants to tell them about. A notifier's
rule for it is to notify anyway.

```sh
agent-notify focused lenny-load
# yes
```

### `agent-notify doctor`

Where everything resolves to, and what is wrong with it. Exits non-zero when
something is, because a health check nothing can branch on is a health check
nobody runs twice.

It checks the directories, the socket path lengths, the configuration, the
store, liveness, whether the watcher is up — and whether it *answers*, which is
the difference between "something holds the lock" and "something holds the lock
and works". A wedged watcher is named and never killed: recovery is your word,
and a notification tool that kills things unprompted is not one you would leave
running.

It also names what it does **not** check, so its output never reads as a clean
bill of health for something still being built. Today that is which agents have
their hooks installed; each agent-integration answers that with its own
`install`.

One side effect worth knowing: `doctor` forgets whatever has outlived
`keep-ended-sessions`, and says how many.

### `agent-notify version`

```sh
agent-notify version   # or --version, or -v
# 0.0.0-dev
```

What every integration announces at the handshake and the watcher logs. Nothing
arbitrates on it: everything in this monorepo is built and released together, so
a version that did not match would be a deployment somebody half finished rather
than a protocol to negotiate. `doctor` does point out when the watcher that is
running is a different build from the binary you just typed, and leaves the
restart to you.

### `agent-notify focus-session`

Bring one session to the front, through every layer it is buried under — the
window manager, then the multiplexer — because where a session lives is the
containers' business and nobody else's. Aliased to `focus`.

It runs the containers itself rather than asking the watcher to. That is not a
shortcut: focus must work on a machine where nothing is running, and routing it
through a daemon would add a way for it to fail that has nothing to do with
whether the pane is there.

| Flag | Type | Description |
| --- | --- | --- |
| `--quiet` | switch | say nothing; the exit code is the answer |

```
$ agent-notify focus-session lenny-load
  aerospace-container    focused
  zellij-container       focused
lenny-load is in front.
```

The session argument is a key, a session id, a prefix of one (four characters or
more), or the name `list` prints — and TAB offers the ones that are running.
Ambiguity is an error that lists the candidates rather than a guess, because the
whole point of focus is going somewhere on purpose.

A failure is named rather than described, because each one deserves a different
next move: `no-container-configured` (nothing is set up to place a session, and
everything else works without one), `place-is-gone` (the pane is gone; the
session may still be alive elsewhere), `ambiguous` (several places match and
nothing chooses between them, so nothing was moved — going to one at random
looks exactly like working and is wrong half the time), `not-running`,
`never-placed`, `container-unreachable`, `refused`.

### `agent-notify annotate`

Write one owner's section of a record: a task id, a ticket, a colour somebody
wants remembered. Core stores the JSON and never looks inside it.

Annotations are the one part of a record with no lifetime rule attached. Nobody
asked for them, so nothing core does invalidates them: they live until their
owner overwrites them, and an owner's section is replaced wholesale, never
merged.

| Flag | Type | Description |
| --- | --- | --- |
| `--remove` | switch | delete this owner's section instead of writing it |

```sh
agent-notify annotate lenny-load jira '{"ticket": "OPS-4417"}'
# lenny-load: annotations.jira written (sequence 2010)

echo '{"ticket": "OPS-4417"}' | agent-notify annotate lenny-load jira -
agent-notify annotate lenny-load jira --remove
```

`-` as the JSON reads stdin. Invalid JSON is refused before anything is written.

### `agent-notify install`

Hand over to that integration to set itself up. It execs `agent-notify-<name>
install`, replacing this process, so the integration owns the terminal, the exit
code and anything it wants to ask you. **Every option after the name belongs to
that program**, and core does not parse them.

```sh
agent-notify install claude --print
agent-notify install macos-bar
```

TAB offers the integrations on your PATH. An integration that is not there is
refused with the reason: it is its own program, in its own module, and you build
it first.

### `agent-notify watcher`

The one long-lived process. It watches for agents dying and sweeps up behind
them, and it is what every display connects to. One per machine; starting a
second is refused by the first.

| Subcommand | Description |
| --- | --- |
| `run` | be the watcher, here, in this process |
| `start` | start it if it is not already running |
| `stop` | ask it to stop, and never kill it |
| `restart` | stop it, then start it |
| `reload` | SIGHUP: re-read the config without restarting |
| `status` | is it running, since when, and does its socket answer |

`status`, `stop` and `reload` exit **3** when nothing is running, which is not a
failure — `restart` treats it as a fine state to start from.

```
$ agent-notify watcher status
running: pid 86777, version 0.0.0-dev, since 2026-09-22T22:15:40Z
```

`run` is normally started for you. Run it yourself to watch what it does:

| Flag (on `run`) | Type | Description |
| --- | --- | --- |
| `--foreground` | switch | stay attached and log to the terminal |
| `--state` | string | the state directory, when not resolved from the environment |
| `--runtime` | string | the runtime directory |
| `--config` | string | the configuration file |

The three path flags exist because the spawner passes them: it also sanitises
the environment that path resolution reads, and without them a detached watcher
can end up watching a different directory than its clients write to — and
nothing errors, it simply never sees anything.

Losing the race to take the lock is the ordinary outcome and exits 0. Anyone may
start one; the lock is what makes that harmless.

### `agent-notify record`

Connect as a display would and write every view to a file, with the time it
arrived. A recording of a real day is how a display gets worked on without
waiting for agents to do something interesting.

| Flag | Type | Description |
| --- | --- | --- |
| `--output` | path | where to write; default is stdout |

```sh
agent-notify record --output ~/a-busy-afternoon.jsonl
# ^C
# recorded 412 moment(s)
```

One JSON object per line: `after_ms`, the whole `session`, and the `event` that
caused it when there was one.

### `agent-notify replay`

Play a recording into whatever connects, with no store, no agents and no
session-watcher.

| Flag | Type | Default | Description |
| --- | --- | --- | --- |
| `--speed` | float | `1` | how much faster than recorded; `0` plays with no waiting |
| `--loop` | switch | off | start over when the recording ends |
| `--wait-for` | duration | `10s` | how long to wait for something to connect |

It **requires `AGENT_NOTIFY_ROOT`** and refuses without one (exit 2), so that a
recording is never played into the same place your real sessions live. It then
takes the singleton lock at that root, which means a real watcher cannot be
running there — correct rather than awkward: a display being fed a recording
must not also be fed reality.

```sh
export AGENT_NOTIFY_ROOT=/tmp/an-replay
agent-notify replay ~/a-busy-afternoon.jsonl --speed 4 --loop &
AGENT_NOTIFY_ROOT=/tmp/an-replay agent-notify-macos-bar
```

### `agent-notify report-event`

What an agent-integration calls when its agent did something — the command-line
spelling of the hook path, for a shell script, a test, or an agent whose adapter
is not written in Go.

**It never fails and never speaks.** It exits 0 whatever happened and writes
nothing to stdout, because an agent interprets its hook's exit code — Claude
treats some of them as instructions — so a hook that failed would reach into the
very session it is describing. Everything it has to say goes to the log. An
unknown flag is a line in the log, not a non-zero exit into an agent that is
listening.

| Flag | Type | Description |
| --- | --- | --- |
| `--agent` | string | **required.** the agent this session belongs to, as configured |
| `--session` | string | **required.** the agent's own id for the session |
| `--event` | string | **required.** one of the nine events |
| `--detail` | string | the agent's refinement of the state |
| `--message` | string | what the agent said; `-` reads stdin |
| `--name` | string | what the agent calls this session |
| `--cwd` | string | the session's working directory; defaults to this process's |
| `--no-message` | switch | leave the stored message alone |

```sh
agent-notify report-event --agent claude --session "$SESSION" \
    --event turn-finished --message - < "$transcript"
```

There is no second implementation of the hook path here: the Go adapters call
exactly the same function this does.

### `agent-notify completion`

The cobra-generated script for `bash`, `zsh`, `fish` or `powershell`. See
[Shell completion](#shell-completion) above, including what to do in nushell,
which needs no script at all.

## The library

Seven packages are the entire public surface, and they are exactly the seven
directories at the top of the module — the doors. Everything else is under an
`internal/`, where it can change without breaking a module we do not control,
which is the whole reason the boundary is there. `main.go` sits beside them
because the command line is a client of the same seven and never a second
implementation of anything (R14).

All of them are under `github.com/lassoColombo/agent-notify/`.

| Package | For |
| --- | --- |
| `session` | the vocabulary: events, kernels, the record, the protocol, the display rules |
| `hook` | an agent-integration: one function |
| `subscribe` | a display: connect, be handed the state, render |
| `container` | a container: place and focus a session |
| `capture` | the one subcommand that runs inside the agent |
| `tool` | running the external program an integration drives, under a timeout |
| `logs` | the one log file every agent-notify process appends to |

A test keeps that list honest: `layering_test.go` fails if a new exported
package appears at the top level, if a package lands in no floor of the
dependency graph, or if one subcommand reaches into another.

### The vocabulary

Nine events go in, six kernels come out, and `Reduce` is the whole state
machine. An adapter cannot invent an event — one that could would be an adapter
that decides what your bar counts.

```go
session.Reduce(session.Working, session.TurnFinished)  // finished-a-turn
```

Seven of the nine assert a state (`user-sent-prompt`, `agent-progressed`,
`turn-finished`, `turn-failed`, `turn-interrupted`, `blocked-on-human`,
`session-ended`); `session-started` and `context-changed` carry metadata and
leave a live session exactly where it is — and so does an event this build has
never heard of, because failing to understand a state is not a licence to decide
what it was. Any event arriving on an ended session revives it, so resume works
even for an agent with no session-start hook at all.

The rules every display would otherwise write for itself, and write slightly
differently, are here once: `Record.DisplayName()`, `Record.State()`,
`ByUrgency`, `MostUrgent`, `Urgency`, `Differs`, `JustArrived`, `Ago`, and
`NewPalette` for the user's glyph table.

### Writing an agent-integration

A translation and one call.

```go
hook.Record(session.Report{
    Key:     session.Key{Agent: "claude", SessionID: payload.SessionID},
    Event:   session.TurnFinished,
    Message: &payload.LastAssistantMessage,
})
```

`Record` does the rest: resolves where things live, reads the configuration,
walks its own ancestry to find the agent's process, runs the
`capture-environment` of each integration that has one, reads the branch, takes
the session's lock, reduces, writes, and pokes the watcher — starting one if
nobody is listening. It returns no error, and that is the contract rather than
an oversight.

Every field of a `Report` except `Event` and `Key` is optional, and an absent
field means "leave what is there alone" rather than "clear it" — so a hook that
knows the cwd and one that does not are both usable, and the one that does not
cannot erase what the other established. `Message` is a pointer because `nil`
and `""` mean different things.

### Writing a display

A render function and a main of about ten lines.

```go
func main() {
    subscribe.Run(context.Background(), subscribe.Integration{
        Name:   "zellij-display",
        Roles:  []string{"display"},
        WakeOn: []string{"kernel", "detail", "name"},
        OnChange: func(view subscribe.View) error {
            return render(view.Sessions)
        },
    })
}
```

The callback is handed **the current state**, never a stream of transitions,
which makes it structurally impossible to write a display that breaks after a
dropped message, a restart or a closed laptop. `view.Changed` is what moved
since the *last call* — across a reconnection and a session-watcher restart,
because what it is compared against is what YOU were last shown rather than what
arrived on any one connection — and is empty on the very first one, which is
what stops a notifier that started thirty seconds ago announcing every agent
that happens to be blocked.

`Run` owns connect, declare, coalesce, reconnect and shutdown. There is no
resync to own: the session-watcher sends the whole world every time, so being
out of date is not a state you can be in.
It starts a watcher if there is none. Beside it: `Settings(&mine)` decodes this
integration's own settings table and refuses a key nobody declared,
`Read()`/`ReadIncludingEnded()` are the cold read with no socket, `History(key)`
is one session's last few messages and transitions, `ConfigFile()` is for an
`install` that has a table to add, and `CoreBinary()` is for a display that
offers a click.

One trap worth knowing about, because it is silent: a field your renderer reads
and your `WakeOn` omits wakes you never for that change, and nothing errors.
`EachFieldMoved` exists so a test can find that for you.

```go
for field, moved := range session.EachFieldMoved(base) {
    if render(base) != render(moved) && !slices.Contains(wakeOn, field) {
        t.Errorf("the render moves with %q and this display does not wake for it", field)
    }
}
```

### Writing a container

Up to four functions and a two-line main. A container is *run*, not connected:
it is asked things rather than told them, so it has subcommands.

```go
func main() {
    os.Exit(container.Main(container.Integration{
        Name:      "zellij-container",
        Capture:   capture,    // runs inside the agent: read, do not ask
        Interpret: interpret,  // runs in the watcher: ask the tool
        Focus:     focus,
        Focused:   focused,
    }))
}
```

Every function is optional — a container that can place a session but not focus
it is a legitimate thing to ship. `Focused` returns three values, because
"nobody can tell" is the state of every fresh install and a bool would force the
receiver to guess which of the two it meant.

The contract in one line: one JSON object on stdout and exit 0. Anything else
means "I could not answer", and stderr is the reason — core never parses it and
reads no meaning into which non-zero code came back, because a program that
crashed, hung or was deleted are all the same thing.

### `capture-environment`

An agent's environment exists nowhere but the agent's own process, so the only
program that can read it is a descendant of it — which means a child of the
hook, and nothing else. Anything that needs to know *where* a session is must
therefore answer this one subcommand, whatever else it is: a container fills in
`Capture` alongside its other three, and a display adds one case to the `main`
it already has.

```go
case capture.Command:
    os.Exit(capture.Main(Name, readTheTwoVariablesWeNeed))
```

It runs on the path the agent is waiting on, under a one-second timeout, so the
rule is absolute: **read local state and return.** Never ask your tool anything,
never open a socket, never wait. Whatever it returns is stored verbatim under
that integration's name and is opaque to core in both directions; what it means
is agreed between your `Capture` and your `Interpret` and nobody else.

There is nothing to declare. Every integration core can run is asked this one,
and an integration with nothing to read answers an empty object, which core
stores as no entry at all.

## What is not finished

The codebase says this in places and it is worth saying here.

- **It runs on macOS only.** `GOOS=linux go build ./...` does not compile:
  process facts, exit watching and the socket peer-pid lookup have darwin
  implementations and no others. Linux is M17, and two assumptions in the
  liveness design stay marked `[assumed]` until it lands.
- **Nothing is published.** No module proxy, no releases, no artifacts. Every
  integration is built beside core through a `replace` directive, and a clone of
  one module alone does not compile. That is M18's job, along with versioning.
- **`Version` is the constant `0.0.0-dev`** until the build learns to stamp it.
  Nothing arbitrates on it anyway.
- **Nothing in the public API is promised to survive.** Every integration in
  this system is built from this source and released with it, so a symbol that
  stops being useful is deleted rather than kept for a reader that does not
  exist. That changes when core is published, not before.
- **`doctor` is incomplete, and says so.** It does not check whether an agent's
  hooks are actually installed.
- **The picker is not done** (M16), and `wait` and a `history` command are still
  on the list.
- **Annotations are never invalidated by `Apply`.** The rule that a derived
  annotation dies with the captured context it came from needs provenance the
  record does not yet carry.
- **Windows, remote sessions, a global event log and writing back into a
  session** are not missing steps. They are decisions recorded in plan.md that
  would have to be reversed first.

What does work, and has been the thing running on the machine it was written on
since 2026-09-18: two agents tracked in one vocabulary, liveness, zellij pane
and tab titles, the semaphore on the menu bar, notifications, and focus through
every layer a session is buried under.
