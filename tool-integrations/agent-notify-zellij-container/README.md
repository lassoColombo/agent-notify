# agent-notify-zellij-container

zellij's **container** integration for [agent-notify](../../agent-notify): it
works out which pane a session is in, brings that pane to the front, and says
whether you are already looking at it.

Its sibling, [agent-notify-zellij-display](../agent-notify-zellij-display),
paints pane and tab titles and can do nothing else. They are two programs on
purpose: a display writes text, a container *moves your cursor*, and wanting the
first is not the same decision as allowing the second (plan.md D-37). Install
either, both, or neither.

## Installing

```
agent-notify install zellij-container            # prints what it needs; writes nothing
agent-notify install zellij-container >> ~/.config/agent-notify/config.toml
```

```toml
[integration.zellij-container]
binary              = "/opt/homebrew/bin/agent-notify-zellij-container"
capture-environment = true

[integration.zellij-container.settings]
zellij = "/opt/homebrew/bin/zellij"   # required, absolute; install resolves it

[container]
order = ["zellij-container"]
```

`capture-environment = true` is what tells the hook to **run** this program
rather than read variables itself. The hook has no socket and cannot ask
anything at connect time, so that one fact lives in the config file, written
there by this install (D-39).

`[container] order` is outermost first — a window manager before the multiplexer
inside it. If you already have that table, install says so and leaves your order
alone: which container is outside which is your fact, not this program's.

## What it does

It is not a daemon. Core runs it with a subcommand when it needs an answer, and
each answer is one JSON object on stdout (D-38):

| | when it runs | what it must not do |
| --- | --- | --- |
| `capture-environment` | inside the agent, on the hook path | ask zellij **anything** |
| `interpret-environment` | in the session-watcher, once per capture | — |
| `focus` | when somebody clicked | — |
| `focused` | when a notifier is about to speak | — |

The split between the first two is the whole design. `ZELLIJ_SESSION_NAME` and
`ZELLIJ_PANE_ID` exist only inside the agent's own process, so reading them has
to happen there — and that is the one place where `zellij action` against a
wedged server would be sitting in the path your agent waits on. So capture reads
and returns; interpretation asks zellij which tab holds the pane, later,
somewhere that is allowed to block.

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
  Asking first means asking the whole question, though. `is_focused` is per
  layer as well as per tab, so the focused **tiled** pane of a tab that is
  showing its floating panes is `is_focused` and not in front, and zellij takes
  `focus-pane-id` on it perfectly happily — exit 0, floating layer lowered. A
  guard that read `is_focused` alone therefore skipped the single focus that
  would have cleared the cover, and left people arriving at the right pane
  behind a floating one they had to close by hand.
- **The exit code cannot tell you a session is missing.** A missing session exits
  0 when another detached session happens to be alive and 1 when none is, so the
  parse decides. `list-clients` is the same trap in a different coat: asked about
  a session that does not exist it exits **0** and writes the list of sessions
  that do where its table should be, so there too the parse decides — the
  `CLIENT_ID` header is what separates "nobody is attached" from "that session is
  not there", and those two are a `zellij attach` and a stale record
  respectively. Every answer here reads before it acts, which is also what R17
  requires: coordinates are validated at the moment of use, because panes die
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
  this: the server routes it with the client id of whoever sent the action, and
  a CLI invocation is its own throwaway client, so your terminal stays where it
  is. Measured against a real attached client, from outside and from a pane
  inside the session, with and without `--pane-id`: exit 0, no output, nothing
  moves — the same silence whether the target session is missing, exited, or
  perfectly healthy. (Upstream asks for `zellij action --client-id`:
  [zellij-org/zellij#5624](https://github.com/zellij-org/zellij/issues/5624).)
  A jump into another session is therefore the **window manager's** job — focus
  the terminal window that session is attached to, and this container does the
  pane inside it (§A11.2, outermost first). Where no window shows it at all, the
  honest answer is `not-attached`.

## Failures are words, not sentences

Each one deserves a different next move, so each has a name: `place-is-gone`
(the pane went away — the session may still be alive elsewhere), `not-running`
(zellij itself is not there), `never-placed` (this session started somewhere
this container was not watching), `refused` (zellij was asked, understood, and
would not), and `not-attached` (the session is running and no terminal is
showing it, so there is no screen to bring anything to the front of — `zellij
attach <session>` and the same focus works).

`not-attached` is not one of core's words yet; it is declared here and core
carries a problem it has never heard of through untouched. It is the one the
other four could not cover: not gone, not absent, not a refusal, and its next
move belongs to nobody else.

## Testing

```
env -u GOROOT go test ./...
```

The focus rule is a table of panes and tabs and needs no zellij, and so is the
`list-clients` parse. The rest runs against a real zellij and skips itself where
there is none.

That includes the thing this file used to say no test could cover — whether a
focus **moved** anybody — because a client does not have to be a person. zellij
is a terminal emulator, so a pane of one session running `zellij attach` to
another is a real attached client: `list-clients` reports it, tabs become
`active`, and a focus has somebody to be performed for. The suite builds one and
asserts both halves — a session nobody is attached to is named `not-attached`
rather than reported as focused, and an attached one ends with that client's own
row pointing at the pane.

What is still left to a person watching their own terminal: whether the window
that client lives in was the one in front. That is the window manager's half of
the answer and this program cannot see it.

## Building

Go 1.26. Core is not published yet, so `go.mod` has a `replace` pointing beside
this repository; that line comes out when core is.
