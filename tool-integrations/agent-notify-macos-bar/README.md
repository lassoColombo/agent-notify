# agent-notify-macos-bar

macOS's own **display** integration for [agent-notify](../../agent-notify), and
one of two: this draws the semaphore in the real menu bar, with no bar of
anybody else's to install first, and
[agent-notify-macos-notifications](../agent-notify-macos-notifications)
interrupts you when an agent wants you. Two displays, two programs, two config
tables, each reading only its own. Install either, or both, or neither.
One status item saying which states your agents are in and which of them want
you, and a menu behind it listing the sessions, most urgent first, with the one
you choose brought to the front.

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

## Two displays, installed separately

`agent-notify-macos-bar` and [`agent-notify-macos-notifications`](../agent-notify-macos-notifications)
are **two displays**, not two halves of one thing. Each:

- is its own repository and its own binary, in its own `.app` bundle with its
  own identifier;
- is installed by its own `install`, which adds its own table to
  `~/.config/agent-notify/config.toml` and touches nothing else —
  `[integration.macos-bar]` here, `[integration.macos-notifications]` there;
- is configured in its own `[…settings]` section, and refuses a key it did not
  declare, the other's included;
- is started, supervised and retired separately by the session-watcher, and
  neither knows the other exists.

Install either, both, or neither. To remove one, delete its table and its
bundle; the other does not notice. Both are handed the same state by the
watcher, so they cannot disagree about what is happening — only about how to
show it.

**One alien per state that has anybody in it**, most urgent first, each in that
state's own colour — and the ones waiting on a person flicker while the ones
getting on with it sit perfectly still. Waiting on a person is core's own rule,
anything more urgent than working, which is also exactly what earns a
notification: the bar and the banner agree by construction.

**The mark never changes shape**, and that is the property the designs before
this one could not keep. A count per state answered "what is happening" and
never "do I need to do something". One count for the most urgent state was
worse: the single symbol changed identity whenever the agents did, so a glance
had to be decoded before it could be read. This is the marks without the
numbers — the bar is read by colour and by movement, not by being parsed. How
many are in each state is in the menu, and in the tooltip, which is also what
the item is called to VoiceOver: a picture has no name of its own.

**A row is a session and nothing else**: its state, its name, how long. It does
not carry what the agent said. That was tried twice — as a truncated second line
and as a hover tooltip — and both were bad in the same way: a fragment of a
sentence, in a place nobody chose to look. What an agent says belongs in the
notification, which arrives when it is said and is a proper piece of user
interface. The menu is for finding a session and going to it.

**The menu's rows wear the same mark**, in their state's colour and never
moving: the flicker belongs to the bar, where it is a signal somebody might be
trying to catch out of the corner of an eye, and a menu is already being looked
at. The marks sit in each item's image well, where AppKit draws an icon and
where every other menu on the system puts one. They do not need to name their
states — the section headers do that, and the colours do it again — and five
different SF Symbols read as five unrelated things stacked up. Choosing a row
runs `agent-notify focus-session`, which is the same thing every other display
runs.

Put SF Symbols back a state at a time under `[…settings.symbols]` if you prefer
them. A symbol this macOS has never heard of draws nothing at all rather than
falling back, so the names are checked when the config is read and each state
keeps a text glyph behind its symbol. They are coloured by painting the hue over
the drawn symbol, source-atop, rather than by asking for it with a palette
configuration — because **a palette colour fills the knockout**:
`questionmark.circle.fill` is a disc with a question mark cut out of it, and
asked for in red it arrives as a plain red disc. Source-atop leaves the
destination alpha alone, so the hole stays a hole.

