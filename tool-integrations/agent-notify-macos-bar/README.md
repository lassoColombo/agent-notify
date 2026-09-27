# agent-notify-macos-bar

The **display** integration that puts [agent-notify](../../agent-notify)'s
semaphore in the real macOS menu bar: one mark per state your agents are in,
the ones waiting on you flickering, and a menu behind it listing every session
with the one you choose brought to the front.

It needs nothing installed first. The [sketchybar
display](../agent-notify-sketchybar) needs sketchybar and the [zellij
display](../agent-notify-zellij-display) needs zellij; this one needs the menu
bar every Mac already has, which is the reason it exists.

```
  👾 👾 👾                   ← one alien per live state; the ones waiting on you flicker

  ┌────────────────────────────────────┐
  │  blocked on you                    │  ← a real section header
  │  👾 ask-me-anything          1m    │
  │  ────────────────────────────────  │
  │  finished a turn                   │
  │  👾 lenny                    2m    │
  │  ────────────────────────────────  │
  │  working                           │
  │  👾 agent-notify             6m    │
  │  👾 dmilog3-dashboard       12m    │
  └────────────────────────────────────┘
```

---

- [What is on the bar](#what-is-on-the-bar)
  - [The item](#the-item)
  - [The menu](#the-menu)
  - [What a row reads, and what it does not](#what-a-row-reads-and-what-it-does-not)
  - [The other displays it is not](#the-other-displays-it-is-not)
- [Installation](#installation)
  - [Prerequisites](#prerequisites)
  - [Build it out of the monorepo](#build-it-out-of-the-monorepo)
  - [`install`: the bundle, and the table it prints](#install-the-bundle-and-the-table-it-prints)
  - [Turning it on](#turning-it-on)
  - [Checking it afterwards](#checking-it-afterwards)
  - [Put the item where you want it](#put-the-item-where-you-want-it)
  - [Signing, and what it buys](#signing-and-what-it-buys)
  - [Upgrading and uninstalling](#upgrading-and-uninstalling)
- [Configuration](#configuration)
  - [Where the file is](#where-the-file-is)
  - [`[integration.macos-bar]` — core's half](#integrationmacos-bar--cores-half)
  - [`[integration.macos-bar.settings]` — this display's half](#integrationmacos-barsettings--this-displays-half)
  - [The three palettes](#the-three-palettes)
  - [How a colour may be spelled](#how-a-colour-may-be-spelled)
  - [Everything is checked at startup](#everything-is-checked-at-startup)
  - [Environment](#environment)
  - [The timings you cannot configure](#the-timings-you-cannot-configure)
- [Commands](#commands)
- [What it asks to be woken for](#what-it-asks-to-be-woken-for)
- [How it is built](#how-it-is-built)
- [What is unfinished](#what-is-unfinished)

---

## What is on the bar

### The item

**One alien per state that has anybody in it**, most urgent first, each in that
state's own colour, and the ones waiting on a person flicker while the ones
getting on with it sit perfectly still. Waiting on a person is core's own
rule — anything ranked above `working` — which is also exactly what earns a
notification, so the bar and the banner agree by construction rather than by
two lists agreeing with each other.

**The mark never changes shape.** That is the property the designs before this
one could not keep: a count per state answered "what is happening" and never
"do I need to do something", and one count for the most urgent state was worse,
because the symbol changed identity whenever the agents did and a glance had to
be decoded before it could be read. This is the marks without the numbers. How
many are in each state lives in the menu, and in the **tooltip** — which is also
what the item is called to VoiceOver and to the accessibility API, since a
picture has no name of its own:

```
agent-notify — 2 finished a turn, 1 working
```

With nothing running at all the item is still a mark, in `resting`, the one hue
no state wears. A status item with nothing in it is a few points of blank menu
bar that nobody can see or click, which reads as the display having died.

### The menu

The states are sections, the sessions are rows under them, in the same order as
the marks, with a separator between one state and the next. A session's row
carries its name, the mark in its state's colour, and how long it has been in
that state, right-aligned on its own tab stop. **Choosing a row runs
`agent-notify focus-session`** — the same thing every other display runs, because
where a session lives is the containers' business and a display that went and
looked would be a second implementation of it, wrong in its own way.

Each state lists at most [`rows`](#integrationmacos-barsettings--this-displays-half)
sessions and then says `and 3 more`. With nothing running the menu says
`no agents`, because an empty menu looks like a display that has broken rather
than like a quiet machine.

### What a row reads, and what it does not

`dump` prints the document the display would apply, straight from the store and
with no menu bar involved — and it is the honest answer to "what does it think
is going on", because every other display in this system can simply be looked
at while a menu exists only for the second somebody is holding it open:

```
$ agent-notify-macos-bar dump
{
  "title": [
    { "text": "", "symbol": "invader", "colour": "0xff9ccfd8", "flicker": true },
    { "text": "  ", "symbol": "invader", "colour": "0xffc4a7e7" }
  ],
  "menu": [
    { "kind": "section", "text": "finished a turn" },
    {
      "kind": "row",
      "text": "lenny-load",
      "symbol": "invader",
      "glyph": "●",
      "colour": "0xff9ccfd8",
      "age": "2h",
      "key": "COLOMBOSRNBX~claude~099ccac2-c26e-4064-8bbc-c09019ef28e4"
    },
    { "kind": "separator" },
    { "kind": "section", "text": "working" },
    {
      "kind": "row",
      "text": "agent-notify-95",
      "symbol": "invader",
      "glyph": "◌",
      "colour": "0xffc4a7e7",
      "age": "1m",
      "key": "COLOMBOSRNBX~claude~956a9fe5-0a41-4023-8df8-253936353273"
    }
  ],
  "tooltip": "agent-notify — 2 finished a turn, 1 working"
}
```

That is the whole vocabulary: a row is a name, a state, an age and the key
`focus-session` takes. **It does not carry what the agent said.** That was tried
twice — as a truncated second line and as a hover tooltip — and both were bad in
the same way: a fragment of a sentence, in a place nobody chose to look. What an
agent says belongs in the notification, which arrives when it is said and is a
proper piece of user interface. The menu is for finding a session and going to
it.

### The other displays it is not

Several displays at once is the ordinary case here, not a clever one. They never
talk to each other: each connects to the session-watcher on its own, is handed
the same state, and renders it wherever it renders.

| | what it draws | what it needs |
| --- | --- | --- |
| **agent-notify-macos-bar** | one item, one mark per live state, a menu of sessions behind it | nothing — the system menu bar |
| [agent-notify-sketchybar](../agent-notify-sketchybar) | a counter per state on sketchybar, a chip per counter, the agent's last message under a hovered row | sketchybar, running and configured |
| [agent-notify-macos-notifications](../agent-notify-macos-notifications) | a notification when an agent wants you | nothing, but a properly signed bundle |

**This and sketchybar are alternatives, not layers.** They draw the same
semaphore on the same strip of screen, so running both means two of everything;
pick the one whose bar you already live with. They differ in more than their
host: sketchybar's chip opens by itself when something happens and shows what
the agent last said, where this one leaves the message to the notifier and never
opens its menu — an `NSMenu` opening starts a modal tracking loop that takes the
keyboard for as long as it is up, so a display that opened one by itself would
eat what you were typing several times an hour. What a paint costs is the reason
they even subscribe differently: there, a paint is a process, and here it is a
JSON encode and a dispatch onto a queue.

The notifications program is a genuinely different surface and is installed
separately or not at all — its own binary, its own bundle, its own
`[integration.macos-notifications]` table. Install either, both, or neither.

---

## Installation

### Prerequisites

- **macOS**, and nothing older than what your Go toolchain builds for. Every
  measurement quoted in this repository was taken on macOS 26.6.2, arm64.
- **Go 1.26 or newer**, because this is a cgo binary and **Go 1.21 builds one
  whose ad-hoc signature is invalid**: macOS 26 kills it on launch, before
  `main`, with no message anywhere. If this binary ever dies instantly and
  silently, check `codesign -v` before anything else. If your shell exported
  `GOROOT` from an outer context it overrides the toolchain pinned in
  `.tool-versions`; `env -u GOROOT go build ./...` is the fix.
- **The Xcode command line tools**, for the Cocoa and ApplicationServices
  headers this links against: `xcode-select --install`.
- **`codesign`, `security` and `iconutil` on `PATH`** — `install` shells out to
  all three, to sign the bundle, to list the identities your keychain holds, and
  to assemble the icon.
- **A logged-in graphical session.** There is no menu bar in a system context,
  and AppKit's answer to being started where there is no window server is to
  abort the process; this asks first and says so instead. The session-watcher is
  perfectly capable of starting a display somewhere there is no screen, which is
  why the question is asked at all.
- **Nothing else.** No accessibility permission is needed to draw — only `check`
  wants it, to ask the accessibility API what is on the bar, and it says so when
  it has not been granted.

### Build it out of the monorepo

This is one module inside the `agent-notify` monorepo, and core is not published
yet: `go.mod` carries a `replace` pointing at `../../agent-notify`. So there is
**one clone**, and you build this module from its own directory.

```sh
git clone git@github.com:lassoColombo/agent-notify.git ~/projects/agent-notify
cd ~/projects/agent-notify

# core first — the CLI everything else talks to
go -C agent-notify build -o /opt/homebrew/bin/agent-notify ./cmd/agent-notify

# then this display
go -C tool-integrations/agent-notify-macos-bar build -o /opt/homebrew/bin/agent-notify-macos-bar .

agent-notify-macos-bar --help    # it built
```

Anywhere on `PATH` will do instead of `/opt/homebrew/bin` — `~/.local/bin`, or
whatever `go env GOBIN` says. Being on `PATH` matters for exactly one thing:
`agent-notify install macos-bar` finds this program by name. The config file
names the binary by absolute path afterwards, and the session-watcher runs that
path and not your `PATH`.

### `install`: the bundle, and the table it prints

```sh
agent-notify install macos-bar              # the normal spelling: core execs this program's install
agent-notify-macos-bar install              # the same thing, directly
agent-notify-macos-bar install --app DIR    # put the .app somewhere other than ~/Applications
agent-notify-macos-bar install --sign NAME  # sign the bundle with a named identity
```

`agent-notify install <integration>` is a dispatcher and nothing else: core
`exec`s `agent-notify-macos-bar install` and every option after the name belongs
to this program. Core does not know what a bundle is and must not.

Two things happen, and they are deliberately unlike each other:

**A `.app` bundle is written**, to `~/Applications/agent-notify-macos-bar.app`.
That is this program's own artifact — macOS will not remember where you put a
menu bar item without one and there is no other way for it to exist — so
building it is mechanical and `install` does it, idempotently, over whatever was
there before. It holds a signed **copy** of the binary, an `Info.plist` with
`LSUIElement` so the display never appears in the Dock or the app switcher, and
an icon drawn at install time.

**The config table is printed, not written.** Everything in agent-notify's
config file is yours to write, and the test for whether something belongs there
is whether only you can know the answer. Whether this display should be running
is exactly that — and since `enabled` defaults to true, the table being there is
what turns it on. So `install` puts the table on **stdout** and everything else
on stderr, and you decide:

```
$ agent-notify install macos-bar
[integration.macos-bar]
binary = "/Users/you/Applications/agent-notify-macos-bar.app/Contents/MacOS/agent-notify-macos-bar"

The bundle is at /Users/you/Applications/agent-notify-macos-bar.app. Nothing else was written:
put the table above in /Users/you/.config/agent-notify/config.toml when you want this running, since the
table being there is what turns it on.

The bundle holds a COPY of the binary, so run this again after rebuilding.

Banners are a second display, agent-notify-macos-notifications, with a table
of its own — install it too if you want to be interrupted as well as informed.
```

The split of streams is the point: somebody who has already decided can redirect
the table into the file and nothing else will land inside it.

```sh
agent-notify install macos-bar >> ~/.config/agent-notify/config.toml
```

`--print` is not a flag here; printing is what `install` does.

### Turning it on

```sh
agent-notify install macos-bar >> ~/.config/agent-notify/config.toml
agent-notify watcher reload     # re-read the config without restarting
```

The session-watcher reads the config at startup and on `reload` (which is a
`SIGHUP`), and a reload stops and restarts every integration — deliberately, so
that a display you have just rebuilt is actually the one now running. It starts
this program, supervises it, restarts it if it dies, and gives up after
`integration-tries` consecutive failures (five by default); `agent-notify
watcher reload` clears that and retries. You never start this binary by hand, it
is not a login item, and it needs nothing in any other program's configuration.

It puts its item on the menu bar when it connects, and — almost — takes it off
when it stops. `removeStatusItem:` is deliberately not called on shutdown,
because it erases the item's saved position; the item goes when the process
does.

### Checking it afterwards

`agent-notify doctor` is the one that answers "is it running and connected":

```
$ agent-notify doctor
agent-notify 0.0.0-dev   darwin/arm64

paths
  root        (default — AGENT_NOTIFY_ROOT is not set)
  state       /Users/you/Library/Application Support/agent-notify
  runtime     /var/folders/hg/…/T/agent-notify
  config      /Users/you/.config/agent-notify/config.toml
  log         /var/folders/hg/…/T/agent-notify/agent-notify.log

directories  ok    present, mode 700
sockets      ok    longest path 82 of 103 bytes — …/agent-notify/session-changes.sock
config       ok    2 agent(s), 6 integration(s), keep-ended-sessions 168h0m0s
store        ok    3 live session(s), 74 ended and still resumable, 0 forgotten this run
liveness     ok    boot 027BA0B8-… — 3 running, 0 gone but not yet ended, 0 cannot tell
watcher      ok    pid 86777, version 0.0.0-dev, since 2026-09-22T22:15:40Z
                  the socket answers: 77 session(s) in its snapshot
integrations ok    reported by pid 86777 at 2026-09-23T20:38:39Z
             macos-bar              connected, pid 86843 [display]
             macos-notifications    connected, pid 86844 [display]
             zellij-display         connected, pid 86847 [display]
```

`macos-bar … connected` is the line that matters. A configuration error is
reported here too, by name, because this display refuses a setting it cannot act
on at startup rather than drawing something wrong.

`check` is this program's own, and answers the questions `doctor` cannot — the
ones that are invisible when they are wrong and invisible when they are right:

```
$ agent-notify-macos-bar check
binary           /Users/you/Applications/agent-notify-macos-bar.app/Contents/MacOS/agent-notify-macos-bar
bundle           io.github.lassocolombo.agent-notify-bar
menu bar         available
focus-session    /opt/homebrew/bin/agent-notify
on the bar       👾

Being ON the bar is not the same as being DRAWN on it. A full menu bar
allocates the leftmost items positions and never renders them, and the
accessibility API cannot tell the difference — so the last check is you,
looking at the screen.
```

Run as the bare binary it says so, and points you at the copy inside the bundle.
`on the bar` asks the accessibility API — the only thing that can see a status
item, since `NSStatusItem` windows are hosted out of process and never appear in
`CGWindowListCopyWindowInfo` — and `cannot be asked` means accessibility has not
been granted to whatever started this process, which is not the same as the item
being absent.

And `dump` says what it would draw, without a menu bar. Those three answer
different questions: `doctor` whether it is connected, `check` whether it can
draw, `dump` what it would draw.

### Put the item where you want it

**Once, after installing, cmd-drag the item to where you want it.** It stays
there, and that is the entire reason the bundle exists. A bundle-less process
has no preferences domain that survives it — measured: a value written into the
`agent-notify-macos-bar` domain read back fine and had vanished by the next
launch — so `NSStatusItem.autosaveName` has nowhere to file the position and the
item goes back to the **leftmost** slot on every restart.

On a full menu bar the leftmost slot is not drawn at all. Not a metaphor: on a
1512-point notched MacBook, twenty-one status items ran contiguously from x=520
to x=1506, and only those from x≈912 rightwards were on the screen. The other
ten — Teams, OneDrive, Bitwarden, WireGuard and friends — were allocated
positions and never rendered. The display worked perfectly and could not be
seen.

**There is a counter-measurement, and it is why this machine's config points at
the bare binary** [measured 2026-09-21]: a bundled build put its `NSStatusItem`
up, reported it visible at level 25 with a real frame, and the window server
never drew it — bare binaries appear to be adopted into the menu bar where
bundles signed by something this machine does not trust are not. If the bundled
one does not appear for you, point `binary` at the bare path instead and accept
the cost `install` warns about:

```toml
[integration.macos-bar]
binary = "/opt/homebrew/bin/agent-notify-macos-bar"
```

Either way, the last check is looking at the screen.

### Signing, and what it buys

`install` signs the bundle ad-hoc unless you name an identity, and ad-hoc is
enough for everything **this** display does. The identity matters for the
notifications program next door, whose measurements are quoted in `bundle.go`
because they cost an afternoon: an ad-hoc signature means
`UNUserNotificationCenter` refuses with "Notifications are not allowed for this
application", no prompt is ever shown, the status goes to `denied` without
anybody being asked, and the app never appears in System Settings — so there is
nowhere to go and turn it on. A self-signed certificate in the login keychain,
**untrusted**, is enough; macOS wants an identity, not a trusted one and not
Apple's. A self-signed identity is also stable across rebuilds where an ad-hoc
one is not.

```sh
agent-notify-macos-bar install --sign "agent-notify self-signed"
```

The identity is checked against `security find-identity -p codesigning` and the
install fails, listing what your keychain does have, if the name is not one of
them. When `--sign` is not given, `install` reads
[`sign`](#integrationmacos-barsettings--this-displays-half) back out of the
config — because install is re-run after every rebuild and having to remember a
flag each time is how a bundle quietly goes back to ad-hoc. With `--sign` given,
the table it prints includes the setting so the next run remembers.

One warning that cannot be undone: **a decision macOS has made about a bundle
identifier cannot be unmade.** An identifier that was ever used with an ad-hoc
signature keeps a `denied` that survives being signed correctly afterwards, does
not appear in System Settings, and has no reset. `io.github.lassocolombo.agent-notify-bar`
is already the second identifier this display has had, for exactly that reason.

### Upgrading and uninstalling

The bundle holds a **copy** of the binary, not a symlink, because `codesign`
refuses a symlinked executable outright — "the main executable or Info.plist
must be a regular file". So a rebuild does not reach the bundle on its own:

```sh
go -C tool-integrations/agent-notify-macos-bar build -o /opt/homebrew/bin/agent-notify-macos-bar .
agent-notify install macos-bar > /dev/null     # refresh the bundle; it is idempotent
agent-notify watcher reload
```

Copying into the bundle goes through a temporary file and a rename, because the
file being replaced is usually the one being executed — writing over a running
binary is `ETXTBSY` — and a rename leaves the running process on the inode it
already has, so there is never a moment when the bundle holds half a binary.

To uninstall, delete the `[integration.macos-bar]` table, `agent-notify watcher
reload`, and remove `~/Applications/agent-notify-macos-bar.app`. Nothing else in
the config file changes and no other integration notices.

---

## Configuration

### Where the file is

`~/.config/agent-notify/config.toml`, or `$XDG_CONFIG_HOME/agent-notify/config.toml`.
There is no file by default and running without one is the supported case: this
display's defaults are complete. `agent-notify doctor` prints the exact path it
resolved, which is the one to trust.

Everything this display reads is in its own two tables. A key nobody declared is
refused **by name**, including the other macOS display's keys — because a
misspelled setting that changes nothing and says nothing is the config bug
people give up on.

### `[integration.macos-bar]` — core's half

This table is core's schema, the same for every integration.

| Key | Type | Default | What it does |
| --- | --- | --- | --- |
| `binary` | string | — | The program the session-watcher supervises. Absolute, or looked up on `PATH`. `install` prints the path inside the bundle. |
| `enabled` | bool | `true` | Absence means enabled: writing the table is how you ask for the display. `false` keeps the table and stops it being started. |
| `capture-environment` | bool | `false` | Not used here, and deliberately: a menu bar does not care where a session lives, and getting you there is `focus-session`'s job. |
| `settings` | table | — | Handed over verbatim and never read by core. See below. |

```toml
[integration.macos-bar]
binary = "/Users/you/Applications/agent-notify-macos-bar.app/Contents/MacOS/agent-notify-macos-bar"
```

One key elsewhere in the file matters to this display: top-level
`agent-notify-binary`, which is how it finds the `agent-notify` to run when you
choose a row. A supervised child's `PATH` is not your shell's `PATH`, so naming
it explicitly is worth doing.

### `[integration.macos-bar.settings]` — this display's half

All of it optional.

```toml
[integration.macos-bar.settings]
rows     = 8                          # sessions per state before "and n more"
announce = "0s"                       # "8s" spells the name out for eight seconds
resting  = "0xff8c88a6"               # the mark's colour when nothing is running
font     = ""                         # the menu bar's own
sign     = "agent-notify self-signed" # remembered for the next `install`
```

| Key | Type | Default | What it changes |
| --- | --- | --- | --- |
| `rows` | int | `8` | How many sessions a state lists before it says `and n more`. A menu scrolls when it has to, so this is about a menu being readable rather than one fitting. Any value `> 0` is taken; `0` and below leave the default. |
| `announce` | duration | `"0s"` (off) | How long a state change adds the name of the session it happened to, after the marks, in that state's colour. `"8s"` is eight seconds. Negative, and anything that is not a duration, is refused at startup. |
| `resting` | colour | `0xff8c88a6` (Subtle) | The mark's colour when nothing is running at all. The one hue no state wears, so an empty machine cannot be mistaken for an agent doing something. |
| `font` | string | `""` | The family the **item** is drawn in. Empty is the menu bar's own font, which is right for the shapes that ship. Name one if you want Nerd Font glyphs. Refused at startup if this machine does not have it. |
| `sign` | string | `""` (ad-hoc) | The code signing identity `install` gives the bundle, remembered so a re-install after a rebuild does not need the flag again. **Nothing at runtime reads it.** |

`announce` is written the way a person writes a duration — the same spelling as
every other duration in this system, because this display imports core's type
rather than growing one of its own (D-78). A bare number is refused, since
`announce = 8` would otherwise be eight nanoseconds. It is off by default, and that is a retraction: the item taking itself over
to spell out a session's name was this display's own idea of a notification and
it was a bad one — invisible unless you are looking at the menu bar, and it
changes the item's width on a bar where width is the scarcest thing there is.
macOS has a notification system that does all of it better. Turn it on and:

```
  👾 👾     →     👾 👾  agent-notify     →     👾 👾
```

The deadline belongs to the **record** — `state_since` plus the window — not to
the display, so every repaint computes the same answer and no repaint can extend
it. Open the menu while an announcement is up and the row it is about is lit. It
never opens the menu by itself, for the keyboard reason above.

### The three palettes

Three sub-tables, all keyed the same way: by the **state**, which is either a
kernel (`working`) or a kernel and its detail (`blocked-on-you/permission-prompt`),
and resolution is most specific first — the pair, then the kernel, then the
nearest known rank, which is what lets a display built today paint a state core
gains tomorrow as "something roughly this important" rather than as nothing.

```toml
[integration.macos-bar.settings.symbols]
blocked-on-you = "exclamationmark.triangle.fill"   # an SF Symbol name
working        = "arrow.triangle.2.circlepath"

[integration.macos-bar.settings.glyphs]
blocked-on-you                     = "▲"   # the fallback when the symbol is unknown
"blocked-on-you/permission-prompt" = "▲"

[integration.macos-bar.settings.colors]
blocked-on-you  = "0xffeb6f92"   # 0xAARRGGBB, the spelling every other display takes
finished-a-turn = "labelColor"   # or an AppKit name, which adapts to light mode
```

**`symbols`** is the mark drawn in each menu row's image well, where AppKit puts
an icon and where every other menu on the system puts one. Every state ships the
same one: `invader`, this program's own sprite, which is not an SF Symbol and
never will be — it is eleven cells by nine, drawn by hand — and is spelled like
one so it can go anywhere a symbol name goes. Five different SF Symbols read as
five unrelated things stacked up; the rows do not need their icons to name their
states, because the section headers do that and the colours do it again. Put SF
Symbols back one state at a time if you prefer them. A symbol this macOS has
never heard of draws **nothing at all** rather than falling back, so the names
are checked when the config is read.

| State | Default symbol | Default glyph | Default colour |
| --- | --- | --- | --- |
| `blocked-on-you` | `invader` | `?` | `0xffeb6f92` — Love |
| `broke` | `invader` | `!` | `0xfff6c177` — Gold |
| `finished-a-turn` | `invader` | `●` | `0xff9ccfd8` — Foam |
| `working` | `invader` | `◌` | `0xffc4a7e7` — Iris |
| `idle` | `invader` | `○` | `0xcce0def4` — the text colour at 80% |

**`glyphs`** is the text mark drawn ahead of a row's name when the symbol is not
available. They are geometric shapes from the Unicode block every Mac font has,
and **not** the Nerd Font private-use codepoints the other displays use: the menu
bar is drawn in the system font, which has no idea what `` is and shows a box.
That is the one thing about a native bar that is less free than sketchybar's,
and `font` is the way out of it. The shapes differ from each other as shapes, not
only as colours, so the marks still say something with the colour taken away.

**`colors`** is [Rosé Pine](https://rosepinetheme.com), and **every state has its
own hue** — which is a reversal of the rule that used to be here. Colour only
where it means something was right for an item showing one mark and wrong for one
showing several, because then identical sprites sit side by side and colour is
the only thing telling them apart. What is rationed now is **movement**: only the
states waiting on a person flicker, and a hue that never moves is quiet in a way
a hue on its own is not. The hues are chosen against each other rather than one
at a time, because what has to be distinguishable is anything in the same
movement class — the three that flicker are Love, Gold and Foam, the two that sit
still are Iris and dimmed text. `working` is Iris and not Pine because `#31748f`
is built for an opaque background, and brightened it lands a shade away from the
Foam beside it; two blues on a translucent bar are one blue.

`idle` is the text colour dimmed rather than the palette's Subtle or Muted tones.
Those are built for an opaque `#191724`, and a menu bar is a translucent grey
lighter than that, so on it they come out darker than the system's own secondary
label — which was already too dark to read.

The cost of a fixed palette is **light mode**: these are values for a dark menu
bar, and somebody who switches appearance wants `labelColor` and friends, which
the same table takes by name.

### How a colour may be spelled

Either `0xAARRGGBB`, the spelling every other display in this system takes, or
the name of one of AppKit's own, which adapts when the appearance changes:

`systemRed`, `systemOrange`, `systemYellow`, `systemGreen`, `systemMint`,
`systemTeal`, `systemCyan`, `systemBlue`, `systemIndigo`, `systemPurple`,
`systemPink`, `systemBrown`, `systemGray`, `labelColor`, `secondaryLabelColor`,
`tertiaryLabelColor`, `quaternaryLabelColor`, `controlAccentColor`.

### Everything is checked at startup

Colours, symbol names and the font are validated when the config is read, while
there is still somebody to tell — and the display refuses to start rather than
drawing something wrong:

```
[integration.macos-bar.settings.colors] working = "0xgg" is neither 0xAARRGGBB nor a colour
AppKit knows (systemRed, systemOrange, labelColor, secondaryLabelColor, …)
```

A colour AppKit cannot draw is not an error at paint time — it is a state quietly
drawn in the ordinary menu colour, which looks exactly like a state nobody
bothered to configure. Same for a symbol, which draws nothing at all, and a font,
which is silently replaced by the menu bar's own. The refusal is reported by the
session-watcher and shows up in `agent-notify doctor` and in the log.

One more startup error: **the display will not start if it cannot find
`agent-notify`**, because a menu whose rows cannot be acted on is a list, not a
menu. That is what `agent-notify-binary` in the config file is for.

### Environment

This program reads **no environment variable of its own**. The only one in play
is core's:

| Variable | Effect |
| --- | --- |
| `AGENT_NOTIFY_ROOT` | Moves the state directory, the runtime directory and the config file beneath one directory — the one step that makes an isolated instance possible. Unset, the platform defaults apply: `~/Library/Application Support/agent-notify`, a runtime directory under `$TMPDIR`, and `~/.config/agent-notify/config.toml`. |

It is read through the subscriber SDK, so this display is pointed at a recording
the same way anything else is — `agent-notify record` and `agent-notify replay`
set it for you.

### The timings you cannot configure

None of these is a setting, and each is deliberate.

| What | How long | Why it is fixed |
| --- | --- | --- |
| Repaint with nothing new to say | 30s | The menu says how long a session has been in its state, and nothing will ever tell this display that four minutes have become five, because nothing changed. Elapsed time is rendering and never state — storing it would mean a write per second per session, each waking every display to say nothing — so the clock lives here. How often is this display's business, not a person's. |
| The paint that takes an announcement down | at its deadline + 100ms | The repaint clock shortens its own wait to the announcement's deadline, so the paint that ends one lands when it ends rather than up to a refresh later. The 100ms is so that the paint does not race the deadline, find the announcement still up by a microsecond, and reschedule itself for ever. |
| Flicker | 0.55s, between full opacity and 0.3 | It is a timer on the AppKit side rather than a repaint, which keeps the animation off the wire: a display that flickered by repainting would send two documents a second for as long as somebody left an agent waiting. The title is redrawn at a lower opacity rather than the button being faded, because `alphaValue` on a status item's button does nothing at all — the window is hosted by another process, and what crosses is drawn content. |
| One `focus-session` | 30s | Not a performance bound: it is there so the goroutine cannot be lost. Core walks the container order giving each step five seconds of its own, so a focus through a window manager and a multiplexer can legitimately take three times that, and a shorter bound would kill focuses that were about to succeed. |
| `check`'s wait for the item | 1.5s | Long enough for AppKit to have actually put the item up before the accessibility API is asked about it. |

---

## Commands

```
agent-notify-macos-bar            stay connected and paint
agent-notify-macos-bar check      say what it can and cannot do on this machine
agent-notify-macos-bar dump       print what it would put on the bar, and stop
agent-notify-macos-bar install    build the bundle and print the config table
agent-notify-macos-bar --help     this list
```

| Command | Flags | What it does |
| --- | --- | --- |
| *(none)* | — | Connects to the session-watcher and paints until it is stopped. This is what the watcher runs; you do not. Exits 1 if there is no window server. |
| `check` | — | Binary, bundle identifier, menu bar, the `agent-notify` it would run, and what the accessibility API says is on the bar. Exits 1 with no window server. |
| `dump` | — | The document it would apply, as indented JSON, read straight from the store with no menu bar involved. |
| `install` | `--app DIR`, `--sign NAME` | Writes the bundle, prints the table on stdout and the explanation on stderr. Idempotent. |

---

## What it asks to be woken for

A display declares which record fields are worth waking it for, so nothing is
woken for a change it does not care about. This one asks for:

```go
[]string{"kernel", "detail", "rank", "name", "cwd", "state_since"}
```

and that list is not maintained by hand: there is a test that moves one field at
a time, renders twice, and fails when the menu draws differently for something
not named here. A list kept by hand drifts away from the render the first time
somebody adds a line to a row.

**`message` is not on it**, and it used to be. This menu has never drawn what an
agent said — it shows the state, the name and how long — so asking to be told
when the message changed bought a repaint every time somebody typed, and nothing
to show for it. Ended sessions are not asked for either: an ended session is not
displayed, and there is nothing to count.

---

## How it is built

Everything that decides what should be on the screen is in Go and produces one
`WhatTheBarShows` — a title in coloured pieces, a flat list of menu rows, and a
tooltip — which crosses into Objective-C as **one JSON string**. `menubar.m`
applies it and makes no decisions at all, which is why every question this
display answers can be tested on a machine with no screen. A tree of items with
a tree of colours under it, mapped field by field through cgo, is exactly the
kind of boundary that turns into a second program; `NSJSONSerialization` is in
the system and costs nothing at the rate a menu bar changes.

The menu is **built when somebody opens it**, from the last paint, rather than
when a paint arrives. A paint happens whenever any agent anywhere does anything;
a menu is open for a second or two a day — and rebuilding one underneath a person
holding it open is the other thing this avoids.

The render function is handed the **current state**, never a transition, so
there is nothing to remember and nothing to get out of step with after a dropped
message, a restart or a closed laptop. `main` locks the OS thread in its `init`,
because AppKit will only be driven from the thread the process started on and
`NSApplication` checks.

Three things about the macOS menu bar that cost time to find, all measured on
macOS 26.6.2, arm64, 2026-09-18:

- **A status item does not need an app bundle to exist, and does need one to be
  usable.** A plain Go binary can own one; it just cannot remember where the
  person put it.
- **`NSStatusItem` windows are hosted out of process** — window number
  `4294967296` — so they never appear in `CGWindowListCopyWindowInfo`. What can
  see one is the accessibility API, through `AXExtrasMenuBar` on the owning pid,
  which is what this module's tests use as their oracle. Its limit: AX reports a
  position and a size for an item the menu bar has decided not to draw, so those
  tests pass while nothing is on the screen.
- **Go 1.21 builds a cgo binary whose ad-hoc signature is invalid**, and macOS 26
  kills it on launch, before `main`, with no message anywhere.

---

## What is unfinished

- **Nothing here is published.** This module lives in the `agent-notify`
  monorepo and its `go.mod` `replace`s core with `../../agent-notify`, so a clone
  of anything less than the whole repository does not compile. That line comes
  out when core is released.
- **Whether the bundle or the bare binary is the one that gets drawn is not
  settled**, and the two measurements above disagree. Try the bundle first; if
  the item never appears, point `binary` at the bare path and lose the remembered
  position.
- **Light mode is not handled.** The default palette is values for a dark menu
  bar. Switching appearance means writing AppKit colour names into
  `[…settings.colors]` yourself; nothing adapts on its own.
- **`check` cannot tell you the thing you most want to know.** It can say the
  item is on the bar; it cannot say the bar is drawing it, because the
  accessibility API cannot tell the difference either. The last check is you,
  looking at the screen.
- **The version handshake does not negotiate.** Every integration announces the
  core version it was built against, and a mismatch is refused permanently rather
  than retried — everything here is built and released together, so a mismatch is
  a half-finished deployment and not a protocol to arbitrate. Rebuild both.
