<div align="center">
  <h1>agent-notify-macos-notifier</h1>
  <p><strong>A real macOS notification when one of your agents wants you</strong></p>
  <p>
    The session's name, what happened to it, and what it last said<br>
    Tap it and you are in that session, through every layer it is buried under<br>
    One banner per session, replaced rather than stacked
  </p>
</div>

---

```
  ┌────────────────────────────────────────────────┐
  │ 👾   lenny                             👾      │
  │      blocked on you                            │
  │      Shall I delete the branch?                │
  └────────────────────────────────────────────────┘
      ↑                                    ↑
   the app, in Iris              the state, in its own colour
```

- [What it is](#what-it-is)
  - [What a banner says](#what-a-banner-says)
  - [What earns one](#what-earns-one)
  - [What tapping one does](#what-tapping-one-does)
  - [The only macOS display there is](#the-only-macos-display-there-is)
- [Installation](#installation)
  - [Prerequisites](#prerequisites)
  - [1. Install alerter](#1-install-alerter)
  - [2. Clone the monorepo and build](#2-clone-the-monorepo-and-build)
  - [3. Run `install`](#3-run-install)
  - [4. Check it](#4-check-it)
  - [Upgrading, and uninstalling](#upgrading-and-uninstalling)
- [Configuration](#configuration)
  - [Where the file is](#where-the-file-is)
  - [`[integration.macos-notifier]` — core's half](#integrationmacos-notifier--cores-half)
  - [`[…settings]` — this display's half](#settings--this-displays-half)
  - [`sound`](#sound)
  - [`[…settings.preview]`](#settingspreview)
  - [`[…settings.colors]`](#settingscolors)
  - [`alerter`](#alerter)
  - [Environment](#environment)
- [Commands](#commands)
- [Why this is an exec and not an API call](#why-this-is-an-exec-and-not-an-api-call)
- [How it is built](#how-it-is-built)
- [What is unfinished](#what-is-unfinished)

---

## What it is

A **display** integration for [agent-notify](../../agent-notify): it connects to the
session-watcher, is handed the state of every agent running on your machine, and posts a
macOS notification when one of them reaches a state worth interrupting you for. It only
ever reads. It writes nothing back into a session, and the only thing it ever runs is
`agent-notify focus-session`.

### What a banner says

Three lines, and each of them comes out of one field of the session's record:

| Line | Where it comes from |
| --- | --- |
| title | the session's display name — its `name`, else the basename of its `cwd`, else the first eight characters of its id |
| subtitle | the state as a person would say it: `kernel`, plus `/detail` when there is one, with the hyphens turned into spaces — `blocked on you`, `working/compacting` |
| body | what the agent last said (`message`), cleaned and wrapped — eight lines of sixty characters by default, the last one cut with an ellipsis |

The picture beside the text is the same space invader the menu bar display wears, drawn in
the colour of the state: Love for `blocked-on-you`, Gold for `broke`, Foam for
`finished-a-turn`. A banner and the bar are about the same thing at the same moment, and an
agent that is one colour on one and another colour on the other is two programs telling you
two stories. It is passed twice, as `--content-image` and as `--app-icon`, so that the
colour is what the banner is recognised by rather than alerter's own icon.

The notification's identifier is the session's key, so **a second notice about one session
replaces the first** rather than stacking under it. A semaphore with eight banners about
one agent is a worse semaphore than no banners at all.

Everything else about how a banner is presented — banner or alert, grouping, Do Not
Disturb, whether it shows on the lock screen — is macOS's, in System Settings →
Notifications → **alerter**, which is whose identity these arrive under. This is not the
place to reimplement System Settings.

### What earns one

A notification is the one **edge** in a system that is otherwise level-triggered: every
other display is handed the current state and repaints, which is what makes a display
impossible to write wrongly, but a banner is posted once, at a moment. So this is the one
display that reads `View.Changed` — what moved — and ignores the sessions entirely.

Three rules, and every one of them is about the record in hand rather than about anything
remembered:

- **The state has to have moved.** This display asks to be woken for `message` as well, so
  a blocked agent revising what it said arrives here again and again. Each change carries
  the kernel its session moved *from*, and that is what tells the two apart.
- **It has to be worth interrupting for**, which is core's own `WantsYou`: a rank above
  `working`. That is `blocked-on-you`, `broke` and `finished-a-turn` today, judged by the
  rank the record carries rather than by a list compiled into this build — so a state a
  newer core ships is judged correctly by a display that has never heard of it.
- **It has to be live.** The transition to `ended` reaches even a display that asked for no
  ended sessions, because that is how a bar learns to take a row away, and "it finished"
  is not something to interrupt anybody with.

This program remembers nothing at all between views, and that is the point rather than a
tidy-up. What it was last shown is the SDK's to keep, which is the only place that can keep
it right across a reconnection and a session-watcher restart — two things this program
cannot see happen. Two consequences you can rely on: **starting it never opens with a burst of
banners** for every agent that happens to be blocked, and **the session-watcher restarting
posts nothing**.

The fields it asks the watcher to wake it for are exactly the ones a banner is made of:

```go
[]string{"kernel", "detail", "rank", "name", "cwd", "message"}
```

A test moves one record field at a time and fails if a banner comes out different for
something not on that list, which is the failure this system has no other way of catching:
a field a renderer reads and the declaration omits means a display that is right when it
starts and wrong an hour later, with nothing logged anywhere.

### What tapping one does

It runs the same `agent-notify focus-session --quiet <key>` every other integration runs,
bounded at thirty seconds. Where a session lives — which window, which multiplexer, which
pane — is the containers' business, and anything that went and looked would be a second
implementation of it, wrong in its own way.

That is also why `install` writes no `capture-environment` line: a notification does not
care where a session lives, only what it is doing.

### The only macOS display there is

There used to be two. `agent-notify-macos-bar` put a semaphore on the menu bar, in its own
`.app`, kept alive by its own launch agent, and it was **deleted on 2026-10-07 (D-87)**. So
a banner is the whole of what macOS will tell you, and the surfaces that answer "which
session wants me" are [zellij](../agent-notify-zellij), [aerospace](../agent-notify-aerospace-container)
and the [picker](../agent-notify-picker).

The two were deliberately separate — macOS files its decision about notifications against a
bundle identifier and that decision cannot be unmade, so the program that asked a person for
permission had to be the one that needed it. Neither half of that argument survives: this
program has no bundle of its own any more (D-86), and there is no second display to keep
apart from.

## Installation

### Prerequisites

- **macOS.** Verified on macOS 26.6.2, arm64.
- **[alerter](https://github.com/vjeantet/alerter)**, which is what actually puts the banner
  on the screen: `brew install vjeantet/tap/alerter`. It is a small Swift program on
  `UNUserNotificationCenter`, code-signed and notarised by Apple, and it needs macOS 13 or
  newer.
- **Go 1.26 or newer.** If your shell exported `GOROOT` from an outer context it overrides
  the toolchain pinned in `.tool-versions`; `env -u GOROOT go build ./...` is the fix.
- **agent-notify itself**, built and on your PATH.
- **Nothing else.** No Xcode command line tools, no `codesign`, no keychain identity, no
  bundle. This module builds with `CGO_ENABLED=0`.

### 1. Install alerter

```sh
brew install vjeantet/tap/alerter
```

**Banners arrive under alerter's identity, not under one of ours**, and that is the whole
trade this module makes. macOS will not take a notification from a process without a bundle
it has registered, and owning such a bundle meant a signed `.app`, a self-signed certificate
made by hand, a reinstall after every build, and an identifier macOS would make a permanent
and unreversible decision about. Borrowing one costs a dependency and costs you the per-app
row in System Settings → Notifications being called something else. See
[Why this is an exec](#why-this-is-an-exec-and-not-an-api-call).

### 2. Clone the monorepo and build

Everything lives in one repository: core under `agent-notify/`, the agent-integrations and
the tool-integrations beside it. `make install` at the root builds every module into
`$(go env GOPATH)/bin`, which has to be on your PATH — finding `agent-notify-<name>` there
by name is the whole of what core knows about an integration.

```sh
git clone <the agent-notify monorepo> ~/projects/agent-notify
cd ~/projects/agent-notify
make install                        # or: make install GOBIN=/opt/homebrew/bin
```

### 3. Run `install`

```sh
agent-notify install macos-notifier
# or, equivalently, the program by its own name:
agent-notify-macos-notifier install
```

One thing happens: the table is filed in `conf.d/macos-notifier.toml` beside your
`config.toml`, which is never written. It carries this program's absolute path and alerter's,
because the session-watcher runs the first and the first runs the second, and neither has
your shell's PATH (D-67).

```toml
[integration.macos-notifier]
binary = "/opt/homebrew/bin/agent-notify-macos-notifier"

[integration.macos-notifier.settings]
alerter = "/opt/homebrew/bin/alerter"
```

`enabled` defaults to true and the table being there is what turns it on, so there is nothing
left to do. `agent-notify watcher reload` if you want it picked up without waiting.

**`binary` means "core may run this"**, and for as long as this program posted its own
notifications it could not be there: a tap is delivered to the process that posted the
banner, so there had to be a process, and launchd had to keep it. alerter is that process
now.

### 4. Check it

```console
$ agent-notify doctor
...
integrations ok    reported by pid 1670 at 2026-10-07T21:09:19Z
             macos-notifier    drawn when something it watches moves
                                    answers render — built against 0.0.0-dev
```

That line is the handshake working. Then the program's own:

```console
$ agent-notify-macos-notifier check
alerter          /opt/homebrew/bin/alerter
focus-session    /opt/homebrew/bin/agent-notify
sound            with the default chime
notifications    a test banner has been posted
```

If nothing appeared, macOS is deciding that and not this program: look in System Settings →
Notifications under **alerter**.

To see what it is reacting to, without any notifications in the way:

```sh
agent-notify tail --wake-on kernel,detail,rank,name,cwd,message
```

That is the same subscription this display makes, printed.

### Upgrading, and uninstalling

```sh
make install                     # at the root, and that is the whole upgrade
```

There is no second copy anywhere, so nothing goes stale and `install` does not have to be
re-run — unless alerter moved, which is the one thing the table remembers about your machine.

`agent-notify uninstall macos-notifier` removes the drop-in. Nothing else exists to
remove.

## Configuration

### Where the file is

`~/.config/agent-notify/config.toml`, or `$XDG_CONFIG_HOME/agent-notify/config.toml`, or
`$AGENT_NOTIFY_ROOT/config.toml` when that is set. This program never works that path out
for itself — it asks the SDK, so that a path derived twice cannot differ once.

Everything optional. There is no file by default and running without one is supported,
except that without the table the session-watcher never starts this at all.

**A key nobody declared is refused by name**, and the program exits at startup saying which
one. A misspelled setting that changes nothing and says nothing is the config bug people
give up on.

### `[integration.macos-notifier]` — core's half

| Key | Type | Default | What it does |
| --- | --- | --- | --- |
| `enabled` | bool | `true` | absence means yes: writing the table is how you ask for it. `false` keeps the table and stops it running |

There is deliberately no `capture-environment` here, and no list of environment variables:
what an integration reads is knowledge it already holds in its own source, and a second copy
in a file you edit is a copy that can disagree.

### `[…settings]` — this display's half

```toml
[integration.macos-notifier.settings]
alerter = "/opt/homebrew/bin/alerter"   # written by `install`
sound   = "Submarine"                   # true, false, or the name of a sound

[integration.macos-notifier.settings.preview]
lines = 8
width = 60

[integration.macos-notifier.settings.colors]
blocked-on-you = "0xffeb6f92"
```

| Key | Type | Default | What it changes |
| --- | --- | --- | --- |
| `alerter` | string | PATH | where alerter is, absolute. See [below](#alerter) |
| `sound` | bool or string | `true` | what a banner plays. See below |
| `preview.lines` | int, ≥ 1 | `8` | how many lines of the agent's last message the body carries |
| `preview.width` | int, ≥ 16 | `60` | characters per line before it wraps |
| `colors.<state>` | string, `0xAARRGGBB` | the menu bar's table | the colour the invader beside the text is drawn in |

Everything that can fail is resolved **once, at startup, where there is still somebody to
tell**: a sound macOS cannot find, a colour that is not hex, a preview too small to read, a
core binary that cannot be located. Each of those is a refusal with a message, not a banner
that quietly comes out wrong forever.

### `sound`

One key taking three kinds of value, rather than two keys that could disagree and would
need a winner nobody would guess:

- **`true`, or no line at all** — the usual notification chime.
- **`false`** — no sound is attached at all, which is silence whatever else is set: there is
  nothing for macOS to play.
- **a name** — `"Submarine"`, `"Glass"`, anything in `/System/Library/Sounds`, and anything
  you put in `~/Library/Sounds`.

A name is looked up in `~/Library/Sounds`, `/Library/Sounds`, `/Network/Library/Sounds` and
`/System/Library/Sounds`, most specific first, and it is forgiving about what you write and
exact about what it passes on: `submarine`, `Submarine` and `Submarine.aiff` are the same
file, and all three are handed to macOS as `Submarine.aiff`, which is what its own lookup
matches on. Not the app bundle, although macOS would look there too — `install` rewrites the
bundle from scratch, so a sound kept inside it is a sound that disappears.

**A name that is not on this machine is refused when the config is read**, and the reason is
worth knowing before you try to debug it yourself. `[UNNotificationSound soundNamed:]`
accepts any string whatever, returns an object, and reports nothing — the object is
identical for a real name and a nonsense one. Resolution happens later, inside the
notification daemon, and a name it cannot find becomes **the default chime**. So a
misspelling that got through would be the wrong noise forever with nothing anywhere to read
about it.

```console
$ agent-notify-macos-notifier check --sound Sumbarine
agent-notify-macos-notifier check: "Sumbarine" is not a sound on this machine.
There is: Basso, Blow, Bottle, Frog, Funk, Glass, Hero, Morse, Ping, Pop, Purr, Sosumi, Submarine, Tink.
A sound of your own goes in ~/Library/Sounds.
```

What that check *cannot* tell you is whether the daemon will take a name this program did
find on disk. Only your ears can:

```sh
agent-notify-macos-notifier check --sound Submarine
```

which posts the test banner with it. That is how you audition a sound before writing it down.

Sound is here, and nothing else about how a banner is presented is, because it is the one of
those a person decides per **machine** rather than once: the same config wants one noise on
the desktop and none on the laptop you take into meetings, and macOS's own switch is per-app
with nothing between chime and silence. System Settings still has the last word — sound
turned off there is silence whatever this file says.

### `[…settings.preview]`

The body of a banner is the agent's last message alone, with no state line in front of it:
the state has already been said in the subtitle, and saying it twice is noise. It is wrapped
at `width`, on spaces where it can be, keeping the newlines and the blank lines the agent
wrote — the difference between a list and a paragraph — and cut to `lines`, with the last
one ending in an ellipsis.

```toml
[integration.macos-notifier.settings.preview]
lines = 3     # a terser banner
width = 48
```

`lines` below 1 and `width` below 16 are refused at startup. A banner is not a document:
what it is for is recognising which of your agents this is, and whether what it last said is
the thing you are waiting for.

### `[…settings.colors]`

The keys are states, most specific first — a kernel, or a `kernel/detail` pair — over the
defaults this display brings with it:

```toml
[integration.macos-notifier.settings.colors]
blocked-on-you                   = "0xffeb6f92"   # love — answer this now
broke                            = "0xfff6c177"   # gold — something went wrong
finished-a-turn                  = "0xff9ccfd8"   # foam — there is something to read
"blocked-on-you/permission-prompt" = "0xffebbcba"  # a pair is more specific than its kernel
```

Those three are the defaults, and they are the menu bar's, written out again so that the two
agree by construction. Change them here and on the bar together, or the two displays start
telling you different stories. `working` and `idle` have entries too and never appear on a
banner; they are there so that the table is the same table.

**Hex only, as `0xAARRGGBB`.** AppKit's colour names are no use: the invader is drawn into a
PNG that macOS composites onto a surface of its own, where a colour that adapts to the
appearance has nothing to adapt to. The alpha is written and then dropped for the same
reason. A value that is not hex is refused when the config is read, naming the key — because
the alternative is a banner that quietly arrives with no picture, which looks exactly like a
banner nobody bothered to draw one for.

A state a newer core ships and this build has never met is drawn in the colour of the known
state nearest it in rank, which is "something roughly this important" rather than nothing at
all.

### `alerter`

```toml
[integration.macos-notifier.settings]
alerter = "/opt/homebrew/bin/alerter"
```

Where alerter is, absolute, written by `install` out of the PATH you ran it with. Left empty
it is looked up on PATH, which is right for a shell and wrong for anything core starts
(D-67).

It is **checked when the config is read**, not when there is something to say: a `brew
upgrade` that moved it would otherwise be a display that silently stops interrupting
anybody, discovered on the one occasion it mattered. A path that is not there is a refusal
naming it.

### Environment

This program reads no environment variable of its own. Three reach it anyway:

| Variable | Effect |
| --- | --- |
| `AGENT_NOTIFY_ROOT` | moves agent-notify's state directory, runtime directory and config file beneath it — one variable, so that running an isolated instance is one step |
| `XDG_CONFIG_HOME` | where the config file is looked for, when the root is not set |
| `TMPDIR` | holds `agent-notify-invaders/`, the fixed directory the coloured PNGs are cached in — one file per colour, about thirty kilobytes each, reused across restarts |

The path to `agent-notify` itself is not found on PATH, because nothing core starts has your
shell's. It comes from core's own `agent-notify-binary` setting, and it is what tapping a
banner runs. A display that cannot locate it refuses to render rather than discovering it
when somebody taps a banner.

## Commands

```
agent-notify-macos-notifier render     post banners for what moved (core runs this)
agent-notify-macos-notifier check      post a test banner and say what would carry it
agent-notify-macos-notifier install    file the [integration.macos-notifier] table
agent-notify-macos-notifier uninstall  remove it
agent-notify-macos-notifier --help     the same list
```

`render` is core's, and you do not run it yourself: the session-watcher runs it with the
view on stdin whenever something this display asked about moves, and it posts and exits.

There is one more, `hold`, and it is not for a person. It is what a detached copy of this
program runs to hold ONE banner: it waits for alerter, and focuses the session if the answer
was a tap. It is a command rather than a goroutine because the waiting has to outlive the
render, which core kills after five seconds.

`check` is the diagnostic. It reports where alerter is, where `focus-session` resolves to,
what a banner will sound like, and then posts one through exactly the path a real banner
takes. What it cannot tell you is whether anything appeared on the screen — macOS decides
that, under alerter's name in System Settings — so the last check is you, looking.

## Why this is an exec and not an API call

This program used to post its own notifications, in four hundred lines of Objective-C
against `UNUserNotificationCenter`, and the reason it was that much is worth keeping: **none
of it was about notifications, all of it was about identity.** All measured on macOS 26.6.2.

- **Without a bundle identifier, `UNUserNotificationCenter` does not fail — it terminates
  the process**, `bundleProxyForCurrentProcess is nil` raised inside a `dispatch_once`, with
  nothing to catch. So the program had to live inside a `.app`.
- **An ad-hoc signature is refused**: *"Notifications are not allowed for this
  application"*, no prompt, the status going to `denied` without anybody being asked, and the
  app absent from System Settings — so there was nowhere to go and turn it on. A self-signed
  certificate in the login keychain, untrusted, worked. macOS wants an identity, not a
  trusted one and not Apple's. So the `.app` had to be signed, with a certificate made by
  hand, and because `codesign` refuses a symlinked main executable the bundle held a **copy**
  — which went stale on every rebuild.
- **A decision macOS makes about a bundle identifier cannot be unmade.** An identifier ever
  used with an ad-hoc signature keeps its `denied` after being signed properly, is absent
  from System Settings, and has no reset. Two were burned finding that out.

An embedded `Info.plist` was tried as a way out and does not reach this far
[measured 2026-10-07]: a bare Go binary with an `__TEXT,__info_plist` section does get a real
`bundleIdentifier` and a preferences domain that survives the process, and still aborts on
`UNUserNotificationCenter`, because what that wants is LaunchServices registration of a
bundle on disk.

So the identity is borrowed. alerter is a `.app` somebody else maintains, signed and
notarised by Apple, and three things about it were measured before this was built
[2026-10-07, alerter 26.5]:

- **A second banner in the same `--group` reaps the first.** The superseded alerter exits by
  itself, printing `@CLOSED`. So nothing here tracks children: one banner per session,
  replaced rather than stacked, is what `--group` already means.
- **A detached alerter outlives whatever started it**, reparented to pid 1, still holding
  its banner. That is what lets this be a render rather than a resident process.
- **A tap is `@CONTENTCLICKED` on stdout**, and the close button, a timeout and being
  replaced are three other words. Only the first focuses anything — the other three are
  somebody declining to be interrupted, and acting on them would take you to a session you
  had just dismissed. That rule is the one thing in this module with a test per outcome.

The cost is a dependency that is not in this repository, and banners that arrive under
alerter's name rather than agent-notify's. What it buys is no bundle, no `codesign`, no
keychain identity, no launch agent, no copy to go stale, and no identifier of ours for macOS
to make an irreversible decision about (D-86).

## How it is built

Everything is Go, and the module builds with `CGO_ENABLED=0`: there is no Objective-C, no
cgo and no Xcode command line tools in the build.

`notify.go` is the whole of the edge-making — which transitions are worth interrupting
somebody for — and `preview.go` is the whole of what a banner says. `alerter.go` is the only
file that knows macOS exists, and it is an `exec.Command`: it turns a `Notice` into alerter's
flags, starts a detached copy of this program to hold the banner, and reads one word back.

`invader.go` draws the picture with `image/png`: a rounded plum tile with a space invader on
it, eleven cells across and nine down, in the colour of the state. It used to be AppKit, and
the only reason it could be was that Cocoa had to be linked anyway.

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

macOS **moves** an attachment into its own store when the request is added — so the path is
gone afterwards, and the cache in `invaderpngs.go` is built around redrawing whatever has
been taken away, at one `stat` per notification.

`go test ./...` needs nothing installed. A stub called `alerter` on PATH is what the settings
tests resolve against, and the rule about which answer focuses a session is tested by running
`hold` as its own process against a stub that prints each of alerter's four words in turn —
which is what `hold` always is in production. Three of the deleted sketchybar module's tests
drove a live daemon and were red on every machine without one (D-79); this module does not
repeat that.

## What is unfinished

- **Nothing is published.** There are no releases, no tags and no remote: you clone the
  monorepo and build. The `replace` line in `go.mod` pointing at `../../agent-notify` comes
  out when core is published.
- **Banners wear alerter's name.** System Settings → Notifications files them under
  **alerter**, so turning them off there turns off anything else on the machine that uses it,
  and the per-app controls are not agent-notify's. alerter's `--sender` can impersonate
  another bundle identifier and is left at its default; whether pointing it somewhere better
  is an improvement or a lie has not been decided.
- **macOS only.** There is no Linux notifier, and this module is not the place one would go.
- **No notification actions.** A banner can be tapped and that focuses the session; it
  carries no buttons and no reply field, although alerter has both.
- **Nothing bounds an undismissed banner.** alerter has `--timeout` and this does not pass
  it, so a banner nobody touches is held by a process until a later banner for the same
  session replaces it. Bounded by the number of live sessions, and unbounded in time.
