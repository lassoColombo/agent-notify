<div align="center">
  <h1>agent-notify</h1>
  <p><strong>The semaphore over your coding agents</strong></p>
  <p>
    A standard for agents to get your attention.
  </p>
</div>

---

- [What this module is](#what-this-module-is)
- [How the pieces fit](#how-the-pieces-fit)
  - [The integrations](#the-integrations)
- [Installation](#installation)
  - [What you need](#what-you-need)
  - [Clone the monorepo](#clone-the-monorepo)
  - [Build and install everything](#build-and-install-everything)
  - [Set them up](#set-them-up)
  - [Start the watcher](#start-the-watcher)
  - [Shell completion](#shell-completion)
- [Configuration](#configuration)
  - [Where the file is](#where-the-file-is)
  - [Environment variables](#environment-variables)
  - [The top-level keys](#the-top-level-keys)
  - [`integrations.<name>`](#integrationsname)
  - [`integrations.<name>.settings`](#integrationsnamesettings)
  - [`containers`](#containers)
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

## The Problem

I want my window manager to notify me when one of my agents requires my attention, and jump to it with the click of a button.  
I want to browse all my running sessions in a picker and jump to the one I select.  
I want my terminal multiplexer to change the color of the pane based on the state of the agent running inside.

> Basically I want a great integration between my agents and my environment.

However, a standard for agents to communicate informations about the running sessions does not yet exist - and it may never will.  
All agents expose hooks, but they speak different dialects and obey to different mechanics.  
This lack of a standard has practical, unwanted consequences:
- the integration must implement one standard for every agent it wants to integrate with: if it wants to integrate a new agent it must implement new logic.
- the integration is coupled with the agent's behaviour: if something about the agent changes the integration must change too.

This lack of standard is unfortunate for someone who wants to develop an integration

If we had a standard we could write integrations that work by default with all agents. When a new agent comes around you don't need to develop new logic in order to talk with it. 

> Agent-notify provides that standard.

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
It notices agents dying, sweeps up behind them, and runs each display when
something that display watches moves. It wakes on the store itself: a kqueue
over `state/sessions` and `state/ended`, which every record write renames into.
There is one per machine and starting a second is refused by the first. Nobody
has to start it: the first hook that finds nobody holding the lock starts one.

A **tool-integration** renders that state (a display), acts on it (a container:
where is this session, and bring it to the front), or both. Core runs it with a
subcommand and JSON on stdin; a display that must own its process — a menu bar
— watches the store the same way the session-watcher does. Several displays at
once is the ordinary case, and they never talk to each other.

Reading does not need any of this. `agent-notify list` and `subscribe.Read` go
straight to the store with no watcher involved, which is what lets an agent's
own statusline poll three times a second for your *other* agents.

### The integrations

Each is its own Go module in this monorepo, one per agent and one per tool.

| | role | |
| --- | --- | --- |
| [agent-notify-claude](../agent-integrations/agent-notify-claude) | agent | Claude Code's hooks |
| [agent-notify-codex](../agent-integrations/agent-notify-codex) | agent | Codex CLI's hooks |
| [agent-notify-macos-notifier](../tool-integrations/agent-notify-macos-notifier) | display | a macOS notification when an agent wants you |
| [agent-notify-zellij](../tool-integrations/agent-notify-zellij) | display and container | pane and tab titles, placing and focusing a session |
| [agent-notify-picker](../tool-integrations/agent-notify-picker) | display | every session in a terminal, and a way into one |
| [agent-notify-aerospace-container](../tool-integrations/agent-notify-aerospace-container) | container | finding and raising the window a session is in |

## Installation

Nothing is published anywhere yet: there is no `go install` from a proxy, no
release, no tap. Everything below builds from this working copy, and the
integrations find core through a `replace` directive pointing at this directory.

### What you need

- **Go 1.26 or newer.** `.tool-versions` pins `golang 1.26.2`. If your shell
  exported `GOROOT` from an outer context it overrides that pin, and
  `env -u GOROOT go build ./...` is the fix.
- **macOS, or Linux 5.3 or newer.** Exits are watched with kqueue on one and
  pidfds on the other, and nothing else is supported: the three packages that
  ask the kernel have a `_darwin.go` and a `_linux.go` each, and no fallback.
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

### Build and install everything

```sh
cd ~/projects/agent-notify
make install
```

`make install` runs `go install .` in every module. Where they land is
`GOBIN`, and it has to be a directory on your PATH: finding
`agent-notify-<name>` there by name is the whole of what core knows about an
integration. `make install` prints where it put them and says so when that is
not on your PATH.

```sh
make install GOBIN=/opt/homebrew/bin
``` `make test` runs every module's tests; the `go.work` at
the root is what lets an editor see all eight modules as one.

### Set them up

```sh
agent-notify install
```

It asks every `agent-notify-*` on your PATH what it answers and runs
`agent-notify-<name> install` on the ones that answer the verb: the
agent-integrations, which write the hook into their agent's own settings file.
That is the only thing `install` exists for — a file core cannot know about.
**Nothing writes `config.yaml`, ever** (D-66, D-101), and after `make install`
and `agent-notify install` you are done: the file need not exist.

A tool-integration has nothing to install. **What is on PATH is what runs**
(D-102): `agent-notify-zellij` in a directory on your PATH is the whole of
asking for zellij, and the file never lists what runs. It is where you turn one
off, or tune one:

```yaml
integrations:
  picker:
    enabled: false
```

Name an agent to set up just that one; options after a name belong to that
program:

```sh
agent-notify install claude codex
agent-notify install claude --print
```

Two things stay yours. `containers` says which shell is outside which, and that
is not discoverable, so with two containers it goes in `config.yaml` by hand
(with one, that one is the order). And the picker's keybinding is KDL in
zellij's config; its README has it.

`agent-notify uninstall` mirrors `install`: each agent takes its hook back, and
nothing else was written. Run `install` again after a rebuild; it is idempotent.

### Start the watcher

You do not have to. The first hook that finds nobody holding the lock starts
one, and so does any display that owns its process. Start it by hand when you
want to watch it:

```sh
agent-notify watcher start
# started: pid 86777
```

Then check the whole thing over:

```
$ agent-notify doctor
agent-notify 0.0.0-dev   darwin/arm64

paths
  root        (default — AGENT_NOTIFY_ROOT is not set)
  state       /Users/you/Library/Application Support/agent-notify
  runtime     /var/folders/hg/sj58j9kx5sz6w73q4wf60dtm0000gn/T/agent-notify
  config      /Users/you/.config/agent-notify/config.yaml
  log         /Users/you/Library/Application Support/agent-notify/agent-notify.log

directories  ok    present, mode 700
config       ok    no file, using defaults — 6 integration(s) on PATH, 6 enabled, keep-ended-sessions 168h0m0s
binary       ok    /opt/homebrew/bin/agent-notify
store        ok    3 live session(s), 74 ended and still resumable, 0 forgotten this run
liveness     ok    boot 027BA0B8-… — 3 running, 0 gone but not yet ended, 0 cannot tell
watcher      ok    pid 86777, version 0.0.0-dev, since 2026-09-28T20:15:40Z
integrations ok    reported by pid 86777 at 2026-09-28T20:38:09Z
             aerospace-container    run when core needs it
                                    answers capture-environment, interpret-environment, focus, focused — built against 0.0.0-dev
             claude                 yours to start; core never runs it
                                    answers install, uninstall, none of which core calls
             codex                  yours to start; core never runs it
                                    answers install, uninstall, none of which core calls
             macos-notifier         drawn when something it watches moves
                                    answers render — built against 0.0.0-dev
             picker                 yours to start; core never runs it
                                    answers nothing core calls
             zellij                 drawn when something it watches moves
                                    answers capture-environment, interpret-environment, focus, focused, render — built against 0.0.0-dev
containers   ok    outermost first: aerospace-container, zellij

not checked here: whether each agent still runs its hooks. An
agent-integration's `install` says so, and is safe to run again.
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

One file, YAML (D-103), and **there is no file by default**. Running without
one is the supported case rather than a degraded one: states, counts, liveness,
history and every integration on your PATH all work with nothing configured.
What a file buys you is opinions — a tool turned off, a glyph changed, a
retention window, which shell is outside which.

The file never lists what runs. **What is on PATH is what runs** (D-102): the
set of integrations is every `agent-notify-<name>` found there, read whenever
the configuration is read, and a name appears in the file only to be turned off
or tuned. Nothing in the file is an integration's own knowledge, and no program
writes it (D-101). A key whose correct value is a fact about a program does not
belong here: it would only be a second copy that can disagree with the first,
and you would be the one who broke it. Paths are the case in point: every
program here is found on PATH by name, and the one path you may write is the
override for a tool a process started outside your shell cannot see.

Core is the only reader. An integration never locates, opens or parses the
file; it asks core for its own `settings` and is handed them (D-103).

### Where the file is

| Situation | Path |
| --- | --- |
| `AGENT_NOTIFY_ROOT` is set | `$AGENT_NOTIFY_ROOT/config.yaml` |
| `XDG_CONFIG_HOME` is set | `$XDG_CONFIG_HOME/agent-notify/config.yaml` |
| otherwise | `~/.config/agent-notify/config.yaml` |

`agent-notify doctor` prints the one it resolved, which is the answer to use
when the two of you disagree. It is the only file: there is no `conf.d/` and
no drop-in beside it (D-101).

### Environment variables

There are two variables agent-notify owns, and the rest are the platform's own,
read through the standard rules.

| Variable | What it does |
| --- | --- |
| `AGENT_NOTIFY_ROOT` | Moves everything beneath one directory: `<root>/state`, `<root>/run`, `<root>/config.yaml`. One variable, so that running an isolated instance is one step. It is made absolute, so a relative value is resolved against the process's own working directory — which for a program core starts is not yours. |
| `AGENT_NOTIFY_LOG_LEVEL` | `debug`, `info`, `warn` or `error`. Absent means `info`, and so does a value it cannot read — which it says in the log, because a typo in the variable you set precisely to see more must not be the reason you see the same as before. It is a variable rather than a configuration key because the log is opened before the configuration is read, deliberately, so that complaints about the configuration have somewhere to go. Setting it once before starting anything sets it for everything. |
| `XDG_STATE_HOME` | Where records and the log go on Linux. Ignored on macOS, which uses `~/Library/Application Support`. |
| `XDG_RUNTIME_DIR` | Where the lock and the report go on Linux. |
| `TMPDIR` | The same, on macOS: under launchd it is a per-user `0700` directory. With none, `os.TempDir()` plus a uid suffix, because `/tmp` is writable by everyone and an unsuffixed name there is a name another user can take first. |
| `XDG_CONFIG_HOME` | Where the configuration file is, as above. |
| `PATH` | Where every program here is found by name, and what decides which integrations exist: `agent-notify` itself, every `agent-notify-<name>` (each one found here runs, D-102), and the tool an integration drives. Every process descends from a hook, which descends from the agent you started from a shell, so this is your PATH; an agent started from the Dock is the exception, and the settings key is for it (D-101). |

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

`keep-ended-sessions` is written the way a person writes a duration. It is
`time.ParseDuration`'s spelling and the only one in this system: core's file and
every integration's settings read the same (D-78), and YAML does not change
that — the text arrives as text, quoted or not.

```yaml
keep-ended-sessions: 24h     # one day
keep-ended-sessions: 168h    # the default week
keep-ended-sessions: "30m"   # half an hour, if you want the list short; quotes are fine
```

A bare number is refused, and deliberately: there is no way to accept
`: 8` that does not also accept somebody who meant eight seconds writing what a
`time.Duration` reads as eight nanoseconds. The one number that survives is `0`,
which means the same however it is spelled. Getting it wrong costs the value and
not the file it is written in:

```
config       FAIL  /Users/you/.config/agent-notify/config.yaml: keep-ended-sessions = "7 days", which is not a duration; write it as 168h or 30m; using 168h0m0s
                  running with: 6 integration(s) on PATH, 6 enabled, keep-ended-sessions 168h0m0s
```

Every duration that is not this one — timeouts, sweep intervals, lock waits — is
deliberately not configurable. Each was mechanism rather than preference, and a
person editing this file has no information with which to choose a better value
than the code does. They are named constants beside the code they bound.

### `integrations.<name>`

A map keyed on the program's name, with one entry per program you have
something to say about — and **an entry is never the ask**. Every
`agent-notify-<name>` on your PATH runs, whether or not it is written here
(D-102); the entry is where you turn one off or hand it settings. You write
names, not paths. A name written here with no `agent-notify-<name>` on PATH is
a complaint, because the file is then describing something that does not exist:

```
config       FAIL  /Users/you/.config/agent-notify/config.yaml: integrations.zelij is written, and there is no agent-notify-zelij on PATH
```

Agents are in here on the same terms as everything else: `agent-notify-claude`
on PATH is an integration and `doctor` lists it. `enabled: false` under it
takes it out of the handshake and the report, and no further — its agent runs
it as a hook, and that was never core's to start or stop; `agent-notify
uninstall claude` is how that stops.

```yaml
integrations:
  picker:
    enabled: false
  zellij:
    settings:
      glyphs: { working: "*" }
```

| Key | Type | Default | What it does |
| --- | --- | --- | --- |
| `enabled` | boolean | absent means **yes** | `false` means "do not run this for me": core will not ask it anything, run it for anything, or hand its environment to the hook. Being on PATH is how you ask for an integration, so absence means enabled. |
| `settings` | map | empty | Handed to the integration verbatim. See below. |

What core does with the program is what it declared at the handshake: the hook
runs it on the agent's path if it answers `capture-environment`, the
session-watcher runs it to render or to focus if it answers those. The picker
answers nothing, so core never runs it; a keybinding does.

### `integrations.<name>.settings`

Everything under here belongs to that integration alone. Core never reads it,
never validates it, and must not know what the zellij display's glyph keys are
called. Core does load it — it is the one reader of the file (D-103) — and
hands it to the integration through `subscribe.Integration.Settings`, which
refuses a key the integration never declared **by name**, because a misspelled
setting that changes nothing and says nothing is the config bug people give up
on.

```yaml
integrations:
  macos-notifier:
    settings:
      sound: false
```

What each integration accepts is in that integration's own README.

The one key the three that drive a tool share is the tool's path — `zellij`,
`aerospace`, `alerter` — and it is **optional**: the integration looks the tool
up on PATH when it runs, and the key is the override for a process started
without your PATH, an agent launched from the Dock (D-101, amending D-67). The
message when neither finds it names the key to write.

```yaml
integrations:
  zellij:
    settings:
      zellij: /opt/homebrew/bin/zellij    # only if `zellij` is not on the runtime PATH
```

The one shape core does define is the **palette**: a display that paints a glyph
or a colour per state resolves it most-specific-first, and the map is yours.

```yaml
integrations:
  zellij:
    settings:
      glyphs:
        blocked-on-you: "!"
        blocked-on-you/permission-prompt: "?"
        working: "~"
```

Keys are the `kernel` or the `kernel/detail` pair. A state this build has never
met falls back to the nearest known rank, which is what makes a display built
today survive a seventh state shipped tomorrow.

### `containers`

```yaml
containers: [aerospace-container, zellij]
```

| Key | Type | What it does |
| --- | --- | --- |
| `containers` | list of integration names, **outermost first** | The order `focus-session` walks: raise the window, then focus the pane inside it. It is configured rather than discovered because nesting is not discoverable — zellij inside a window aerospace manages looks, from inside, exactly like zellij on its own. With exactly one integration answering `focus` there is nothing to nest, and an absent list is that one (D-101); with two or more you write it, and `doctor` says so until you do — "aerospace-container and zellij all answer focus, and which is outside which is not discoverable: write containers, outermost first". |

A name in the list that is not on PATH, or that is turned off, is a complaint;
so is a container that answers `focus` and is left out, because nothing would
ever ask it to.

### A complete file

This is a working configuration: a multiplexer that both paints and navigates, a
window manager and notifications, all on PATH and none of them named to be
turned on. Also on PATH, and turned off: a picker. The two agents are on PATH as
well and need no line — their hooks are in their own settings files, written by
`agent-notify install`.

```yaml
keep-ended-sessions: 168h

integrations:
  zellij:
    settings:
      glyphs:
        working: "*"
  macos-notifier:
    settings:
      sound: false
  picker:
    enabled: false

# Two containers, so which is outside which has to be said.
containers: [aerospace-container, zellij]
```

### What happens when the file is wrong

The rule that outranks every other rule: **a broken configuration must never
break the agent.** Loading it has no failure path at all — it returns something
usable and a list of complaints, and the caller decides how loudly to say them.
A hook logs and exits 0; `doctor` prints and exits 1.

Failure is graded, and the grades are not the same thing:

- **A file that is not YAML is refused whole**, and the defaults apply. Nothing
  in it is used, because a half-decoded file silently mixes your intent with
  defaults in a way nobody can see. The same goes for a value of the wrong type
  — a word where `history-messages` wants a number. An integration asking for
  its settings from a refused file is told that, rather than "you have none".
- **A key that is not recognised is ignored**, by name, and everything the file
  got right survives. The strict pass that finds it is a second decode purely to
  report typos: the first pass has to populate the configuration whatever
  happens, and a decoder that stopped at the first unknown key would decide how
  much of your file survives based on the order you happened to write it in.
- **A value that cannot be used is replaced by its default**, one at a time. A
  negative `history-messages`, or a `keep-ended-sessions` that is not a
  duration, costs you that line and not the other twelve.
- **A name under `integrations` with no program on PATH is a complaint** named
  after the program, and the rest of the file stands (D-102). A program that is
  on PATH and does not answer is a different thing, and the session-watcher's
  report and `doctor` say which.

```
$ agent-notify doctor
config       FAIL  /Users/you/.config/agent-notify/config.yaml: 1 key(s) not recognised and ignored:
                    line 3: field bogus-key not found in type config.Config
                  running with: 6 integration(s) on PATH, 6 enabled, keep-ended-sessions 24h0m0s
```

### Re-reading it

Nothing watches the file, deliberately: picking up a half-saved file mid-write
is a real failure and not a theoretical one. You ask.

```sh
agent-notify watcher reload
# asked pid 86777 to re-read /Users/you/.config/agent-notify/config.yaml
```

Asking is also how you retry, and how PATH is read again: an integration you
have just built gets picked up, and one that was broken gets another go rather
than being written off for the life of the process. The state directory is
fixed for the life of the process; everything else takes effect now.

## Where everything lives on disk

Two directories, because one must survive a reboot and the other must not: a
lock that outlived the process holding it is a ghost that takes manual cleanup,
and a session record that vanished on reboot is a session you can no longer
resume. Everything is mode `0700`, files `0600`, because a record
carries the text of what an agent last said.

| | macOS | Linux | Under `AGENT_NOTIFY_ROOT` |
| --- | --- | --- | --- |
| state | `~/Library/Application Support/agent-notify` | `$XDG_STATE_HOME/agent-notify` | `<root>/state` |
| runtime | `$TMPDIR/agent-notify` | `$XDG_RUNTIME_DIR/agent-notify` | `<root>/run` |
| config | `~/.config/agent-notify/config.yaml` | the same | `<root>/config.yaml` |

Inside state: `sessions/<key>.json` for what is live, `ended/<key>.json` for what
is over and still resumable, `history/<key>.json` for the last few things each
session said, `locks/<key>.lock` one per session, and `agent-notify.log`, which
every process writes to and which is in state rather than runtime because a log
swept away by a reboot is a log missing exactly the failures worth reading about.
Inside runtime: `session-watcher.lock` and `integrations.json` (what each integration said it answers, written so that
`doctor`, a focus off a keybinding and the hook can all read it without running
anything — and so you can `cat` it at three in the morning).

A session key is `host~agent~session-id`, percent-encoded so the encoding cannot
be ambiguous however strange an agent's session ids turn out to be, and capped
at 200 bytes because it becomes a filename.


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
| [`install`](#agent-notify-install) | any | wire every agent on your PATH up, or the ones named |
| [`uninstall`](#agent-notify-install) | any | take every agent on your PATH out of its hooks, or the ones named |
| [`watcher`](#agent-notify-watcher) | 0/1/3 | the one long-lived process |
| [`report-event`](#agent-notify-report-event) | **always 0** | what an agent-integration calls when its agent did something |
| [`completion`](#agent-notify-completion) | 0/1 | a completion script for your shell |

Anything else that fails exits 2. A bare `agent-notify` prints the help rather
than failing, because the first thing anybody types is the name of the program.

### `agent-notify list`

What is running, most urgent first. Aliased to `ls`.

This is the cold read path: it reads the store directly, with no session-watcher
anywhere. A session whose process is gone is shown as ended even
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

Watch sessions change, live. It watches the store as a display would and prints
every view it is handed, which makes it the way to find out what a display is
being told before blaming the display.

| Flag | Type | Description |
| --- | --- | --- |
| `--json` | switch | print the whole view as one JSON object per line |
| `--wake-on` | strings | a record field worth waking for; repeat it, or separate them with commas. TAB completes the fields |
| `--all` | switch | include sessions that have ended |

```
$ agent-notify tail --wake-on kernel
── 21:04:11: 3 session(s) ──
30  lenny-load         finished-a-turn  2h15m
20  agent-notify-95    working          38s
21:04:26  30  agent-notify-95   finished-a-turn   Done — the README is written.
```

A name that is not a record field is refused before anything runs, and the
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

It checks the directories, the configuration, that `agent-notify` itself is on
PATH (without it no hook can start a session-watcher, and the only other sign
is a line in the log), the store, liveness, whether the watcher is up — and
whether it *answers*, which is the difference between "something holds the
lock" and "something holds the lock and works". A wedged watcher is named and
never killed: recovery is your word, and a notification tool that kills things
unprompted is not one you would leave running.

The `config` line counts what PATH holds and what the file left on — `6
integration(s) on PATH, 5 enabled, keep-ended-sessions 168h0m0s` — because
PATH is what decides what runs (D-102). Then the integrations, one line per
program on PATH that is not turned off, agents included: what each answered at
the handshake, or why it could not be asked. An agent-integration reads `yours
to start; core never runs it — answers install, uninstall, none of which core
calls`, and the picker `answers nothing core calls`; both are correct and
neither is a fault. With no `agent-notify-*` anywhere on PATH the line says
`none on PATH`. And the containers: which answer `focus`, in the order they
will be walked, or that none does and nothing can be focused while everything
else works.

It also names what it does **not** check, so its output never reads as a clean
bill of health for something still being built. Today that is whether each
agent still runs its hooks; an agent-integration's `install` says so, and is
safe to run again.

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
  zellij                 focused
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

Wires the agents up. With no name it asks every `agent-notify-*` on your PATH
`capabilities` and runs `agent-notify-<name> install` on each that lists
`install` among its methods, one after another, each owning the terminal while
it runs. Those are the agent-integrations, and what each writes is the hook,
into its own agent's settings file — the one thing core cannot discover (R10).
Nothing writes `config.yaml` (D-101). With a name it runs that program's
`install` whether or not it said it answers, and **every option after a name
belongs to that program**: core does not parse them.

```sh
agent-notify install
agent-notify install claude --print
```

A tool-integration has nothing to install: it is on PATH, so it runs (D-102).
With no agent on PATH the command says so and exits 0. TAB offers
the integrations on your PATH. A name that is not there is refused with the
reason: it is its own program, and you build it first.

`agent-notify uninstall` mirrors it: `agent-notify-<name> uninstall` for every
program on PATH that answers the verb, or for the ones named. Each takes back
what its install wrote, and core has nothing of its own to remove.

### `agent-notify watcher`

The one long-lived process. It watches for agents dying and sweeps up behind
them, and it runs the displays. One per machine; starting a second is refused
by the first. `doctor` says whether it is running.

| Subcommand | Description |
| --- | --- |
| `run` | be the watcher, here, in this process |
| `start` | start it if it is not already running |
| `stop` | ask it to stop, and never kill it |
| `restart` | stop it, then start it |
| `reload` | SIGHUP: re-read the config without restarting |

`stop` and `reload` exit **3** when nothing is running, which is not a failure —
`restart` treats it as a fine state to start from.

`run` is normally started for you. Run it yourself to watch what it does:

| Flag (on `run`) | Type | Description |
| --- | --- | --- |
| `--foreground` | switch | stay attached and log to the terminal |

It resolves where things are from its environment, like every other command.
The spawner cuts the agent's environment down to the variables that decide
where things are, so a detached watcher resolves the same directories its
clients write to.

Losing the race to take the lock is the ordinary outcome and exits 0. Anyone may
start one; the lock is what makes that harmless.

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
| `--agent` | string | **required.** the agent this session belongs to; the middle third of its key |
| `--process` | string | the name the agent's process runs under, for finding it in this process's ancestry; defaults to `--agent` |
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
| `session` | the vocabulary: events, kernels, the record, the view, the display rules |
| `hook` | an agent-integration: one function, and a reverse reader for its agent's files |
| `subscribe` | a tool-integration: answer the subcommands core runs, read the store, watch it |
| `container` | the words a container answers in: outcomes and verdicts |
| `tool` | running the external program an integration drives, under a timeout |

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
`ByUrgency`, `MostUrgent`, `Differs`, `Ago`, and
`NewPalette` for the user's glyph table.

### Writing an agent-integration

A translation and one call.

```go
os.Exit(hook.Main(os.Args[1:], usage, "claude",
    map[string]func([]string) int{"install": install, "uninstall": uninstall},
    func(payload Payload) (session.Report, bool) {
        return session.Report{
            Key:     session.Key{Agent: "claude", SessionID: payload.SessionID},
            Event:   session.TurnFinished,
            Message: &payload.LastAssistantMessage,
        }, true
    }))
```

The third argument is the name the agent's process runs under, for finding it
in the hook's ancestry. It is a constant of the program, not configuration:
nothing in a file could know it better (D-101). The named subcommands are the
program's own — `install` and `uninstall`, which a person runs and which may
speak and fail — and `Main` answers `capabilities` with them as methods, which
is how `agent-notify install` knows whom to hand the verb to. `Main` hands a
named subcommand its arguments, refuses any other word with the usage, and
otherwise decodes the payload on stdin into a fresh `Payload`, hands it to the
translation and records what it returns. It never exits non-zero and never
writes to stdout, because an agent reads both as verdicts. `LastResponses` reads the agent's transcript
backwards for what the newest responses cost, given the one function that
knows what a line of that transcript looks like.

`Record` does the rest: resolves where things live, reads the configuration,
walks its own ancestry to find the agent's process by that name, runs the
`capture-environment` of each enabled integration that declared it, reads the branch, takes
the session's lock, reduces, writes, and starts a session-watcher if none holds
the lock. It returns no error, and that is the contract rather than an
oversight.

Every field of a `Report` except `Event` and `Key` is optional, and an absent
field means "leave what is there alone" rather than "clear it" — so a hook that
knows the cwd and one that does not are both usable, and the one that does not
cannot erase what the other established. `Message` is a pointer because `nil`
and `""` mean different things.

### Writing a tool-integration

One `Main`, and every function you fill in is a subcommand core may run. The
`capabilities` answer is derived from what is filled in, so it cannot disagree
with it.

```go
var me = subscribe.Integration{
    Name:      "zellij",
    WakeOn:    []string{"kernel", "detail", "name", "captured_context"},
    WantEnded: true,       // to give a pane back when its session ends
    Reads:     capture,    // capture-environment: runs inside the agent
}

func main() {
    os.Exit(subscribe.Main(me, subscribe.Commands{
        Render:    render,     // core runs it with the view on stdin
        Interpret: interpret,  // runs in the watcher: ask the tool
        Focus:     focus,
        Focused:   focused,
    }, os.Args[1:]))
}
```

A display fills in `Render`; a container fills in the other three; zellij fills
in all four. `Focused` returns three values, because "nobody can tell" is the
state of every fresh install. The contract in one line: one JSON object on
stdout and exit 0. Anything else means "I could not answer", and stderr is the
reason — core never parses it.

`render` is handed a `session.View`: **the current state**, never a stream of
transitions, and `view.Changed` is what moved since the last render — empty on
the first, which is what stops a notifier that started thirty seconds ago
announcing every agent that happens to be blocked.

A display that must own its process — a menu bar — fills in no `Render`, is
started by launchd, and calls `subscribe.Run(ctx, me, paint)`: it watches the
store and hands `paint` a view whenever something in `WakeOn` moves, starting a
session-watcher if none is running. `agent-notify tail` is the same loop.

There is no `install`. A tool-integration runs because it is on PATH (D-102),
and the program has nothing to write anywhere (D-101). `Named` is for
subcommands of your own that a person runs — the picker's `--preview` and
`--label` — and `Default` is what runs when no word matched.

Settings come from core, never from the file: an integration does not know
where the file is and does not parse YAML (D-103). Declare the shape with
`yaml` tags and ask for it filled in. `Settings(&into)` decodes this
integration's own `integrations.<name>.settings` into it, refuses a key nobody
declared by name, leaves `into` untouched when there is no entry, and returns
the refusal when the whole file was refused.

```go
type Settings struct {
    Zellij string            `yaml:"zellij"`   // the tool's path, when PATH will not do
    Glyphs map[string]string `yaml:"glyphs"`   // kernel or kernel/detail → glyph
}

var mine Settings
if err := me.Settings(&mine); err != nil {
    return err   // "integrations.zellij.settings: 1 key(s) not recognised and ignored: …"
}
```

Beside it: `Read()`/`ReadIncludingEnded()` are the cold
read, `History(key)` is one session's last few messages and transitions,
`Focus(key)` runs `agent-notify focus-session` for a display that offers a
click, and `CoreBinary()` is where `agent-notify` is, found on PATH. The tool
you drive is found the same way: `tool.Find(key, name, configured)` returns the
configured path when the settings give one, checked, and otherwise the name
looked up on PATH, with a message naming the key when neither answers.

`Main` refuses a `WakeOn` naming a field no record has, and a `Named`
subcommand called by any word core runs — `capabilities`, `capture-environment`,
`interpret-environment`, `focus`, `focused`, `render`. One trap it cannot catch, because it
is silent: a field your renderer reads and your `WakeOn` omits wakes you never
for that change. `FieldsRenderedButNotWokenFor` exists so a test can find that
for you.

```go
missing := session.FieldsRenderedButNotWokenFor(base, WhatToWakeFor(), render)
if len(missing) > 0 {
    t.Errorf("the render moves with %v and this display does not wake for them", missing)
}
```

### `capture-environment`

An agent's environment exists nowhere but the agent's own process, so the only
program that can read it is a descendant of it — which means a child of the
hook, and nothing else. Anything that needs to know *where* a session is fills
in `Reads` on its `subscribe.Integration`.

It runs on the path the agent is waiting on, under a one-second timeout, so the
rule is absolute: **read local state and return.** Never ask your tool anything,
never open a socket, never wait. Whatever it returns is stored verbatim under
that integration's name and is opaque to core in both directions; what it means
is agreed between your `Reads` and your `Interpret` and nobody else.

Filling in `Reads` is the declaration: `capabilities` lists
`capture-environment` among the methods like any other, and the hook asks only
the enabled integrations that declared it (D-101). What one answers is stored
like any other answer, and that is how core knows the question was answered
and need not be asked again.

### How core finds an integration

Three things are true of a working integration, and each is a different
mechanism. It is on your PATH as `agent-notify-<name>`, which is what turns it
on: the name is the whole of what core knows about it (D-101), and being found
is the whole of the ask (D-102) — `integrations.<name>.enabled: false` is the
only way off. It answered `capabilities`, which the session-watcher asks at
startup and on `reload` and writes to `integrations.json` for `focus`, the hook
and `doctor` to read; what it listed there is what core will run it for, and a
program that answers nothing is one core never runs. And a container is named
in `containers`, which is the one fact only you know — unless it is the only
container, in which case there is nothing to know. `doctor` prints where each
of yours stands.

## What is not finished

The codebase says this in places and it is worth saying here.

- **Linux is tested, not lived on.** The whole suite passes on Linux 6.10, in
  Docker Desktop's VM, but no real agent has been watched on a Linux desktop.
  A Linux record's `started_at` reads as a moment in 1970, on purpose: it is
  the time since boot, because a date there moves when the clock is set
  (D-89).
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
- **`wait` and a `history` command** are still on the list.
- **Annotations are never invalidated by `Apply`.** The rule that a derived
  annotation dies with the captured context it came from needs provenance the
  record does not yet carry.
- **Windows, remote sessions, a global event log and writing back into a
  session** are not missing steps. They are decisions recorded in plan.md that
  would have to be reversed first.

What does work, and has been the thing running on the machine it was written on
since 2026-09-18: two agents tracked in one vocabulary, liveness, zellij pane
and tab titles, notifications, a picker, and focus through every layer a
session is buried under.
