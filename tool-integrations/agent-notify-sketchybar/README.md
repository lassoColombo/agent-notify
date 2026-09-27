<div align="center">
  <h1>agent-notify-sketchybar</h1>
  <p>
    The agent semaphore on your macOS menu bar, drawn with SketchyBar<br>
    One counter per state, a chip behind each one listing the sessions in it<br>
    A hover shows what that agent last said, and a click takes you to it
  </p>
</div>

```
  A blocked-on-you  1    A agent-notify  permission-pr…  now
  X broke           0
  O finished-a-turn 1    O lenny                         now
  ~ working         2    ~ agent-notify   2m | ~ nushell  1s
  . idle            0
```

Shown here as ASCII. What ships are Nerd Font marks, the colours are what
actually tell the states apart, and a state with nothing in it is not drawn at
all — it only keeps its place.

---

- [What this is](#what-this-is)
  - [Two menu-bar displays, and how to choose](#two-menu-bar-displays-and-how-to-choose)
- [Installation](#installation)
  - [Prerequisites](#prerequisites)
  - [Clone the monorepo and build](#clone-the-monorepo-and-build)
  - [Register it with core](#register-it-with-core)
  - [Nothing goes in your sketchybarrc](#nothing-goes-in-your-sketchybarrc)
  - [Start it and check it](#start-it-and-check-it)
  - [Running it by hand](#running-it-by-hand)
  - [Removing it](#removing-it)
- [Configuration](#configuration)
  - [Where the file is](#where-the-file-is)
  - [The table core reads](#the-table-core-reads)
  - [The settings table](#the-settings-table)
  - [Glyphs and colours](#glyphs-and-colours)
  - [The chip](#the-chip)
  - [The preview](#the-preview)
  - [What is refused, and when](#what-is-refused-and-when)
  - [The timings you cannot configure](#the-timings-you-cannot-configure)
  - [Environment](#environment)
- [What it puts on the bar](#what-it-puts-on-the-bar)
- [When something happens](#when-something-happens)
- [Hovering a row: what that agent last said](#hovering-a-row-what-that-agent-last-said)
- [Why it repaints on a timer](#why-it-repaints-on-a-timer)
- [Five things about sketchybar worth knowing](#five-things-about-sketchybar-worth-knowing)
- [Testing](#testing)
- [What is unfinished](#what-is-unfinished)

---

## What this is

This is a **display** integration for [agent-notify](../../agent-notify): it
subscribes to the session-watcher, is handed the state of every agent running on
your machine, and paints it onto [SketchyBar](https://github.com/FelixKratz/SketchyBar).
It never talks to an agent, it never writes to the store, and nothing an agent
does waits on it — a display's failure is its own.

What it draws is five counters, one per live state, in a fixed order by urgency:
`blocked-on-you`, `broke`, `finished-a-turn`, `working`, `idle`. Behind each
counter is a popup — a chip — listing the sessions in that state, most urgent
first, each with its name, its detail and how long it has been like that.
Clicking a row runs `agent-notify focus-session` and takes you there, through
every container the session is buried in; clicking the counter itself takes you
to the most urgent session in it, which is what you meant nine times in ten.
Hovering a row fills a scrollable preview under the chip with the agent's last
message.

Ended sessions are never drawn, and there is no counter for them.

### Two menu-bar displays, and how to choose

[`agent-notify-macos-bar`](../agent-notify-macos-bar) does the same job on the
same surface, natively, and the two are **alternatives**: run one, not both, or
you will have the semaphore twice.

|  | this | macos-bar |
| --- | --- | --- |
| needs | SketchyBar installed and running | nothing but macOS |
| the bar | five counters, one per state, each with its count | one `NSStatusItem`, one mark per live state, no numbers |
| the list | a popup chip per counter, rows drawn by this program | a real `NSMenu` with section headers |
| the agent's words | a scrollable preview under the hovered row | not shown; that is what the notifications display is for |
| built out of | argv handed to the `sketchybar` client | cgo and AppKit, in a `.app` bundle |

The short of it: if you already run SketchyBar and want the semaphore to live
inside the bar you already built, this is the one. If you do not, macos-bar puts
it on the menu bar with nothing to install first.

Either can sit alongside
[`agent-notify-macos-notifications`](../agent-notify-macos-notifications),
which interrupts you rather than drawing anything. Several displays at once is
the ordinary case: they never talk to each other, they are each handed the same
state, and each has its own table in the config file.

## Installation

### Prerequisites

- **macOS.** SketchyBar is macOS-only and so, therefore, is this.
- **SketchyBar itself**, installed and running. Everything here was measured
  against v2.24.0.

  ```sh
  brew tap FelixKratz/formulae
  brew install sketchybar
  brew services start sketchybar
  sketchybar --query bar | head      # it answers ⇒ it is running
  ```

- **A Nerd Font in your bar's font configuration.** The default glyphs are
  private-use codepoints from Font Awesome's BMP block, and a font without them
  draws nothing. Either set a Nerd Font as your bar's `icon.font`, or override
  every glyph with something your font has — see
  [Glyphs and colours](#glyphs-and-colours).
- **Go 1.26 or newer**, which is what `.tool-versions` pins. If your shell
  exported a `GOROOT` from somewhere else it will override the pinned
  toolchain; `env -u GOROOT go build ./...` is the fix, and it is why every
  command below is written that way.
- **agent-notify core**, built and on your `PATH`, because this integration is
  registered through it and every click runs it.

### Clone the monorepo and build

Core, the agent-integrations and the tool-integrations are **one repository**.
There is nothing to clone per integration, and nothing is published to a remote
yet: this module's `go.mod` carries a `replace` pointing at core's directory in
the tree, so it builds beside core and nowhere else.

```sh
git clone <the monorepo> ~/projects/agent-notify
cd ~/projects/agent-notify

# core: the CLI and the session-watcher
cd agent-notify && env -u GOROOT go install ./cmd/agent-notify

# this display
cd ../tool-integrations/agent-notify-sketchybar && env -u GOROOT go install .
```

`go install` without a version argument works inside the module and honours the
`replace`; `go install github.com/lassoColombo/...@latest` does not and will not
until core is published.

**Build it into the place you mean to keep it before you go any further.** The
registration step below prints the absolute path of the binary that printed it,
so a binary that later moves leaves a config line pointing at nothing. `go
install` puts it in `$GOBIN`, or `$GOPATH/bin`, or `~/go/bin` — check with `go
env GOBIN GOPATH` — and that directory has to be on your `PATH` for
`agent-notify install sketchybar` to find it.

```sh
which agent-notify agent-notify-sketchybar
```

### Register it with core

`agent-notify install <integration>` is a dispatcher and nothing else: it execs
`agent-notify-<integration> install` and hands it the terminal. What this
integration's install does is **print the table it needs and change nothing**.

```sh
agent-notify install sketchybar            # or: agent-notify-sketchybar install
```

```toml
[integration.sketchybar]
binary = "/Users/you/go/bin/agent-notify-sketchybar"

[integration.sketchybar.settings]
sketchybar = "/opt/homebrew/bin/sketchybar"
```

Every word of explanation goes to stderr and the table alone to stdout, so
somebody who has already decided can redirect it:

```sh
agent-notify install sketchybar >> ~/.config/agent-notify/config.toml
```

Two values in there are things the program knows and you would have to go and
look up. `binary` is this program's own resolved path. `sketchybar` is wherever
`sketchybar` is **on the PATH of the shell you ran install from**, which is the
only context that can answer the question: the session-watcher starts this
program later, and a supervised child's `PATH` is not yours. There is
deliberately no search and no `PATH` fallback at run time. If `sketchybar` was
not on your `PATH` when you ran install, the line is printed empty with a
comment saying so and the command exits 1 — fill it in by hand.

The table being present is what turns the display on. `enabled` defaults to
true, so writing the table **is** the ask, which is why install refuses to write
it for you: only you can know whether you want this running.

### Nothing goes in your sketchybarrc

This is the part that differs from every other SketchyBar item you have. You do
not add items, you do not add a plugin script, and you do not `--reload`
anything on install.

The session-watcher starts this program; it puts its own items on the bar when
it connects and takes them off again when it stops. A counter left behind would
say "two agents are working" for ever, and there is no way for a person to tell
a stale number from a current one — an empty bar is honest, a frozen one is not.

The one thing your `sketchybarrc` is good for here is **saying where the
counters should sit**. SketchyBar orders a side by the order things were added
to it, and this display connects long after your config has finished running, so
by default its items land at the far end of whichever side they are on. Naming a
neighbour is the only way to say "here" rather than "last":

```toml
[integration.sketchybar.settings]
before = "sep_agents"   # an item your own sketchybarrc added
```

Naming an item that does not exist costs one complaint from sketchybar in the
log; the render still lands, in the place it would have anyway.

### Start it and check it

The watcher supervises the integrations listed in the config file, so it has to
re-read it:

```sh
agent-notify watcher reload     # re-read the config without restarting
agent-notify watcher status     # is it running, since when, does its socket answer
```

Then, in order of what breaks:

```sh
agent-notify doctor                                  # where everything resolves to
agent-notify list                                    # is anything running to draw
sketchybar --query sketchybar.working                # does the bar have our items

# what the display is saying
tail -f "$HOME/Library/Application Support/agent-notify/agent-notify.log"
```

The log is the place to look, because a supervised daemon has no terminal to
complain to. This display logs `painting` with the sketchybar path and the
refresh interval when it starts, and logs every `[!]` line sketchybar answers
with.

If you have no agents running, every counter is drawn `off` and the bar looks
exactly as it did before — which is correct, and indistinguishable from broken.
Start an agent, or run `agent-notify list`, before concluding anything.

### Removing it

Delete `[integration.sketchybar]` and its settings from the config file and
`agent-notify watcher reload`. The display takes its own items off the bar as it
stops, so there is nothing to clean up by hand. Nothing else in the file
changes, and no other integration notices.

## Configuration

### Where the file is

One file, `config.toml`, resolved by core and never by this program:

| | path |
| --- | --- |
| default | `~/.config/agent-notify/config.toml` |
| `XDG_CONFIG_HOME` set | `$XDG_CONFIG_HOME/agent-notify/config.toml` |
| `AGENT_NOTIFY_ROOT` set | `$AGENT_NOTIFY_ROOT/config.toml` |

There is no file by default and running without one is supported everywhere in
agent-notify — but not here: this display needs at minimum to be told where
sketchybar is, and it refuses to start without it.

`AGENT_NOTIFY_ROOT` moves the state directory, the runtime directory and the
configuration file beneath one root, which is what makes running an isolated
instance one step rather than three. It is the only environment override in the
system, and this program reads no environment variable of its own.

### The table core reads

`[integration.sketchybar]` belongs to core: it is how the watcher knows to start
this program at all.

| key | type | default | what it does |
| --- | --- | --- | --- |
| `binary` | string | — | the program to supervise. Absolute, or a bare name looked up on the watcher's `PATH` — which is not your shell's, so prefer absolute. `install` prints the resolved one. |
| `enabled` | bool | `true` | `false` keeps the table and stops the program being started. Absence means enabled: writing the table is the ask. |
| `capture-environment` | bool | `false` | **not for this display.** A bar does not care where a session lives. Do not set it. |

Core also has one top-level key this display leans on, though it rarely needs
writing:

```toml
agent-notify-binary = "/Users/you/go/bin/agent-notify"
```

A chip has to be able to run something when it is clicked, and the thing it runs
is `agent-notify focus-session`. This display resolves that path **at startup**
and refuses to start if it cannot, rather than discovering the problem when
somebody clicks. Empty means "work it out", which succeeds when `agent-notify`
is on the watcher's `PATH`.

### The settings table

Everything below lives under `[integration.sketchybar.settings]` and belongs to
this integration alone. Core never looks inside it, and **a key nobody declared
is refused by name**, because a misspelled colour that changes nothing and says
nothing is the config bug people give up on.

```toml
[integration.sketchybar.settings]
sketchybar = "/opt/homebrew/bin/sketchybar" # required, absolute
position   = "left"
rows       = 8
before     = "sep_agents"
announce   = "8s"
```

| key | type | default | what it does |
| --- | --- | --- | --- |
| `sketchybar` | string | — | **required.** Absolute path to the sketchybar client. Named after the tool rather than `binary` because the table above it already has a `binary` — this program's — and install prints both together. |
| `position` | string | `"left"` | where the counters sit. Handed to sketchybar without interpretation, so `left`, `right`, `center`, `e` and `q` all mean what they mean there. |
| `rows` | int | `8` | how many sessions a chip lists before its last row says `and n more`. Values below 1 are ignored and the default stands. |
| `before` | string | `""` | put the counters immediately before an item of yours. |
| `after` | string | `""` | put them immediately after one. Naming both is an error. |
| `announce` | duration | `"8s"` | how long a state change lights the bar up. `"0s"` turns the announcement off entirely and leaves you with counters that only count. Negative, and anything that is not a duration, is an error. |

`announce` is written the way a person writes a duration — the same spelling as
every other duration in this system, because this display imports core's type
rather than growing one of its own (D-78). A bare number is refused, since
`announce = 8` would otherwise be eight nanoseconds rather than the eight
seconds whoever wrote it meant.

An absent key and `"0s"` are different answers: absent takes the eight-second
default, `"0s"` is a request for a bar that stays passive.

### Glyphs and colours

Two tables, both optional, both layered over the defaults this display ships.
The keys are states — a kernel, or a `kernel/detail` pair, most specific first:

```toml
[integration.sketchybar.settings.glyphs]
working = "~"

[integration.sketchybar.settings.colors]
working                            = "0xff31748f"
"blocked-on-you/permission-prompt" = "0xffeb6f92"
```

| state | default glyph | default colour |
| --- | --- | --- |
| `blocked-on-you` | ``, a warning triangle | `0xffeb6f92` — Rosé Pine love |
| `broke` | ``, a cross | `0xfff6c177` — gold |
| `finished-a-turn` | ``, a speech bubble | `0xff9ccfd8` — foam |
| `working` | ``, circular arrows | `0xff31748f` — pine |
| `idle` | ``, a dot | `0xff6e6a86` — muted |

Colours are sketchybar's own `0xAARRGGBB`. A state this build has never heard of
— core gaining a new one — is painted the way the nearest state by rank is
painted, rather than not at all.

There is no separate table for the highlight an announcement draws: it is the
state's own colour at a different alpha, because a highlight that did not match
the glyph beside it would be a second vocabulary to learn.

### The chip

`[integration.sketchybar.settings.popup]` is how the popup behind a counter is
drawn. Three values rather than sketchybar's twenty, because the rest are either
decided by the content or things nobody changes.

| key | type | default | what it does |
| --- | --- | --- | --- |
| `background` | colour | `0xf2191724` | the chip's own background. Near-opaque on purpose: a bar can be frosted and beautiful at 1% alpha, a panel of text over a terminal cannot, and what you get instead is two documents printed on top of each other. |
| `border` | colour | `0xff524f67` | `""` means no border rather than a black one. |
| `radius` | int | `10` | corner radius. `0` is a real request — square corners — which is why an absent key and a zero are distinguishable. |

### The preview

`[integration.sketchybar.settings.preview]` is the agent's last message, shown
under the chip's rows when you hover one.

| key | type | default | what it does |
| --- | --- | --- | --- |
| `lines` | int | `6` | how many lines are **drawn** at once, the session's name included. Minimum 2: one for the name and one to read. |
| `depth` | int | `40` | how many lines are **written** to the bar — how far a scroll can go. Must be at least `lines`. About a screenful and a half of an answer, which is as much as anybody reads off a menu bar. |
| `width` | int | `64` | characters per line, which is where the message is wrapped. Minimum 16. |
| `text` | colour | `0xffe0def4` | what the agent's words are drawn in. The name on top is drawn in the state's own colour instead, so a preview says which semaphore it belongs to without a second glance. |

`lines` and `depth` are two different numbers on purpose, and conflating them is
the bug the shape exists to prevent — see
[Hovering a row](#hovering-a-row-what-that-agent-last-said).

**There is no way to turn the preview off from the config.** The renderer knows
how to draw chips with nothing under them — counters and rows alone — but the
only values that mean "no preview" are below the minimums above, and those are
refused at startup. The smallest preview you can actually ask for is `lines =
2`, which is the name and one line of what was said.

One thing follows from having a preview at all, and it is invisible: this
display asks the session-watcher to wake it for `kernel`, `detail`, `rank`,
`name`, `cwd` and `state_since` always, and for `message` **only when a preview
is configured**. A display woken for something it does not render is a repaint
per prompt for nothing; one that renders something it is not woken for shows it
stale for ever.

### What is refused, and when

Everything that can fail is done once, at startup, where it can still be
reported to a person. The program prints the complaint and exits 1.

- `sketchybar` unset, not absolute, or not there.
- `before` and `after` both named — the counters can only be in one place.
- `announce` negative, or text that is not a duration.
- `preview.lines` under 2, `preview.depth` under `preview.lines`, or
  `preview.width` under 16.
- a key in the settings table that nobody declared, named in the message.
- no runnable `agent-notify` to give the chips something to run on a click.

### The timings you cannot configure

Four durations govern this display and none of them is a setting. Every one of
them is mechanism — how often to repaint, how long to wait on a subprocess —
and a person editing a config file has no information with which to choose a
better value than the code does. They are named constants beside the code they
bound.

| constant | value | what it bounds |
| --- | --- | --- |
| `Refresh` | 30s | how often the bar is repainted with nothing new to say, so the ages on the chips stay true. The clock is shortened automatically while an announcement is live, so the paint that ends one lands when it is over rather than up to thirty seconds later. |
| `Timeout` | 3s | one `sketchybar` invocation. On expiry that paint is abandoned and the next change paints again. |
| `afterwards` | 100ms | how far past an announcement's deadline the paint that ends it lands, so that it does not race the deadline, find the announcement live by a microsecond, and reschedule itself. |
| `momentum` | 40 | how much wheel makes one line of scroll. The bar reports momentum rather than notches, and deltas arrive anywhere from single digits to the high hundreds. |

`announce` is the one duration that **is** yours, and it is yours because it is
a preference about being interrupted rather than a fact about a program.

### Environment

This program reads no environment variable itself. What it gets is the short,
explicit list the session-watcher hands every child, and two entries on it are
load-bearing here:

- **`USER`** (and `LOGNAME`). `sketchybar` finds its per-user service by name
  and exits with `'env USER' not set! abort` without it — no per-message
  complaint, nothing marked `[!]`, and a display that only read `[!]` lines
  would paint nothing at all in complete silence for ever. That is exactly what
  happened while this was being built.
- **`TMPDIR`** and the `XDG_*` variables, because they are part of where the
  runtime directory is. A child that resolves it differently from the watcher
  looks for the socket where nobody is listening.

`HOME`, `PATH` and `AGENT_NOTIFY_ROOT` come along too. Everything else is left
behind, deliberately: an agent's environment holds API keys, and a long-lived
display would otherwise keep them in memory for days.

## What it puts on the bar

Every item is named `sketchybar.<something>`, so `sketchybar --query` will show
you any of them and a stray one is easy to spot.

| item | what it is |
| --- | --- |
| `sketchybar.<kernel>` | one counter, e.g. `sketchybar.working`. Icon is the glyph, label is the count, and the chip hangs off it as its popup. |
| `sketchybar.<kernel>.row.<n>` | one row of that chip, `rows` of them, a fixed pool drawn or not rather than items added and removed. |
| `sketchybar.<kernel>.pv.<n>` | one line of the preview, `preview.depth` of them. |
| `sketchybar.<kernel>.more` | the footer saying how many preview lines are below the window. |
| `sketchybar.away` | invisible; hears `mouse.exited.global` and closes every chip, because there is no "the pointer left the bar" event on an item. |
| `sketchybar.scroll` | invisible; holds the scroll position in its own `icon`, that being the only place one scroll event can leave something for the next. |
| `sketchybar.seen` | invisible; holds the mark a hover leaves, so an announcement can tell whether somebody came to read it. |
| `sketchybar_scrolled` | an event, not an item — the two namespaces are different — that rows and preview lines forward a wheel to. |

Every render is idempotent and complete: it names every item it owns and sets
everything about it, with no bookkeeping. The structure — the `--add`s and the
scripts that do not depend on what is happening — is sent separately and far
less often, because a preview forty lines deep behind five counters is two
hundred items and re-declaring those several times a second is a thousand
arguments per agent event. When the bar says it has lost something, the next
paint declares the whole structure again and repairs itself.

## When something happens

A bar is a thing in the corner of an eye, and a counter going from 1 to 2 is not
enough to notice. So a state that has just arrived says so:

- its **counter lights up**, in that state's own colour;
- its **chip opens by itself**, on the agent's own words;
- the **row inside it lights up** — the counter says which state changed, the
  row says which agent.

Eight seconds later all of it goes away again. `working` never announces itself
— an agent getting on with it is the state the bar is in most of the day, and a
bar lit up all day means nothing — and neither does `idle`; anything more urgent
than working does, which is core's own rule and exactly what earns a
notification elsewhere. `announce = "0s"` turns the whole thing off.

The deadline is a fact about the record — the moment the state began, plus the
window — and not the moment this display noticed. That is what stops an
unrelated agent's event, which repaints the whole bar, from extending it for as
long as anything anywhere is happening.

**A chip somebody is reading is never taken away.** Every hover leaves a mark;
if the mark says a pointer has been here since the announcement was raised, the
window expires without closing the chip. What closes it then is the pointer
leaving the bar, which is what closes every other chip.

## Hovering a row: what that agent last said

The preview sits under the rows and fills when you hover one. It scrolls — put
the pointer anywhere in the chip and turn the wheel — and the first line is the
session's name, which does not scroll, because the preview is below every row
and whose words these are would otherwise be a guess.

Two things about how it works, both load-bearing:

- **Every line is written to the bar and only the first screenful is drawn.**
  Scrolling moves nothing but `drawing`, over slots that already say the right
  thing, so a wheel never touches the agent's words and nothing about it has to
  be quoted. Writing only the visible lines is what makes a scroll reveal
  blanks.
- **A hover carries its own answer.** The text is baked into the row's script by
  the paint that wrote the row, so a hover is one `sh` and one client — no store
  read, and nothing that can be stale.

That second one is the only place in this system where text an agent wrote is
put inside a shell command, and it is quoted for it: within single quotes `sh`
expands nothing at all, so the one character that matters is the apostrophe,
which becomes a typographic one. There is a test that builds the script from a
hostile message, runs it with a real `/bin/sh`, and checks that nothing
happened.

## Why it repaints on a timer

The chips say how long a session has been in the state it is in, and nothing
will ever tell this display that four minutes have become five — because nothing
changed. Elapsed time is derivable by every subscriber from the record it
already holds, so it is rendering and never state: storing it would mean a write
per second per session, and every one of those writes would wake every display
to say nothing at all. So the age is computed here, and here is where the clock
lives.

## Five things about sketchybar worth knowing

All measured on v2.24.0, all load-bearing:

- **Everything is forgiving, which is what makes a render idempotent.** `--add`
  on an item that exists is informational (`[?]`); `--set` on one that does not
  is a complaint (`[!]`) and the rest of the batch still lands. So this names
  every item it owns and sets everything about it, every time, with no
  bookkeeping — and when the bar says it has lost something, the next paint
  declares the whole structure again and repairs itself.
- **A backslash in any property makes that item's `--query` answer invalid
  JSON.** sketchybar does not escape it, so anything that reads the item back
  fails to parse. It cost a test that could not read an item it had just
  written, and it is why the scripts here name their items one at a time rather
  than with the regex sketchybar would accept in place of a name.
- **The exit code reports only the LAST message in the batch.** `--set missing
  --set real` exits 0; `--set real --set missing` exits 1. It says nothing about
  the hundred messages in between, so it cannot be a verdict on a render. The
  complaints are the truth, and their severity is sketchybar's own.
- **A non-zero exit with nothing marked `[!]` is a wholesale refusal**, and that
  distinction is not pedantry — it is the `USER` failure above, which a display
  that only read `[!]` lines would suffer in complete silence for ever.
- **A query issued immediately after a large batch comes back empty**, exit 0,
  and is fine again 200ms later. This queries once a paint and only while an
  announcement is live, so it costs nothing in normal running; it is mostly a
  trap for a test that looks at its own work too quickly.
- **An item's script also runs on a forced `--update`**, with `$SENDER=forced`.
  Every script here guards on `$SENDER`, because without it a bar reload opens a
  chip nobody hovered. A script gets `$SENDER`, `$NAME`, `$BUTTON` and
  `$SCROLL_DELTA` and nothing else — which is why the scroll keeps its position
  in an item's icon, that being the only place one scroll can leave something
  for the next.

## Testing

```sh
env -u GOROOT go test ./...
```

What the bar should say is a table of sessions in and a command line out, so
most of it needs no sketchybar at all. The rest runs against the real one under
its own item prefix and cleans up afterwards, skipping itself where sketchybar
is not installed or not running.

## What is unfinished

- **Nothing is published.** `go.mod` has a `replace` pointing at core's
  directory in this monorepo, so this module builds beside core and a copy of
  this directory on its own does not compile. The line comes out when core is
  published.
- **The preview has no off switch.** The code paths for a bar without one all
  exist and are exercised, but every value that would select it is refused by
  the sanity checks, so the setting is unreachable.
- **The preview answers to the pointer and nothing else.** There is no keyboard
  path to a chip, a row or a scroll, and sketchybar offers none.
- **The `before`/`after` placement is best-effort.** A reference item that does
  not exist yet — your `sketchybarrc` reloading underneath this display, say —
  is one complaint in the log and items at the end of the side.