**The palette is [Rosé Pine](https://rosepinetheme.com), and every state has its
own hue**: Love, Gold, Foam, Iris, and the text colour at 80% for `idle`. That is a reversal of the rule that was here before — colour only where
it means something — which was right for an item showing one mark and wrong for
one showing several, because then five identical sprites sit side by side and
colour is the only thing telling them apart. **What is rationed now is
movement**: only the states waiting on a person flicker, and a hue that never
moves is quiet in a way a hue on its own is not. The hues are chosen against
each other, not one at a time — what has to be distinguishable is anything in
the same movement class, so the three that flicker are Love, Gold and Foam and
the two that sit still are Iris and dimmed text.

`idle` is the text colour dimmed rather than the palette's Subtle or Muted tones.
Those are built for an opaque `#191724`, and a menu bar is a translucent grey
lighter than that, so on it they come out darker than the system's own secondary
label — which was already too dark to read. The cost of a fixed palette is light
mode: swap the values for AppKit's names below if you switch appearance.

## Installing

```
agent-notify-macos-bar install            # writes the bundle and the config
agent-notify-macos-bar install            # builds the bundle, prints the table
agent-notify-macos-bar install --app DIR  # put the bundle somewhere else
```

Two things happen. A `.app` bundle is written to `~/Applications`, and this goes
into agent-notify's config:

```toml
[integration.macos-bar]
binary = "/Users/you/Applications/agent-notify-macos-bar.app/Contents/MacOS/agent-notify-macos-bar"
```

That is all, and there is deliberately nothing else: no bar, no config of its
own, no login item. The session-watcher starts it, it puts its item on the menu
bar when it connects, and it takes it off again when it stops — because an item
left behind by a process that is no longer running says "two agents are
working" for ever, and there is no way to tell that from a true one.

## Why there is a bundle

**So that macOS has somewhere to remember where you put the item.** A
bundle-less process has no preferences domain that survives it — measured:
a value written into the `agent-notify-macos-bar` domain read back fine and
had vanished by the next launch, and `defaults delete` then reported "Domain not
found". `NSStatusItem.autosaveName` therefore has nowhere to file anything, and
the item goes back to the **leftmost** slot of the menu bar on every restart.

On a full menu bar the leftmost slot is not drawn at all. That is not a metaphor:
on a 1512-point notched MacBook, twenty-one status items ran contiguously from
x=520 to x=1506, and only those from x≈912 rightwards were on the screen. The
other ten — Teams, OneDrive, Bitwarden, WireGuard and friends — were allocated
positions and never rendered. The display worked perfectly and could not be seen.

The bundle holds a signed **copy** of the binary, which is the one thing about it
worth knowing: a symlink would be tidier and `codesign` refuses one outright —
"the main executable or Info.plist must be a regular file" — and an unsigned
bundle is refused by anything that checks one. So rebuilding means running
`install` again. `LSUIElement` keeps it out of the Dock and the app switcher, and
nothing ever opens it through LaunchServices — the session-watcher execs the path
inside it, exactly as it execs every other integration.

The **mark** is eleven cells across and nine down, and it is the same drawing on
the bar and on the app icon:

```
..X.....X..
X..X...X..X
X.XXXXXXX.X
XXXXXXXXXXX
XXX.XXX.XXX
XXXXXXXXXXX
.XXXXXXXXX.
..X.....X..
.X.......X.
```

Every cell lands on whole **device** pixels wherever it is drawn — the edges are
rounded in device space, not the widths, so cells that share an edge go on
sharing it — because half-lit edges at thirteen points read as a mistake rather
than as pixel art. It is not an SF Symbol and never will be, so a paint asks for
it by the name `invader` in the field an SF Symbol name goes in, which also means
you can put it on a menu row if you want it there.

The **app icon** is drawn at install time rather than checked in: the same
sprite in Iris on a rounded plum tile, Overlay down to Base — deliberately
neither of the two hues a state wears, so the icon never reads as an agent
waiting for you. Nothing much looks at it — this is an accessory app with no
Dock tile — but `install` builds it anyway, because the same drawing is what
the notifications program puts on every banner.

**Once, after installing, cmd-drag the item where you want it.** It stays there.

**It has to run in your own logged-in session.** There is no menu bar in a
system context, and AppKit's answer to being started where there is no window
server is to abort; this asks first and says so instead.

## Configuring

Everything optional, all of it in this integration's own table. A key nobody
declared is refused **by name**, and so is a colour AppKit cannot draw or a font
this machine does not have — because a misspelled colour that changes nothing
and says nothing is the config bug people give up on.

```toml
[integration.macos-bar.settings]
rows     = 8        # sessions per state before "and n more"
announce = 0        # the name of what just changed, after the marks; off by
                    # default. Nanoseconds, because that is what a Go
                    # time.Duration is: 8000000000 is eight seconds.
sign     = "agent-notify self-signed"   # written by `install --sign`; ad-hoc is fine here
resting  = "0xff8c88a6"  # the mark's colour when nothing is running at all
font     = ""       # the menu bar's own; name one if you have your own glyphs

[integration.macos-bar.settings.symbols]
blocked-on-you = "exclamationmark.triangle.fill"   # an SF Symbol name
working        = "arrow.triangle.2.circlepath"

[integration.macos-bar.settings.glyphs]
blocked-on-you = "▲"                       # the fallback, if the symbol is unknown
"blocked-on-you/permission-prompt" = "▲"   # the (kernel, detail) pair, most specific first

[integration.macos-bar.settings.colors]
blocked-on-you  = "0xffeb6f92"     # 0xAARRGGBB, the spelling every other display takes
finished-a-turn = "labelColor"    # or an AppKit name, which adapts to light mode
```

The defaults are Rosé Pine in urgency order: `0xffeb6f92` (love), `0xfff6c177`
(gold), `0xff9ccfd8` (foam), `0xffc4a7e7` (iris) and `0xcce0def4` (text,
dimmed) — and `resting` above, Subtle, for the mark that stands in when nothing
is running at all, which is the one hue no state wears. A name AppKit does not
know is refused when the config is read.

The colour names understood are `systemRed`, `systemOrange`, `systemYellow`,
`systemGreen`, `systemMint`, `systemTeal`, `systemCyan`, `systemBlue`,
`systemIndigo`, `systemPurple`, `systemPink`, `systemBrown`, `systemGray`,
`labelColor`, `secondaryLabelColor`, `tertiaryLabelColor`,
`quaternaryLabelColor` and `controlAccentColor`.

## Looking at what it thinks

```
agent-notify-macos-bar dump
```

prints the document it would apply — the title, every row, every tooltip — read
straight from the store with no menu bar involved. Every other display here can
simply be looked at; a menu exists only for the second somebody is holding it
open, so this is where the answer lives instead.

## When something happens, without leaving the bar

`announce` is the older answer, off by default now that there are real
notifications. Set it and a state change adds the name of the session it
happened to, after the marks, for a few seconds:

```
  👾 👾     →     👾 👾  agent-notify     →     👾 👾
```

The deadline belongs to the **record**, not to the display: it is `state_since`
plus the window, so any repaint computes the same answer and no repaint can
extend it. Open the menu while it is up and the row it is about is in bold.

**It does not open the menu**, and that is a decision rather than an omission.
An NSMenu opening starts a modal tracking loop that takes the keyboard for as
long as it is up, so a display that opened one by itself would eat what you were
typing, several times an hour, for something you did not ask for.

## How it is built, and why it is worth knowing

Everything that decides what should be on the screen is in Go, and it produces
a `Paint` — a title in coloured spans and a flat list of menu rows — which
crosses into Objective-C as **one JSON string**. `menubar.m` applies it and
makes no decisions at all. So every question this display answers is answered by
a pure function, and the tests for it run on a machine with no screen.

The menu is built when somebody **opens** it, from the last paint, rather than
when a paint arrives. A paint happens whenever any agent anywhere does anything;
a menu is open for a second or two a day.

## Three things about the macOS menu bar worth knowing

- **A status item does not need an app bundle to EXIST, and does need one to be
  usable.** A plain Go binary can own one; it just cannot remember where the
  person put it. A bundle is also what `UNUserNotificationCenter` requires —
  without a bundle identifier it does not return an error, it aborts the process.
- **`NSStatusItem` windows are hosted out of process** — their window number is
  `4294967296` — so they never appear in `CGWindowListCopyWindowInfo`. What can
  see one is the accessibility API, through `AXExtrasMenuBar` on the owning pid,
  which is what this repository's tests use as their oracle. Know its limit: AX
  reports a position and a size for an item the menu bar has decided not to
  draw, so these tests can pass while nothing is on the screen.
- **Go 1.21 builds a cgo binary whose ad-hoc signature is invalid**, and macOS
  26 kills it on launch, before `main`, with no message anywhere. Go 1.26, which
  this module requires, signs it correctly. If you ever see this binary die
  instantly and silently, check `codesign -v` before anything else.

All three measured on macOS 26.6.2, arm64, 2026-09-18.
