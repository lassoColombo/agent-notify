# agent-notify-zellij-container

zellij's **container** integration for [agent-notify](../../agent-notify): it
works out which pane and tab a session lives in, brings that pane to the front
when somebody asks, and says whether you are already looking at it.

```
$ agent-notify focus-session agent-notify
  aerospace-container    focused
  zellij-container       focused
agent-notify is in front.
```

---

- [What this is, and what it is not](#what-this-is-and-what-it-is-not)
- [How it works](#how-it-works)
  - [The four subcommands](#the-four-subcommands)
  - [Why capture and interpret are two programs' worth apart](#why-capture-and-interpret-are-two-programs-worth-apart)
- [Installation](#installation)
  - [Prerequisites](#prerequisites)
  - [Build it](#build-it)
  - [Put it on your PATH](#put-it-on-your-path)
  - [Register it with core](#register-it-with-core)
  - [Check it](#check-it)
  - [Installing the display as well](#installing-the-display-as-well)
- [Configuration](#configuration)
  - [Where the file is](#where-the-file-is)
  - [`[integration.zellij-container]`](#integrationzellij-container)
  - [`[integration.zellij-container.settings]`](#integrationzellij-containersettings)
  - [`[container] order`](#container-order)
  - [Environment variables](#environment-variables)
  - [What is deliberately not configurable](#what-is-deliberately-not-configurable)
- [Five things about zellij worth knowing](#five-things-about-zellij-worth-knowing)
- [Failures are words, not sentences](#failures-are-words-not-sentences)
- [Testing](#testing)
- [What is unfinished](#what-is-unfinished)

---

## What this is, and what it is not

agent-notify divides the tools it talks to by **role**, not by product. A
*display* is told what is happening and renders it; a *container* is asked where
a session is and moves your focus there. zellij can do both, so zellij is two
programs:

| | program | what it does | what it can do to you |
| --- | --- | --- | --- |
| display | [agent-notify-zellij-display](../agent-notify-zellij-display) | paints agent state onto pane and tab titles | writes text |
| container | **this one** | finds the pane, goes to it, reports whether you are there | **moves your cursor** |

They are separate on purpose (plan.md D-37): wanting your tab titles annotated
is not the same decision as allowing something to jump your terminal somewhere.
**Install either, both, or neither** — they have separate binaries, separate
config tables and separate installs, and each reads the environment it needs for
itself, so neither depends on the other being present.

The other difference is shape. A display is a long-lived process that connects
to the session-watcher and is handed state. A container is not a daemon at all:
core **runs** it with a subcommand whenever it needs an answer, and each answer
is one JSON object on stdout with exit 0 (D-38). There is no socket here, no
cache and no state — every invocation asks zellij afresh, which is also what
coordinates-at-the-moment-of-use requires, because panes die without telling
anybody.

## How it works

### The four subcommands

Everything this program does, it does as a subcommand reading one JSON object
from stdin and writing one to stdout. You never run these by hand — core does —
but they are the whole interface, and running them by hand is the fastest way to
see whether it works:

```
$ agent-notify-zellij-container capture-environment
{"ZELLIJ_SESSION_NAME":"agent-notify","ZELLIJ_PANE_ID":"2"}

$ echo '{"ZELLIJ_SESSION_NAME":"agent-notify","ZELLIJ_PANE_ID":"2"}' \
    | agent-notify-zellij-container interpret-environment
{"session":"agent-notify","pane":2,"tab":1,"tab_name":" root"}

$ echo '{"session":"agent-notify","pane":2,"tab":1}' \
    | agent-notify-zellij-container focused
{"answer":"no","detail":"tab 1 is not the active one"}

$ echo '{"session":"agent-notify","pane":2,"tab":1}' \
    | agent-notify-zellij-container focus
{"ok":true}
```

| subcommand | who runs it, and when | what it is allowed to do |
| --- | --- | --- |
| `capture-environment` | the hook, inside the agent's own process, on the path the agent is blocked on | read two environment variables and return — **never ask zellij anything** |
| `interpret-environment` | the session-watcher, once per capture | ask zellij which tab holds that pane; blocking is fine here |
| `focus` | `agent-notify focus-session`, and anything that offers a click | read, act, read back |
| `focused` | a notifier about to interrupt you | read, and answer in three values |

`focus` answers `{"ok":true}` or a named failure; `focused` answers `yes`, `no`
or `cannot-tell`. Anything else — a crash, a timeout, a binary that is not there
— arrives at core as the same thing: a container that did not run. Core never
parses stderr and reads no meaning into which non-zero code came back.

### Why capture and interpret are two programs' worth apart

`ZELLIJ_SESSION_NAME` and `ZELLIJ_PANE_ID` exist only inside the agent's own
process. Nothing outside it can see them, so reading them has to happen in a
child of the agent's hook — and that is precisely the one place where
`zellij action` against a wedged server would be sitting in the path your agent
is waiting on.

So the work is split. `capture-environment` reads the two variables and returns,
under core's one-second capture timeout, contributing nothing if it fails and
costing the agent nothing either way. `interpret-environment` runs later, in the
session-watcher, and does the asking: which tab holds pane 2, and what is that
tab called. The resulting coordinates are stored in the session's record under
this integration's name, opaque to core in both directions.

This is also why zellij is looked up lazily rather than at startup.
`capture-environment` needs no zellij at all, and it is the subcommand that runs
most often and on the most sensitive path; resolving the binary eagerly made it
pay for a config read and a handful of stats on every single hook event, and
fail outright on a machine where zellij lives somewhere unexpected — for a
capture that never needed it.

## Installation

### Prerequisites

- **zellij**, obviously — everything here is measured against **0.45.1**.
  `brew install zellij`, or whatever your platform's equivalent is.
- **Go 1.26 or newer** to build. The toolchain is pinned in `.tool-versions`.
- **agent-notify core**, built and on your PATH. It lives in the same
  repository; if you have not built it yet, do that first.

### Build it

As of today everything is **one monorepo** — core, the agent-integrations and
the tool-integrations in one tree — so there is one clone and no separate
repository to fetch:

```sh
git clone git@github.com:lassoColombo/agent-notify.git
cd agent-notify/tool-integrations/agent-notify-zellij-container

env -u GOROOT go build ./...
```

`env -u GOROOT` is there because a `GOROOT` exported by an outer context
overrides the pinned toolchain; if your shell does not export one, plain
`go build` is fine.

`go.mod` carries a `replace` pointing at core's directory in this tree:

```
replace github.com/lassoColombo/agent-notify => ../../agent-notify
```

Nothing is published to a remote yet, so that line is what makes this compile at
all. It comes out when core is published (M18). Until then, building this module
anywhere but inside the monorepo does not work, and is not meant to.

### Put it on your PATH

Two different things need to find this program, and they find it two different
ways.

**You** need it on your PATH, because `agent-notify install zellij-container`
is a dispatcher: it looks up `agent-notify-zellij-container` on PATH and execs
it. Without that, core reports that it cannot find it and stops.

```sh
env -u GOROOT go build -o ~/.local/bin/agent-notify-zellij-container .
# or, if you keep Go's own bin directory on PATH:
env -u GOROOT go install .
```

**Core** does not use PATH for it at all — it uses the absolute `binary` path in
the config table, which the install step below resolves for you. That is
deliberate, and it is the same reason `zellij` itself has to be an absolute path
in the settings: this program is run by the hook and by the session-watcher,
whose PATH is not your shell's. A launchd job's is `/usr/bin:/bin` and nothing
else, which is how a container can be perfectly correct and never once be found.

### Register it with core

```
$ agent-notify install zellij-container
[integration.zellij-container]
binary              = "/Users/you/.local/bin/agent-notify-zellij-container"
capture-environment = true

[integration.zellij-container.settings]
zellij = "/opt/homebrew/bin/zellij"

[container]
order = ["zellij-container"]

Nothing was written. Put that in /Users/you/.config/agent-notify/config.toml when you want this
running: the table being there is what turns it on.

If you already have a [container] table, add "zellij-container" to its `order` rather than
adding a second table — two of them is a TOML error. Outermost first: a
window manager before the multiplexer inside it.
```

**It prints and writes nothing** (D-66). Everything in agent-notify's config
file is yours to write: your comments, your ordering and your decisions survive
because nothing else ever touches the file. What this program knows and you
cannot — where its own binary is, that it answers `capture-environment`, and
where zellij is on the PATH you are standing in right now — is resolved exactly
and handed to you.

The tables go to **stdout** and every word of explanation to **stderr**, so once
you have decided, this is the whole installation:

```sh
agent-notify install zellij-container >> ~/.config/agent-notify/config.toml
```

If `zellij` was not on your PATH when you ran it, the `zellij` line comes out
empty with a comment saying so, and install exits 1. Fill it in by hand; nothing
can be focused without it.

`agent-notify install <TAB>` lists the integrations on your PATH, which is the
same question `install` asks a moment later, so it cannot offer you something
that would then be refused.

### Check it

```
$ agent-notify doctor
agent-notify 0.x.y   darwin/arm64

paths
  root        (default — AGENT_NOTIFY_ROOT is not set)
  config      /Users/you/.config/agent-notify/config.toml
  ...
config       ok    2 agent(s), 3 integration(s), keep-ended-sessions 168h0m0s
```

doctor parses the file and tells you what it resolved to. It is not a full
health check for a container, and says as much — a container is not a
long-running child, so it does not appear in doctor's per-integration
connection report the way a display does. What doctor will catch is the two
mistakes that actually happen: a config file that does not parse, and a name in
`[container] order` with no table behind it.

The real check is end to end, against a session that is running:

```
$ agent-notify list
$ agent-notify focused <session>        # yes / no / cannot-tell
$ agent-notify focus-session <session>
  zellij-container       focused
<session> is in front.
```

Two things to know while checking. Coordinates arrive only on the **next** hook
event after installing, because capture happens in the agent's process: a
session that was already running when you added the table has no zellij
coordinates yet and will be reported as `never-placed` until the agent next
fires a hook. And `focus-session` is idempotent on purpose — running it twice
exits 0 twice.

### Installing the display as well

They are independent installs and the display has its own table:

```sh
agent-notify install zellij-display   >> ~/.config/agent-notify/config.toml
agent-notify install zellij-container >> ~/.config/agent-notify/config.toml
```

Both print `capture-environment = true`, and both mean it: each captures for
itself, under its own name, so either can be installed without the other. Only
the container appears in `[container] order` — a display has no business being
in a focus chain.

## Configuration

### Where the file is

One file, `config.toml`, found the way core finds it:

| | where |
| --- | --- |
| default | `~/.config/agent-notify/config.toml` |
| with `XDG_CONFIG_HOME` set | `$XDG_CONFIG_HOME/agent-notify/config.toml` |
| with `AGENT_NOTIFY_ROOT` set | `$AGENT_NOTIFY_ROOT/config.toml` |

This program never works that out for itself: it asks core's SDK, because a path
derived twice is a path that will differ once, and the failure is silent — the
table lands in a file nothing reads, and the integration is correct and
invisible.

There is no file by default, and running agent-notify without one is supported.
A file that cannot be parsed is not fatal to core — the complaint goes to the log
and the defaults apply — but it **is** fatal to this integration, which refuses
to guess rather than act on defaults you did not write.

Three keys is the whole of it:

```toml
[integration.zellij-container]
binary              = "/Users/you/.local/bin/agent-notify-zellij-container"
capture-environment = true

[integration.zellij-container.settings]
zellij = "/opt/homebrew/bin/zellij"

[container]
order = ["aerospace-container", "zellij-container"]
```

### `[integration.zellij-container]`

This table is core's, and core reads it. **The table being present is what turns
this on**: installing is adding it, uninstalling is deleting it, and nothing
else in the file changes either way.

| key | type | default | what it does |
| --- | --- | --- | --- |
| `binary` | string | — (required) | the program core runs. **Absolute.** Resolved by `install`; core does not search PATH for it, because the processes that run it do not have yours. |
| `capture-environment` | boolean | `false` | whether the hook runs this program inside the agent to read its environment. **Must be `true`** — without it no session ever gets coordinates and this container is correct and invisible. The hook has no socket and cannot ask at connect time, so this one fact has to be written down (D-39). |
| `enabled` | boolean | `true` | absent means true, because writing the table *is* the ask. `enabled = false` keeps the table and the settings while taking it out of the chain — the way to switch it off for an afternoon without losing the lines. |

**Which** environment variables it captures is not here and is not yours to set.
They live in this program's source, where the code that reads them and the code
that reads them back both already are. A configurable list of variable names was
tried, and a typo in it broke the integration without a word of complaint
anywhere (D-57).

### `[integration.zellij-container.settings]`

Everything under `settings` belongs to this integration alone. Core hands it
through verbatim and never looks inside. **A key nobody declared is refused by
name**, because a misspelled setting that changes nothing and says nothing is the
config bug people give up on.

| key | type | default | what it does |
| --- | --- | --- | --- |
| `zellij` | string | — (required) | absolute path to the zellij binary. Every `zellij action` this program runs, runs that file. |

There is exactly one setting, and it is required. `install` resolves it by
looking zellij up on **your** PATH, which is the one context where the lookup is
answerable — and is exactly why no lookup happens at run time. There is no PATH
fallback and no list of likely Homebrew prefixes; both are attempts to guess
what the environment refuses to say.

It is spelled `zellij` rather than `binary` because the table above it already
has a `binary` of its own — this program's — and `install` prints the two
together, where two keys of the same name would be a coin flip (D-67).

Three ways to get it wrong, each with its own message:

```
[integration.zellij-container.settings] zellij is not set — run `agent-notify install
zellij-container`, which finds zellij in your own shell and prints the line to add

[integration.zellij-container.settings] zellij = "zellij" must be an absolute path: a
supervised child's PATH is not yours, so a bare name means something different here
than it does to you

[integration.zellij-container.settings] zellij = "/opt/homebrew/bin/zellij": stat
/opt/homebrew/bin/zellij: no such file or directory
```

### `[container] order`

```toml
[container]
order = ["aerospace-container", "zellij-container"]
```

This is core's table, shared by every container, and it is **outermost first**:
the window manager, then the multiplexer inside it. Each layer must succeed
before the next is attempted, because the inner one is meaningless without the
outer — focusing a zellij pane in a window on a workspace you are not looking at
leaves you looking at nothing.

Order is configuration rather than discovery because nesting is genuinely not
discoverable: zellij inside a window aerospace manages looks, from inside,
exactly like zellij on its own. Only the person running them knows which is
outside which, which is why `install` prints the line and refuses to merge it for
you. **If you already have a `[container]` table, add the name to its `order`
rather than pasting a second table — two of them is a TOML error.**

A layer with no coordinates for a session is *skipped*, not failed, so having
this container alone is a perfectly ordinary configuration; so is having it
under aerospace for sessions that aerospace can see and beside it for sessions
it cannot.

### Environment variables

| variable | read by | what it does |
| --- | --- | --- |
| `ZELLIJ_SESSION_NAME` | `capture-environment`, inside the agent | zellij sets it in every pane. Absent means this session is not in zellij, and interpretation declines — the ordinary case for an agent in a bare terminal, not an error anybody should see. |
| `ZELLIJ_PANE_ID` | `capture-environment`, inside the agent | likewise. Must parse as an integer. |
| `AGENT_NOTIFY_ROOT` | every subcommand, through core's SDK | moves the config file, the state directory and the runtime directory under one root. One variable, so running an isolated instance is one step — which is exactly what the test suite does. |
| `XDG_CONFIG_HOME` | every subcommand, through core's SDK | where `agent-notify/config.toml` is looked for. |

This program sets nothing, exports nothing, and has no environment variables of
its own.

### What is deliberately not configurable

Four timeouts bound this integration end to end, and **none of them is a config
key**. Every one has a name in source, beside the code it bounds, and a defined
behaviour on expiry:

| bound | value | whose | on expiry |
| --- | --- | --- | --- |
| one `zellij action` invocation | 2s | this program | the subcommand answers "I could not"; core reads a container that did not run, and nothing waits |
| one `capture-environment` | 1s | core's hook | the integration contributes nothing to that record and the hook writes and exits regardless |
| one `interpret-environment` | 5s | the session-watcher | no coordinates this round; the next capture tries again |
| one `focus` step, one `focused` query | 5s | core | a typed failure naming the container that did not answer, and the layers inside it are never attempted |

The reasoning is the same for all four and it is the rule the whole config file
is held to: a key belongs in that file only if **you** are the one who can know
the answer. Which containers you run and how they nest is yours. How long
`zellij action` should be allowed to take is not — nobody editing a TOML file
knows better than the code does, and a person who set it wrong would be
debugging a focus that fails for a reason the file never mentions.

The same test excludes the two other things people reach for: the environment
variables captured (the integration already knows them) and a PATH search for
zellij (the one context that could answer it is `install`, and it already did).

## Five things about zellij worth knowing

All measured on zellij 0.45.1, all load-bearing:

- **Focus is three facts, not one.** `is_focused` is per **tab** — every tab has
  one — and a tab has **two** focused panes at once: the focused tiled pane and
  the focused floating one. Which of them you are looking at depends on whether
  that tab is currently showing its floating panes. So "are you looking at this
  pane" is `tab.active && pane.is_focused && pane.is_floating ==
  tab.are_floating_panes_visible`, and anything short of that is `cannot-tell`
  rather than `no` — a wrong `no` silences a notification that should have fired.
  Focusing sorts the layer out by itself, in both directions: `focus-pane-id` on
  a tiled pane hides the floating layer that was covering it, and on a floating
  pane reveals the layer. So nothing here has to toggle anything — which matters
  most where a picker lives in a floating pane of the very session it is jumping
  inside.
- **zellij refuses to focus a pane that is already focused**, exiting 2 with
  "Pane Terminal(60) is already focused". A focus with nothing left to do would
  therefore be reported as a failure, having succeeded. This asks first, which
  makes focus idempotent — necessary for anything a person can click twice.
  Asking first means asking the whole question, though. `is_focused` is per layer
  as well as per tab, so the focused **tiled** pane of a tab that is showing its
  floating panes is `is_focused` and not in front, and zellij takes
  `focus-pane-id` on it perfectly happily — exit 0, floating layer lowered. A
  guard that read `is_focused` alone therefore skipped the single focus that
  would have cleared the cover, and left people arriving at the right pane behind
  a floating one they had to close by hand.
- **The exit code cannot tell you a session is missing.** Asked about a session
  that does not exist, zellij exits **0** and writes the list of sessions that do
  where the answer should be:

  ```
  $ zellij --session no-such-session action list-clients; echo "exit=$?"
  Session 'no-such-session' not found. The following sessions are active:
  agent-notify [Created 8m 24s ago] (current)
  home [Created 1day 3h ago]
  exit=0
  ```

  So the parse decides, every time. `list-panes` and `list-tabs` sidestep it by
  asking for `--json` and failing to unmarshal; `list-clients` has no `--json`,
  so its `CLIENT_ID ZELLIJ_PANE_ID RUNNING_COMMAND` header is what separates
  "nobody is attached" from "that session is not there" — and those two are a
  `zellij attach` and a stale record respectively. Every answer here reads before
  it acts, because coordinates are validated at the moment of use and panes die
  without telling anybody.
- **Focus exists with nobody watching, and then it is a no-op that exits 0.**
  Against a session no terminal is attached to, `go-to-tab-by-id` and
  `focus-pane-id` are both obeyed: the stored focus moves, no tab becomes
  `active`, the next client to attach lands there, and not one pixel changes
  anywhere. `list-clients` is the only place the difference is visible — it
  prints a header and one row per attached client, each naming the pane that
  client is looking at. So `focus` asks it, answers `not-attached` when the
  answer is nobody, and **reads the state back afterwards** when it is somebody:
  an exit code that is 0 either way is not evidence that anything moved.
- **Nothing can move a terminal from one session to another from the CLI.**
  `zellij action switch-session` is the obvious candidate and it does not do
  this: the server routes it with the client id of whoever sent the action, and a
  CLI invocation is its own throwaway client, so your terminal stays where it is.
  Measured against a real attached client, from outside and from a pane inside
  the session, with and without `--pane-id`: exit 0, no output, nothing moves —
  the same silence whether the target session is missing, exited, or perfectly
  healthy. (Upstream asks for `zellij action --client-id`:
  [zellij-org/zellij#5624](https://github.com/zellij-org/zellij/issues/5624).)
  A jump into another session is therefore the **window manager's** job — focus
  the terminal window that session is attached to, and this container does the
  pane inside it, outermost first. Where no window shows it at all, the honest
  answer is `not-attached`.

## Failures are words, not sentences

A focus that did not happen says **why** as a word, because each one deserves a
different next move and a sentence cannot be switched on:

| word | what happened | what to do about it |
| --- | --- | --- |
| `never-placed` | these are not this container's coordinates — the session started somewhere it was not watching | nothing; core skips this layer rather than failing the focus |
| `place-is-gone` | the pane is not in that session any more | the session may still be alive elsewhere; `agent-notify list` says |
| `not-running` | zellij could not be asked at all — not there, not answering, or the session is gone | start zellij, or check the `zellij` setting |
| `not-attached` | the session is running and no terminal is showing it, so there is no screen to bring anything to the front of | `zellij attach <session>`, and the same focus works |
| `refused` | zellij was asked, understood, and would not — or took the commands and the pane still is not in front | read the detail; this is the one that means something is genuinely wrong |

`not-attached` is **not one of core's words yet**. It is declared here, and core
carries a problem it has never heard of straight through untouched. It is the one
the other four could not cover: not gone, not absent, not a refusal, and its next
move belongs to nobody else.

## Testing

```sh
env -u GOROOT go test ./...
```

The focus rule is a table of panes and tabs and needs no zellij; so is the
`list-clients` parse, and so is everything `install` prints. The rest runs
against a real zellij and skips itself where there is none.

That includes whether a focus **moved** anybody, because a client does not have
to be a person. zellij is a terminal emulator, so a pane of one session running
`zellij attach` to another is a real attached client: `list-clients` reports it,
tabs become `active`, and a focus has somebody to be performed for. The suite
builds one and asserts both halves — a session nobody is attached to is named
`not-attached` rather than reported as focused, and an attached one ends with
that client's own row pointing at the pane.

What is still left to a person watching their own terminal: whether the window
that client lives in was the one in front. That is the window manager's half of
the answer and this program cannot see it.

## What is unfinished

- **Nothing is published.** This is a monorepo with a `replace` directive, not a
  `go install`-able module. M18 is where that changes, and it is also where this
  README stops telling you to clone a tree to get one binary.
- **`not-attached` is a word core does not know.** It survives the trip and reads
  correctly, but core has no branch on it — `focus-session` prints it without the
  paragraph of advice it prints for `place-is-gone` or `no-container-configured`.
- **doctor does not check this integration.** It checks the file it is named in.
  A container is not a supervised child, so there is no connection to report and
  no handshake to fail — which means a `binary` path that no longer exists looks
  fine in doctor and shows up as `container-unreachable` the first time somebody
  clicks.
- **Only tested on macOS**, and only against zellij 0.45.1. Nothing here is
  platform-specific in principle; nothing here has been run on Linux either.
- **Version skew is handled in one direction.** Coordinates written by a newer
  build are decoded leniently, so unknown fields survive; there is no negotiation
  and no version stamp in the coordinates themselves.
