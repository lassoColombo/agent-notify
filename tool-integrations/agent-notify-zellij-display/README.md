# agent-notify-zellij-display

zellij's **display** integration for [agent-notify](../../agent-notify): it
paints what every agent on your machine is doing onto zellij's pane and tab
titles, and it does nothing else.

```
  pane 3   ↻ monomodules      working
  pane 5   ▢ lenny            finished a turn, your move
  pane 7     notes-8e         idle — a quiet agent shows its name and nothing else
  tab  1   ▲ work             the most urgent thing inside it
```

(The glyphs that ship are Nerd Font marks; the ASCII above is for a README.)

It is a long-lived process. It connects to agent-notify's session-watcher once,
says it is a `display`, and is handed the whole current state every time
something it cares about moves. It never talks to an agent, no agent ever waits
on it, and if it dies nothing else notices.

Its sibling, [agent-notify-zellij-container](../agent-notify-zellij-container),
is the other half of zellij: the one that can take your cursor somewhere. They
are two programs on purpose — a display writes titles, a container **moves your
focus**, and wanting the first is not the same decision as allowing the second.
Install either, both, or neither; each captures for itself the two environment
variables it needs, so neither depends on the other being there. Installing both
is the ordinary case, and they never talk to each other.

---

- [What a title says](#what-a-title-says)
- [Installation](#installation)
  - [Prerequisites](#prerequisites)
  - [Build it](#build-it)
  - [Register it with core](#register-it-with-core)
  - [Who starts it](#who-starts-it)
  - [Check it](#check-it)
- [Configuration](#configuration)
  - [Where the file is](#where-the-file-is)
  - [The integration's own table](#the-integrations-own-table)
  - [`[integration.zellij-display.settings]`](#integrationzellij-displaysettings)
  - [Glyphs](#glyphs)
  - [Environment](#environment)
  - [What is deliberately not configurable](#what-is-deliberately-not-configurable)
- [Commands](#commands)
- [What it does, exactly](#what-it-does-exactly)
- [Two facts about zellij worth knowing](#two-facts-about-zellij-worth-knowing)
- [Testing](#testing)
- [What is not finished](#what-is-not-finished)

---

## What a title says

A pane holding a live agent says `<glyph> <name>`:

```
↻ monomodules
▲ agent-notify-8e
```

The glyph is the state, resolved from your glyph table over this display's
defaults. The name is whatever the agent calls that session, as its
agent-integration reported it; when nobody named it, core's own fallback applies
— the last component of the working directory plus two characters of the session
id, so that three sessions open in one repository are not all called the same
thing. That rule is core's (`Record.DisplayName`), not this program's, so the
name on a pane is the same string `agent-notify list` prints and the same string
`agent-notify focus-session` matches on.

A tab says `<glyph> <its own name>`, where the glyph is the **most urgent** agent
anywhere in that tab — the same `max(rank)` an LED over the whole machine
computes, out of the same function. The name is yours: it is recovered from
whatever the tab says right now, minus any glyph this configuration would itself
have written, never remembered. So renaming a tab by hand keeps your new name,
and restarting this program does not stack a second glyph on it.

A pane whose session has **ended** is handed back to zellij's own title, and a
tab with nothing of ours left on it goes back to its own name.

## Installation

### Prerequisites

- **zellij**, and a version whose `zellij action rename-pane` takes `--pane-id`.
  Without that flag a display can only rename the focused pane, which is useless
  — the pane that needs a glyph is by definition the one you are not looking at.
  Everything here was measured on zellij 0.45.1.
- **agent-notify core**, built and on your PATH, with its session-watcher able to
  run.
- **Go 1.26** to build (`.tool-versions` pins 1.26.2).
- A **Nerd Font** in your terminal, if you want the default glyphs to render as
  anything but boxes. If you have not got one, set your own glyph table — see
  [Glyphs](#glyphs).

### Build it

Core, the agents and the tools live in **one repository**. Clone it once and
build this module from its own directory:

```sh
git clone <the agent-notify monorepo> ~/projects/agent-notify
cd ~/projects/agent-notify/tool-integrations/agent-notify-zellij-display

go build ./...                      # produces ./agent-notify-zellij-display
go install .                        # or put it in $GOBIN, which is simpler below
```

`go.mod` carries `replace github.com/lassoColombo/agent-notify => ../../agent-notify`,
which is why the module is built **inside the monorepo** and not out of a clone
of its own. Nothing is published to a remote yet; the `replace` line comes out
when core is published (plan.md M18).

If your shell exported `GOROOT` from an outer context it overrides the toolchain
pinned in `.tool-versions`. `env -u GOROOT go build ./...` is the fix, and the
same applies to `go test`.

The binary must end up somewhere core can name it by an **absolute path** — see
below for why.

### Register it with core

```sh
agent-notify install zellij-display
```

`agent-notify install <name>` is a dispatcher and nothing else: it execs
`agent-notify-zellij-display install`, because core does not know and must not
know which environment variables zellij puts in a pane. The subcommand takes no
options; it **prints a table and writes nothing**:

```toml
[integration.zellij-display]
binary = "/Users/you/go/bin/agent-notify-zellij-display"

[integration.zellij-display.settings]
zellij = "/opt/homebrew/bin/zellij"
```

The table alone goes to stdout and every word of explanation to stderr, so when
you have decided you can simply redirect it:

```sh
agent-notify install zellij-display >> ~/.config/agent-notify/config.toml
```

Putting it there is yours to do, and that is the design rather than an omission:
`enabled` defaults to true, so **the table being present is what turns this on**.
An install that wrote the table would not have configured the display, it would
have switched it on for you. Your comments and your ordering in that file survive
because nothing but you ever writes it.

Two of the three lines are facts about this machine that the program resolves for
you and you should not retype:

- **`binary`** is `os.Executable()` — this binary's own absolute path. A
  PATH of a program core starts is not your shell's (a launchd job's is
  `/usr/bin:/bin` and nothing else), which is how a display can be perfectly
  correct and never once be started.
- **`zellij`** is `exec.LookPath("zellij")`, resolved **here**, at install time,
  because install runs in your shell where the answer is simply available and
  everything that runs later does not. If zellij was not on your PATH when you
  ran the install, the line is printed empty with a comment saying so and the
  command exits 1: a table that looks complete and is not is worse than one that
  says what is missing.
- **There is no line asking to be run inside the agent**, and there used to
  be. Only a process running in there can see which pane it is in, so the hook
  runs `agent-notify-zellij-display capture-environment` and stores what it
  returns, opaquely, under this integration's name — and it does that for every
  integration it can run, so there is nothing left to ask for. One that reads
  nothing answers an empty object and core records nothing for it.

**Which** variables get captured is not in your config file and is not yours to
set. They are `ZELLIJ_SESSION_NAME` and `ZELLIJ_PANE_ID`, they live in this
program's source where the code that reads them and the code that reads them back
both already are, and a second copy in a file you edit is a copy that can
disagree — silently, because a capture naming the wrong variable produces no
error at all, just a display that paints nothing. If you still have an old
`capture = ["ZELLIJ_SESSION_NAME", "ZELLIJ_PANE_ID"]` line, delete it.

### Who runs it

The session-watcher does, one render at a time, when something this display
watches moves. It asks every configured program what it answers — `capabilities`
— and runs the ones that say `render`, so adding the table is the whole act of
turning this on and deleting the table is the whole act of turning it off.

Nothing about that is inferred from the table. What used to decide it was the
program's ABSENCE from `[container] order`, which is a fact about how your
shells nest standing in for a fact about the program, and it was wrong in both
directions.

```sh
agent-notify watcher reload      # pick up the table you just added
agent-notify watcher status
agent-notify watcher restart     # after rebuilding the binary
```

Nothing is kept running. The session-watcher runs this program when something it
watches moves and the program exits, so there is no child to restart, no failure
count and nothing to forgive: a render that failed is repaired by the next one,
which paints the whole world again.

A rebuilt binary **is** picked up, because the next render is a new process — but
what it answers is not. `capabilities` is asked at startup and on reload, so a
rebuild that changes the wake list needs `agent-notify watcher reload`.

You can also just run it in a terminal, which is the fastest way to see what it
is doing, since it logs to stderr:

```sh
agent-notify-zellij-display
# time=... level=INFO msg=painting zellij=/opt/homebrew/bin/zellij timeout=2s
```

### Check it

```sh
agent-notify doctor
```

The two lines to look for:

```
config       ok    2 agent(s), 3 integration(s), keep-ended-sessions 168h0m0s
integrations ok    reported by pid 41022 at 2026-09-23T08:14:02Z
             zellij-display         connected, pid 41185 [display]
```

`connected` means the watcher started it **and** it completed the handshake — the
watcher asks the kernel for the peer pid on the socket, so "connected" is about
the child it started and not about anything that merely calls itself
`zellij-display`. Other states you may see are `starting`, `started, not yet
connected`, `gave up` (with the reason, and `agent-notify watcher reload` as the
retry), and `connected, not ours` for a copy you started yourself.

If the program is on your PATH and nowhere in your config file, doctor says so
under `installed` and does not call it a failure: it is a decision you have not
made, not a fault.

Then look at a pane. `agent-notify tail` shows the state changes going past, and
a title that does not follow one of them is the thing to report.

## Configuration

### Where the file is

`~/.config/agent-notify/config.toml`, or `$XDG_CONFIG_HOME/agent-notify/config.toml`,
or `$AGENT_NOTIFY_ROOT/config.toml` when that variable is set. This program never
works the path out for itself — it asks core, because a path derived twice is a
path that will differ once, and the failure is silent.

There is no file by default and running core without one is supported; this
display, however, cannot run without its table, because the `zellij` setting is
required and nothing can guess it.

### The integration's own table

Everything here is read by **core**, which runs the program:

| Key | Type | Default | What it does |
| --- | --- | --- | --- |
| `enabled` | boolean | `true` | `false` stops the watcher running it, without you deleting the table. Absence means yes: writing the table is how you ask for the integration. |
| `binary` | string | none | The program to run, and naming one is what says core may run it. Looked up on PATH unless absolute — **make it absolute**. |

A key core does not declare is reported by name, and the rest of the file
survives it.

### `[integration.zellij-display.settings]`

Everything under `settings` belongs to this integration alone; core hands it over
verbatim and never looks inside.

| Key | Type | Default | What it does |
| --- | --- | --- | --- |
| `zellij` | string | none — **required** | Absolute path to the zellij binary. Checked at startup: unset, relative, or not on disk are three distinct errors, each naming the key and what to do. |
| `glyphs` | table of string → string | see below | What each state looks like, keyed on `kernel` or `kernel/detail`, layered over this display's defaults. |

There is no PATH search and no fallback for `zellij`, and the absence is the
design. This program is started by the session-watcher, whose PATH is not your
shell's, so a lookup performed *here* is a lookup performed in the one context
that cannot answer it. The list of Homebrew prefixes that used to sit at that
line was an attempt to guess what the environment would not say.

A key nobody declared is **refused by name** rather than ignored:

```
[integration.zellij-display.settings]: 1 key(s) nobody declared:
  glyph (1, 1) is not in Settings
```

That is deliberate. A misspelled glyph that changes nothing and says nothing is
the config bug people give up on.

### Glyphs

```toml
[integration.zellij-display.settings.glyphs]
working                            = "↻"
"working/compacting"               = "~"
"blocked-on-you/permission-prompt" = "?"
idle                               = ""     # empty is a choice, not an omission
```

Keys are resolved most specific first: the exact `kernel/detail` pair, then the
`kernel`, then — for a state this build of the display has never heard of — the
nearest known rank, so a future state is painted like the state nearest to it in
urgency rather than not at all. Your table is layered over the defaults, one key
at a time; you never have to restate the ones you are happy with.

The defaults are Nerd Font marks from Font Awesome's BMP block, chosen so the
states are told apart by **shape**, because a zellij title carries no colour and
shape has to do the whole job:

| State | Codepoint | What it is |
| --- | --- | --- |
| `blocked-on-you` | `U+F071` | a warning triangle — it needs a hand |
| `broke` | `U+F00D` | a cross — the turn died |
| `finished-a-turn` | `U+F075` | a speech bubble — it is talking to you |
| `working` | `U+F021` | circular arrows — turning, in progress |
| `idle` | *(empty)* | a quiet agent shows its name and nothing else |
| `ended` | *(empty)* | a session that has ended shows nothing at all |

If you write your own glyphs into the config file, know that those default
codepoints live in the Unicode Private Use Area and a great deal of tooling —
editors, clipboards, multiplexers, JSON pretty-printers — quietly drops them.
The implementation before this one pasted them in as literals and ended up with
a six-entry table of empty strings and a display where every state looked the
same. In this program's source they are written as escapes for that reason; in
your TOML, paste from something you trust and check the result.

One consequence worth knowing: your glyphs are also how this display recognises
its own leftovers. `Marks()` is every non-empty glyph the table can paint,
longest first, and stripping a tab name means removing any prefix this
configuration would itself have written. A tab whose name genuinely starts with
a warning triangle loses it. That is the price of not keeping a sidecar file of
what we painted.

### Environment

| Variable | Read by | Effect |
| --- | --- | --- |
| `AGENT_NOTIFY_ROOT` | core, in this process | Moves the state directory, the runtime directory and the config file together. Set it to run an isolated instance. |
| `XDG_CONFIG_HOME` | core, in this process | Where `agent-notify/config.toml` is looked for. |
| `ZELLIJ_SESSION_NAME` | `capture-environment`, inside the agent | Which zellij session the agent is in. |
| `ZELLIJ_PANE_ID` | `capture-environment`, inside the agent | Which pane. Parsed as an integer; addressed back as `terminal_<id>`, because ids are unique per kind and `terminal_0` and `plugin_0` are two different panes. |

The last two are read **only** by the `capture-environment` subcommand, which
runs as a child of the agent's hook. A record with nothing captured is not an
error and not a warning: an agent running in a bare terminal is a perfectly
ordinary session that this display has nothing to say about, and it costs
nothing.

### What is deliberately not configurable

- **The zellij timeout.** One zellij invocation is bounded at `2s`
  (`ZellijTimeout` in `settings.go`), and on expiry this render gives up on that
  zellij session and the next change tries again. It is a constant and not a
  setting because nobody editing a config file knows better than this how long
  `zellij action list-panes` should be allowed to take, and the answer does not
  vary by machine in a way a person could act on.
- **Which environment variables are captured**, for the reason above.
- **Which record fields wake this display.** It asks for `kernel`, `detail`,
  `rank`, `name`, `cwd` and `captured_context`, which is exactly what a title can
  show — plus the one nobody would guess, `captured_context`, because that is
  where the pane is recorded and a session that moves has to repaint somewhere
  new. The message, the sequence and what the agent last said change many times
  in a turn and change nothing on a title bar. A test walks one record field at a
  time, plans twice, and fails if a different set of renames comes out for
  something not on that list, so the declaration cannot drift from the render.
- **The shape of a title.** It is `<glyph> <space> <name>`, with the space
  dropped when either half is empty.

## Commands

| Invocation | What it does |
| --- | --- |
| `agent-notify-zellij-display` | Stay connected to the session-watcher and paint. This is the usual way to run it, and what the watcher runs. |
| `agent-notify-zellij-display render` | Paint once and exit, from the view on stdin — or, with nothing on stdin, straight from the store. |
| `agent-notify-zellij-display capture-environment` | Print `{"ZELLIJ_SESSION_NAME":"…","ZELLIJ_PANE_ID":"…"}` and exit. Run by the hook, not by you. |
| `agent-notify-zellij-display capabilities` | Print what this program answers. Run by the session-watcher, not by you. |
| `agent-notify-zellij-display install` | Print the config table. Writes nothing, takes no options. |
| `agent-notify-zellij-display --help` | The same list. |

`render` is one paint and nothing else — no socket, no waiting — which is what
lets something else decide when painting happens.

The view comes in on stdin because a render is not only about the sessions that
exist. This display owns panes it did not create, and the only thing that
remembers which pane a finished agent had is that agent's ended record, so a
process started fresh for one render knows exactly what it was told and nothing
more.

Run by hand there is nothing on stdin, so it reads the store instead: the cold
path, what you want after restarting zellij itself and when asking "why does
that pane say that". That one cannot give a pane back, because the store's live
sessions are all it can see.

## What it does, exactly

Every render reads the panes of each zellij session it has agents in, plans the
difference, and runs only that:

```
zellij --session home action list-panes --json --tab
zellij --session home action rename-pane --pane-id terminal_3 "↻ monomodules"
zellij --session home action rename-tab-by-id 1 "▲ work"
zellij --session home action undo-rename-pane --pane-id terminal_5
zellij --session home action undo-rename-tab --tab-id 2
```

- **It plans against what zellij says now**, not against what it last wrote. That
  costs one subprocess per render — about 15ms, and renders only happen when a
  state actually moves — and buys three things memory cannot: it is correct after
  its own restart, it is correct after a pane moves to another tab, and it never
  issues a rename that would change nothing.
- **`--session` goes before the subcommand**, because it is a flag of zellij
  itself. Without it, a machine with two sessions renames a pane in whichever one
  zellij picks.
- **A pane whose session ended is handed back**, and only if the title is still
  one of ours — it starts with one of our glyphs, or it is exactly the name we
  would have written for a state whose glyph is empty. This display asks the
  watcher for ended sessions it will never draw for precisely this reason: it
  owns a piece of a UI it did not create, and the record is the only thing that
  remembers which piece.
- **Two sessions claiming one pane**: the more urgent one wins, and the records
  are sorted inside the planner rather than trusted from the caller, so the same
  set in a different order plans the same commands.
- **A pane that is gone while its session is not** is skipped, silently. Whether
  a session is alive is core's call, made from the agent's process; a display
  that started concluding things from a missing pane would eventually disagree
  with it.
- **Plugin panes are not panes**, and are never renamed.
- **Anything it did not paint, it does not touch**: panes with no agent, tabs with
  no agent, and every other zellij session are left alone. One zellij session
  failing to answer says nothing about the others and nothing at all about the
  agents, so it is logged at debug and skipped.

## Two facts about zellij worth knowing

Both measured on zellij 0.45.1, both load-bearing:

- **The exit code cannot answer "is that session there".** Asking for a session
  that does not exist exits **0** when some other detached session happens to be
  alive — writing the list of sessions that *do* exist where the JSON should be —
  and exits **1** when none is. A status that depends on unrelated state is not a
  signal, so the parse decides instead. (A missing *pane* inside a live session
  does exit 2, reliably; that is a different question.) This is also why every
  render reads before it writes: a failed read means not one rename is attempted
  against that session.
- **`rename-pane --pane-id` is what makes this possible.** Without it a display
  can only rename the focused pane, and the pane that needs a glyph is the one
  you are not looking at.

The JSON from `list-panes` is decoded into five fields — id, `is_plugin`, title,
tab id, tab name — and the rest of that record is real and ignored, so the next
zellij release that adds a field does not break this.

## Testing

```sh
env -u GOROOT go test ./...
```

The planner is pure: what should be renamed, and to what, is a table of records
and a table of panes in, and a list of commands out, so two agents in one tab, a
pane that moved, a session that ended and a tab you renamed underneath us all
test without a zellij anywhere. The rest runs against a real one — a detached
session, real panes, real renames, cleaned up afterwards, and skipped where there
is no zellij to talk to. `TestPanesCarryLiveStateGlyphs` is the whole chain with
only the agent replaced by a script: a real zellij, real panes, the real
subscriber lifecycle and the real render function.

## What is not finished

- **Nothing is published.** There is no release, no remote, no tap and no
  installable artifact: core, this display and everything beside them are one
  repository you clone and build. The `replace` in `go.mod` is the visible end of
  that, and it comes out with M18.
- **Linux is untested here.** Nothing in this program is macOS-specific, but the
  liveness work underneath it is verified on macOS only (plan.md
  M17).
- **`doctor` does not check this display's own settings.** It will tell you the
  process is connected; it will not tell you that your `zellij` path stopped
  existing. Running the binary in a terminal will, immediately and by name.
