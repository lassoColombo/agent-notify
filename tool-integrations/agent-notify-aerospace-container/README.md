# agent-notify-aerospace-container

[AeroSpace](https://github.com/nikitabobko/AeroSpace)'s **container** integration
for [agent-notify](../../agent-notify): it works out which macOS **window** a
session is in, brings that window to the front — switching workspace on the way
if it has to — and says whether you are already looking at it.

It is the outermost rung of a focus, because a session is at a path and every
layer of it has to be right for any of it to be visible:

```
aerospace   ──▶  Ghostty  ──▶  zellij        ──▶  pane 17
window 34                      session home
```

Focusing a pane in a window you cannot see changes nothing on your screen. That
is the whole reason this program exists, and it is why `containers` puts this
one **first**.

---

- [What it is](#what-it-is)
  - [The four subcommands](#the-four-subcommands)
  - [Why a window cannot simply be read out of the environment](#why-a-window-cannot-simply-be-read-out-of-the-environment)
  - [How the window is found](#how-the-window-is-found)
  - [Nothing about a window is ever stored](#nothing-about-a-window-is-ever-stored)
  - [Failures are words, not sentences](#failures-are-words-not-sentences)
- [Installation](#installation)
  - [Prerequisites](#prerequisites)
  - [Build it](#build-it)
  - [Put it on PATH](#put-it-on-path)
  - [Check it](#check-it)
  - [Prove it moved something](#prove-it-moved-something)
- [Configuration](#configuration)
  - [Where the configuration lives](#where-the-configuration-lives)
  - [`integrations.aerospace-container` — core's half](#integrationsaerospace-container--cores-half)
  - [`integrations.aerospace-container.settings` — this program's half](#integrationsaerospace-containersettings--this-programs-half)
  - [`containers` — your half](#containers--your-half)
  - [The title template in detail](#the-title-template-in-detail)
  - [Environment variables](#environment-variables)
  - [What is deliberately not configurable](#what-is-deliberately-not-configurable)
- [Two things about aerospace worth knowing](#two-things-about-aerospace-worth-knowing)
- [Testing](#testing)
- [What is unfinished](#what-is-unfinished)

---

## What it is

It is not a daemon. Core **runs** it with a subcommand when it needs an answer,
and each answer is one JSON object on stdout with exit 0; anything else means "I
could not answer", and stderr is the reason (plan.md D-38). There is no socket,
no cache and no state of its own.

### The four subcommands

| | when core runs it | what it may do |
| --- | --- | --- |
| `capture-environment` | inside the agent, as a child of the hook | read local state and return — **never** ask aerospace anything |
| `interpret-environment` | in the session-watcher, once per capture | anything; here it happens to ask nothing |
| `focus` | when somebody asked for this session | ask aerospace, and move a window |
| `focused` | when a notifier is about to speak | ask aerospace |

You never run them yourself — core does — but they are ordinary programs and
running them by hand is the fastest way to see what this thinks:

```
$ agent-notify-aerospace-container capture-environment
{"values":{"ZELLIJ_SESSION_NAME":"agent-notify"}}
```

`interpret-environment` is handed the record's `captured_context`: this
program's entry, and the process chain core walked from the hook upward when it
looked for the agent (D-93). The quickest way to hand it one by hand is to
replay a stored record:

```
$ jq .captured_context "$AGENT_NOTIFY_ROOT"/state/sessions/<key>.json | agent-notify-aerospace-container interpret-environment
{"chain":[{"pid":67315,"command":"agent-notify-cla"},{"pid":30426,"command":"claude"},
{"pid":29604,"command":"nu"},{"pid":29601,"command":"zellij"}],"title":"agent-notify | "}
```

```
$ … | agent-notify-aerospace-container focused
{"answer":"yes"}
```

The command names in a chain are truncated to sixteen characters, which is why
the hook appears as `agent-notify-cla`: core reads them from the kernel's
seventeen-byte `p_comm`, and that buffer is not cleared between uses, so the
name is cut at the first NUL rather than trimmed from the right.

The split between `capture-environment` and `interpret-environment` is the one
thing to understand, and it is not stylistic. The agent's environment and its
process tree exist nowhere but inside the agent, so reading them has to happen
there — on the path the agent is waiting on, which is why that half is forbidden
to talk to anything. Everything else happens later, in the session-watcher or in
your own shell, where blocking costs the agent nothing.

### Why a window cannot simply be read out of the environment

zellij's container asks the agent's environment which pane it is in and is told:
`ZELLIJ_PANE_ID` is simply there. A window is not, and three measurements say
there is no walk from an agent to its window at all
[verified 2026-09-18, macOS 26, aerospace 0.20.3, Ghostty 1.3.1]:

- **Nothing in a shell's environment names a window.** Ghostty hands down
  `GHOSTTY_BIN_DIR`, `GHOSTTY_RESOURCES_DIR` and `TERM_PROGRAM` and no id of any
  kind. Its CLI will not help either — `ghostty +new-window` answers "not
  supported on this platform", which is also the answer to every other way of
  asking it about a window.
- **A pid names an application, not a window.** aerospace answers in those
  terms: `list-windows` reports an `app-pid` per window, and two windows of one
  application carry the same one. Measured with a stand-in that could be opened
  and closed safely — two TextEdit windows, one process, one pid under both. So
  a pid narrows the window list to one application and no further, and whether
  that is one window or five is a person's window habits rather than anything
  the session knows.
- **Inside a multiplexer the process chain does not even reach the terminal.**
  An agent's ancestry ends at the zellij *server*, whose parent is 1; the client
  that draws it is in a different process tree entirely, under the terminal.
  This one is not a subtlety — it is the ordinary case, and it is what the title
  key exists for.

### How the window is found

So the window is **found**, every time it is needed, from two keys — neither of
which is sufficient on its own:

| key | where it comes from | when it is exact |
| --- | --- | --- |
| the process chain | walked inside the agent by core's hook, which climbs up to ten rungs looking for the agent, and handed to `interpret-environment` (D-93) | when the nearest ancestor that owns a window owns exactly one |
| a window-title prefix | a template, filled in from the agent's environment | when the title is specific to this session |

The chain is walked nearest-first, and the answer is every window belonging to
the first ancestor that owns any window at all. That is also what makes the
multiplexer case fall out rather than be special-cased: a zellij server owns no
windows, so the walk runs off the end of the chain and the title key is what is
left.

The two are then combined in order of specificity:

| what matched | what happens |
| --- | --- |
| both keys, and they overlap | the windows in both — the best evidence there is |
| both keys, and they do not | the title's windows, because the chain is the stale one: a restarted terminal hands its pid to somebody else while the title still names the session |
| the title alone | the title's windows |
| the chain alone, one window | that window |
| the chain alone, several windows | `ambiguous` — nothing is moved |
| the title was built but matches nothing | `place-is-gone` — nothing is showing this session |
| neither key | `never-placed` — core steps past this layer and the ones inside still run |

The first window of the survivors is the one focused. The rest are not waste:
`focused` uses them, because two terminal windows attached to the same
multiplexer session both show it, so either of them being in front means you are
looking at it.

### Nothing about a window is ever stored

Every other coordinate in this system is a property of the session: a pane
belongs to its zellij session for as long as both exist. A window is not — it
merely *shows* a session, right now, and stops the moment somebody detaches. A
stored window id would be a cached guess rather than a coordinate, and §A11.3
forbids trusting those.

So `interpret-environment` here asks aerospace nothing at all and only shapes
the keys, and the whole lookup happens inside `focus` and `focused`, where the
answer is used in the same millisecond it is obtained. One `aerospace` call
costs about 12ms, which is what makes asking every single time affordable.

The idea this replaced, for the record: capture `list-windows --focused` once
when the session starts, on the theory that you are looking at the terminal when
you type `claude`. A hook fires whenever the agent does something, and what is
focused then is where the *person* is, not where the agent is.

### Failures are words, not sentences

Each one deserves a different next move, and a sentence cannot be switched on:

| word | what it means |
| --- | --- |
| `place-is-gone` | the key matches no window — nothing is showing this session, so the walk stops before an inner container focuses a pane nobody can see |
| `ambiguous` | several windows could be it and nothing tells them apart; going to one at random is wrong half the time and looks exactly like success |
| `not-running` | aerospace is not there to be asked: not on this process's PATH with no `aerospace` setting to say where, or at a configured path that does not exist |
| `never-placed` | this session has no key at all; core *skips* this layer rather than failing, so the layers inside it still run |
| `refused` | aerospace was asked, understood, and would not |

`focused` has three answers rather than two, and the third is load-bearing:
"no" is said only when the window in front is demonstrably not one of the
candidates. When several windows could be this session and one of them is in
front, the honest answer is `cannot-tell`, and a notifier's rule for that is to
speak anyway.

## Installation

### Prerequisites

- **macOS.** aerospace is a macOS window manager. This program builds and its
  pure half still tests everywhere else, but off darwin there is nothing for
  it to place.
- **[AeroSpace](https://github.com/nikitabobko/AeroSpace) itself**, running.
  `brew install --cask nikitabobko/tap/aerospace`. Verified against 0.20.3;
  `aerospace --version` prints the CLI and the app server separately and both
  have to be there.
- **Go 1.26 or newer** — 1.26.2 is what `.tool-versions` pins.
- **agent-notify core**, built and on your PATH. It lives in this same
  repository, one directory over.

### Build it

This is a monorepo: core, the agent-integrations and the tool-integrations are
one clone. `make install` at the root builds every module into
`$(go env GOPATH)/bin`, which has to be on your PATH: the name
`agent-notify-aerospace-container` found there is all core knows about this
program.

```sh
git clone git@github.com:lassoColombo/agent-notify.git ~/projects/agent-notify
cd ~/projects/agent-notify
make install
```

If your shell exported a `GOROOT`, unset it for the build — `make` does — so
that the toolchain `.tool-versions` pins is the one that compiles.

### Put it on PATH

That `make install` was the whole of installing it (plan.md D-102). There is
no `install` verb for this program, `agent-notify install` sets up only the
agents, and nothing is written to `~/.config/agent-notify/config.yaml` to make
it run: every `agent-notify-<name>` on PATH runs, and the file need not exist.
Where aerospace is, this program looks up on PATH at runtime; the `aerospace`
setting is the override for the one process that was not started from your
shell (see
[this program's half](#integrationsaerospace-containersettings--this-programs-half)).

One thing is yours. If this is your only container, it is the whole of
`containers` and you need not write one. A window manager is outside whatever
multiplexer runs inside it, so with zellij too the list has to be written, this
program **first** — `doctor` says "write `containers`, outermost first" when
two containers answer `focus` and nothing says which is outside which:

```yaml
containers: [aerospace-container, zellij]
```

To turn it off without removing the binary, `integrations.aerospace-container.enabled: false`.
To take it out, delete the binary from PATH.

### Check it

```sh
agent-notify doctor
```

```
config       ok    2 integration(s) on PATH, 2 enabled, keep-ended-sessions 168h0m0s
integrations ok    reported by pid 86777 at 2026-10-11T20:38:44Z
             aerospace-container    run when core needs it
                                    answers capture-environment, interpret-environment, focus, focused — built against 0.0.0-dev
             zellij                 drawn when something it watches moves
                                    answers capture-environment, interpret-environment, focus, focused, render — built against 0.0.0-dev
containers   ok    outermost first: aerospace-container, zellij
```

Read that honestly. The `config` line counts every `agent-notify-<name>` on
PATH and how many of them are not turned off (D-102). The `integrations` list
is the session-watcher's report of what each of those said to `capabilities`
when it was asked, at startup and on `agent-notify watcher reload`; one that
did not answer is listed as `could not be asked`, with the reason. The
`containers` line is the order a focus will walk, or what is wrong with it: a
name in `containers` with no program on PATH, a name in the list whose program
does not answer `focus`, or two containers and no list. Beyond that, doctor
knows nothing about this program; whether aerospace can be found, and whether
a window can, is checked by using it.

The direct checks are these:

```sh
agent-notify-aerospace-container capture-environment          # your session name, if the template names it
agent-notify focus-session <session>                          # the real thing
agent-notify focused <session>                                # yes / no / cannot-tell
```

### Prove it moved something

No test covers whether a focus actually moved anybody; that one is demonstrated
by a person watching their own screen. Put something else on another workspace,
then:

```
looking at:  window 107 Firefox, workspace 2
$ agent-notify focus-session m15-demo
  aerospace-container    focused
  zellij                 focused
agent-notify is in front.
after:       window 34 Ghostty, workspace 1
focused?     yes
```

## Configuration

### Where the configuration lives

One file, core's, and it is YAML (plan.md D-103):
`~/.config/agent-notify/config.yaml`, or
`$XDG_CONFIG_HOME/agent-notify/config.yaml` if you set that, or under
`$AGENT_NOTIFY_ROOT` if you set *that*. This program never locates or parses the
file itself — it asks core's SDK for its own `settings` — so there is no second
copy of the path rules to disagree with the first. `agent-notify doctor` prints
the file it resolved to.

There is no file by default, and running agent-notify without one is
supported. So is running *this* without one: being on PATH is what turns it on
(D-102), and the file holds only what you want different.

Three keys matter here, and they are owned by three different parties.

### `integrations.aerospace-container` — core's half

Core reads this one. The keys are core's, the same for every integration, and
the entry's name is the program: `aerospace-container` is
`agent-notify-aerospace-container`, found on PATH. There is no key for where
the program is; you write names, not paths. An entry whose program is not on
PATH is a complaint on `doctor`'s `config` line.

| key | type | default | what it does |
| --- | --- | --- | --- |
| `integrations.aerospace-container.enabled` | bool | `true` | absence means enabled: being on PATH is the act of asking for the integration (D-102). Set it to `false` to keep the binary and turn the thing off |

### `integrations.aerospace-container.settings` — this program's half

Core hands this map over verbatim and never reads what is in it. A key nobody
declared is refused **by name**, because a misspelled setting that changes
nothing and says nothing is the config bug people give up on.

| key | type | default | what it does |
| --- | --- | --- | --- |
| `integrations.aerospace-container.settings.aerospace` | string | `aerospace` on PATH | where the `aerospace` binary is, when this process's PATH does not say. Unset, it is looked up on PATH at runtime, which is ordinarily yours: this program runs as a descendant of the agent, which you started from a shell. An agent launched from the Dock has no such PATH, and this key is the answer for it. Found neither way — not on PATH and no key, or a key naming a path that does not `stat` — `focus` reports `not-running` and `focused` answers `cannot-tell`, each with a sentence that names this key, rather than crashing |
| `integrations.aerospace-container.settings.title` | string | `"{ZELLIJ_SESSION_NAME} \| "` | the template for the window-title key. `{VARIABLE}` is filled in from the agent's environment. `""` turns the key off |

```yaml
integrations:
  aerospace-container:
    settings:
      aerospace: /opt/homebrew/bin/aerospace   # only when aerospace is not on the process's PATH
      title: "{ZELLIJ_SESSION_NAME} | "
```

The lookup is PATH-first since D-101. D-67 had made the key required and
absolute because a launchd job's PATH is `/usr/bin:/bin`; there has been no
launchd job since D-87, so the path that was being resolved in your shell is
the one this process already has. The key is named for the tool, not for the
program: nothing in this file names a program's path any more.

### `containers` — your half

```yaml
containers: [aerospace-container, zellij]
```

Outermost first (D-103). This is configuration rather than discovery because
nesting is not discoverable: zellij inside a window that aerospace manages looks, from
inside, exactly like zellij on its own. It is the one thing about containers
nothing decides for you, because which shell is outside which is something
only the person running them knows. The one exception is the case with nothing
to nest: when exactly one enabled integration answers `focus`, an absent list
is that one (D-101). With two or more and no list, nothing is walked, and
`doctor`'s `containers` line says to write it.

Core walks that list in order and each step must succeed before the next is
attempted — focusing a zellij pane in a window on a workspace you are not
looking at leaves you looking at nothing. A layer that has nothing to say about
a session is skipped rather than failed, which is what lets somebody whose outer
layer is unconfigured still focus the pane.

### The title template in detail

```yaml
integrations:
  aerospace-container:
    settings:
      title: "{ZELLIJ_SESSION_NAME} | "   # the default
```

zellij titles its terminal window `<session> | <active tab>`, so the session
name the agent already carries is an index into the window list. The match is a
**prefix**, and the separator is part of the key and is what makes it worth
trusting: `home` would match any window whose title happens to start that way,
while `home | ` is a zellij client attached to the session called home.

Every `{NAME}` in the template is also exactly the list of variables
`capture-environment` reads — the template naming them is what keeps the two
from drifting, since there is no second list to keep in step. If any one of them
is unset or empty, there is **no key at all** rather than a half-built one: a
prefix with a hole in it matches windows that have nothing to do with this
session.

It is configuration rather than code because the coupling belongs to whoever set
those two programs up, and because the next multiplexer should be a line in a
file rather than a release. What it needs is a multiplexer that puts its session
name in **both** the environment and the window title. zellij does. tmux — which
puts neither its session name nor anything else nameable in an agent's
environment — would need a different key.

```yaml
      title: ""    # no multiplexer: the process chain is the only way in, and it is the exact one
```

### Environment variables

This program declares none of its own. The variables it reads are whichever ones
your `title` template names — `ZELLIJ_SESSION_NAME` out of the box — and they
are read in the one place they exist, inside the agent's process.

Two of core's reach it indirectly. `AGENT_NOTIFY_ROOT` moves the state
directory, the runtime directory and the config file beneath it, which is how an
isolated instance is one step; this program honours it because it asks core's
SDK where things are rather than working it out. And `GOROOT`, if your shell
exported one, is a build-time hazard rather than a runtime one — see
[Build it](#build-it).

### What is deliberately not configurable

- **The timeout on one `aerospace` invocation is 2 seconds**, a named constant
  in the source. It is not in the file because nobody editing a config file has
  any information with which to choose a better value than the code does for how
  long `aerospace list-windows` should be allowed to take. It has a defined
  behaviour on expiry, which is what the rule actually asks for: the subcommand
  answers "I could not", core reads that as a container that did not run, and
  nothing waits.
- **How far the process walk climbs is core's to say**, and it is ten rungs
  from the hook (§A8.4). The distance from there to the terminal is not fixed —
  the agent, a shell, a login, and whatever wrapper somebody put in between —
  so core climbs to the top rather than counting steps, and the bound only
  exists because "a process tree cannot contain a cycle" is not the same
  sentence as "a process tree does not contain a cycle".
- **Which window wins when several match.** That order is specificity, argued
  above, and not a preference.
- **The environment variables to capture.** They come out of the title template,
  because a second list is a list that can disagree with the first — silently,
  since a capture naming the wrong variable produces no error, just a session
  with no coordinates.

## Two things about aerospace worth knowing

Both measured on 0.20.3, both load-bearing:

- **`focus --window-id` is the whole job.** A window on another workspace comes
  with its workspace, and focusing the window that is already focused exits 0
  rather than complaining — unlike zellij's `focus-pane-id`, which exits 2 at
  the same request. So there is nothing to ask first and nothing to make
  idempotent by hand.
- **It tells the truth with its exit code.** A window id it does not know exits
  1 and says so; so does a usage mistake. There is no stderr vocabulary to keep
  here, which is why nothing is read out of a failure beyond what the shared
  subprocess runner already says about it.

One more, about the listing rather than the focusing: `--json` alone answers
with app-name, window-id and window-title and **no pid**, and the pid is half of
how a window is recognised here. So every listing asks for
`--format "%{window-id}%{app-pid}%{app-name}%{window-title}"`, and it is one
string in the source because the two listings must agree — a window the
resolution chose and the focused window it is compared against have to be the
same shape.

## Testing

```sh
env -u GOROOT go test ./...
```

The whole of the interesting behaviour — which window, and whether there is an
answer at all — is a pure function of a window list, so it is tested on a machine
with no window manager on it. The subprocess half runs against a stand-in that
answers the way aerospace answers. The process walk is core's, and core tests
it against the real kernel.

What no test covers is whether a focus actually **moved** anybody. See
[Prove it moved something](#prove-it-moved-something).

## What is unfinished

- **Nothing here is published.** The monorepo has no remote, there are no
  releases and no versioning, and `go.mod` carries a `replace` pointing at
  core's path beside it — so this compiles where it sits and a clone of this
  directory alone does not compile at all. Removing that line is part of M18,
  the milestone whose whole job is being installable by somebody who is not us.
- **Inside a multiplexer there is no chain to narrow by**, so a browser window
  whose page title happens to start `home | ` matches the key and is believed.
  That cost is written into a test rather than into a footnote. The separator is
  what keeps it survivable, and the alternative — refusing to focus anything
  inside a multiplexer — is worse.
- **`doctor` cannot say whether a window could be found.** It reports that
  this program was asked and what it answered, and that `containers`
  is consistent; whether aerospace is on PATH, and whether any window carries
  the key, it does not try. A fuller `doctor` is also M18.
- **Off macOS there is nothing to place.** This program has no platform code
  of its own since D-93 — the process walk is core's — so it builds and its
  pure half tests everywhere, and `aerospace` is simply not there to ask. A
  Linux window manager would be a different integration.
- **tmux is not supported by the default template**, and cannot be by any
  template, because it puts nothing nameable in an agent's environment.
