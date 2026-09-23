# agent-notify-aerospace-container

aerospace's **container** integration for [agent-notify](../../agent-notify): it
works out which **window** a session is in, brings that window to the front —
switching workspace on the way if it has to — and says whether you are already
looking at it.

It is the outermost rung of a focus, and a session is at a path:

```
aerospace   ──▶  Ghostty  ──▶  zellij        ──▶  pane 17
window 34                      session home
```

Focusing a pane in a window you cannot see changes nothing on your screen. That
is the whole reason this program exists, and it is why `[container] order` puts
this one **first**.

## Installing

```
agent-notify install aerospace-container            # prints what it needs; writes nothing
agent-notify install aerospace-container >> ~/.config/agent-notify/config.toml
```

```toml
[integration.aerospace-container]
binary              = "/opt/homebrew/bin/agent-notify-aerospace-container"
capture-environment = true

[integration.aerospace-container.settings]
aerospace = "/opt/homebrew/bin/aerospace"   # required, absolute; install resolves it

[container]
order = ["aerospace-container", "zellij-container"]
```

If you already have a `[container]` table, add `"aerospace-container"` to its
`order` rather than pasting a second one — two `[container]` tables is a TOML
error. Put it first: which container is outside which is your fact, and a window
manager is outside whatever multiplexer runs inside it.

## Why this one cannot just read a variable

zellij's container asks the agent's environment which pane it is in and is told.
A window is not there to be told, and three measurements say there is no walk
from an agent to its window at all
(macOS 26, aerospace 0.20.3, Ghostty 1.3.1):

- **Nothing in a shell's environment names a window.** Ghostty hands down
  `GHOSTTY_BIN_DIR`, `GHOSTTY_RESOURCES_DIR` and `TERM_PROGRAM` and no id of any
  kind. Its CLI will not help either — `ghostty +new-window` answers "not
  supported on this platform".
- **A pid names an application, not a window.** aerospace answers in those
  terms: `list-windows` reports an `app-pid` per window and two windows of one
  application carry the same one (measured: two TextEdit windows, one process,
  one pid under both). So a pid narrows the list to one application and no
  further.
- **Inside a multiplexer the process chain does not even reach the terminal.**
  An agent's ancestry ends at the zellij *server*, whose parent is 1; the client
  that draws it is in a different process tree entirely, under the terminal.
  This is the ordinary case, not a corner of one.

So the window is **found**, every time it is needed, from two keys:

| key | where it comes from | when it is exact |
| --- | --- | --- |
| the process chain | walked inside the agent, by `capture-environment` | when the nearest ancestor that owns a window owns exactly one |
| a window-title prefix | a template, filled in from the agent's environment | when the title is specific to this session |

Neither is sufficient alone and together they are better than either: the chain
narrows to one application, the title picks the window out of it.

## The title key

```toml
[integration.aerospace-container.settings]
title = "{ZELLIJ_SESSION_NAME} | "   # the default
```

zellij titles its terminal window `<session> | <active tab>`, so the session
name the agent already carries is an index into the window list. The separator
is part of the key and is what makes it worth trusting: `home` would match any
window whose title happens to start that way, `home | ` is a zellij client
attached to the session called home.

It is configuration rather than code because the coupling belongs to whoever set
those two programs up, and because the next multiplexer should be a line in a
file rather than a release. What it needs is a multiplexer that puts its session
name in **both** the environment and the window title; zellij does, and tmux —
which puts neither its session name nor anything else nameable in the
environment — would need a different key.

Set `title = ""` to turn it off, which is the right setting if you run no
multiplexer: the chain is then the only way in, and it is the exact one.

## Nothing about a window is ever stored

Every other coordinate in this system is a property of the session: a pane
belongs to its zellij session for as long as both exist. A window is not — it
merely *shows* a session, right now, and stops the moment somebody detaches. A
stored window id would be a cached guess rather than a coordinate.

So `interpret-environment` here asks aerospace nothing and only shapes the keys,
and the lookup happens in `focus` and `focused`, where the answer is used in the
same millisecond it is obtained. A listing costs about 12ms, which is what makes
asking every time affordable.

## Failures are words, not sentences

`place-is-gone` (the key matches nothing — nothing is showing this session, so
the walk stops before the inner container focuses a pane nobody can see),
`ambiguous` (several windows could be it and nothing tells them apart — going to
one at random is wrong half the time and looks exactly like success),
`not-running` (aerospace is not there to ask), `never-placed` (this session has
no key at all, which is skipped rather than failed, so the layers inside still
run), `refused` (aerospace was asked, understood, and would not).

## Two things about aerospace worth knowing

Both measured on 0.20.3, both load-bearing:

- **`focus --window-id` is the whole job.** A window on another workspace comes
  with its workspace, and focusing the window that is already focused exits 0
  rather than complaining — unlike zellij's `focus-pane-id`, which exits 2 at
  the same request. There is nothing to ask first and nothing to make idempotent
  by hand.
- **It tells the truth with its exit code.** A window id it does not know exits
  1 and says "Invalid \<window-id\> 34 passed to --window-id"; so does a usage
  mistake. There is no stderr vocabulary to keep here, and nothing here reads
  one.

## Testing

```
env -u GOROOT go test ./...
```

The whole of the interesting behaviour — which window, and whether there is an
answer at all — is a pure function of a window list, so it is tested on a
machine with no window manager on it. The subprocess half runs against a
stand-in that answers the way aerospace answers. The process walk asks the real
kernel, because the point of it is that it agrees with the kernel.

What no test covers is whether a focus actually **moved** anybody. That one is
demonstrated by a person watching their own screen: focus something on another
workspace, run `agent-notify focus-session <session>`, and watch the window come
to you.

## Building

Go 1.26, macOS. Core is not published yet, so `go.mod` has a `replace` pointing
beside this repository; that line comes out when core is.
