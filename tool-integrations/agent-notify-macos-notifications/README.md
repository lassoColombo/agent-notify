<div align="center">
  <h1>agent-notify-macos-notifications</h1>
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
  - [Two macOS displays, installed separately](#two-macos-displays-installed-separately)
- [Installation](#installation)
  - [Prerequisites](#prerequisites)
  - [1. Clone the monorepo and build](#1-clone-the-monorepo-and-build)
  - [2. Make a code-signing identity](#2-make-a-code-signing-identity)
  - [3. Run `install`](#3-run-install)
  - [4. Add the table to agent-notify's config](#4-add-the-table-to-agent-notifys-config)
  - [5. Grant the permission](#5-grant-the-permission)
  - [6. Check it](#6-check-it)
  - [Upgrading, and uninstalling](#upgrading-and-uninstalling)
- [Configuration](#configuration)
  - [Where the file is](#where-the-file-is)
  - [`[integration.macos-notifications]` — core's half](#integrationmacos-notifications--cores-half)
  - [`[…settings]` — this display's half](#settings--this-displays-half)
  - [`sound`](#sound)
  - [`[…settings.preview]`](#settingspreview)
  - [`[…settings.colors]`](#settingscolors)
  - [`sign`](#sign)
  - [Environment](#environment)
- [Commands](#commands)
- [Why macOS makes this hard](#why-macos-makes-this-hard)
  - [Ad-hoc is not signed enough](#ad-hoc-is-not-signed-enough)
  - [Posting is not the same as being talked to](#posting-is-not-the-same-as-being-talked-to)
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

The picture on the **right** is an attachment, the same space invader the menu bar display
wears, drawn in the colour of the state: Love for `blocked-on-you`, Gold for `broke`, Foam
for `finished-a-turn`. A banner and the bar are about the same thing at the same moment,
and an agent that is one colour on one and another colour on the other is two programs
telling you two stories. The icon on the **left** is the app's, fixed for the whole app,
and drawn in Iris precisely so that it never reads as a state.

The notification's identifier is the session's key, so **a second notice about one session
replaces the first** rather than stacking under it. A semaphore with eight banners about
one agent is a worse semaphore than no banners at all.

Everything else about how a banner is presented — banner or alert, grouping, Do Not
Disturb, whether it shows on the lock screen — is macOS's, in System Settings →
Notifications → agent-notify. This is not the place to reimplement System Settings.

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

### Two macOS displays, installed separately

`agent-notify-macos-notifications` and [`agent-notify-macos-bar`](../agent-notify-macos-bar)
are **two displays**, not two halves of one thing. Each is its own binary, in its own `.app`
bundle with its own identifier; each is installed by its own `install`, which prints its own
table — `[integration.macos-notifications]` here, `[integration.macos-bar]` there; each is
configured in its own `[…settings]` section and refuses a key it did not declare, the
other's included; and each is started by its own launch agent, neither knowing the
other exists.

They are separate for a reason that is not tidiness: **macOS files its decision about
notifications against a bundle identifier, and that decision cannot be unmade.** The
program that asks a person for permission should be the program that needs it, with an
identifier of its own and nothing else riding on it. And they are genuinely separable —
somebody may want banners and no item on their bar, somebody else the item and no
interruptions.

Install either, both, or neither. To remove one, delete its table and its bundle; the other
does not notice.

## Installation

### Prerequisites

- **macOS.** There is no other build: this program is Objective-C against AppKit and
  UserNotifications behind a thin Go front. Verified on macOS 26.6.2.
- **Go 1.26 or newer.** If your shell exported `GOROOT` from an outer context it overrides
  the toolchain pinned in `.tool-versions`; `env -u GOROOT go build ./...` is the fix.
- **The Xcode command line tools**, for cgo, and for `codesign`, `security` and `iconutil`,
  all three of which `install` shells out to.
- **agent-notify itself**, built and on your PATH, with its session-watcher running.
- **A code-signing identity** — see [step 2](#2-make-a-code-signing-identity). This is not
  optional and it is not a nicety: an ad-hoc signed bundle is refused by the notification
  system silently and permanently.
- **A logged-in window session.** This has to run as you, in your own graphical session.
  There is nothing to notify into over ssh, and the program says so and exits rather than
  aborting inside AppKit.

### 1. Clone the monorepo and build

Everything lives in one repository: core under `agent-notify/`, the agent-integrations and
the tool-integrations beside it. This module's `go.mod` carries a `replace` pointing at
core's path in the tree, so it is built **in place**, beside the core it links.

```sh
git clone <the agent-notify monorepo> ~/projects/agent-notify
cd ~/projects/agent-notify/tool-integrations/agent-notify-macos-notifications

go build ./...                      # the binary, here in the module directory
go install ./...                    # or straight into $GOBIN, which is what puts it on PATH
```

`agent-notify install <name>` finds an integration by looking for `agent-notify-<name>` on
your PATH, so `go install` is the form that makes `agent-notify install macos-notifications`
work. Either way, what ends up in the config is a path *inside the bundle* rather than this
binary, so the binary itself does not have to stay anywhere in particular.

Nothing is published to a remote yet. A clone of this directory alone does not compile —
the `replace` line comes out when core is published.

### 2. Make a code-signing identity

A self-signed certificate in your login keychain is enough. It does not have to be trusted
and it does not have to come from Apple; macOS only wants the signature to carry an
identity rather than being ad-hoc. Once, ever:

```sh
cat > codesign.cnf <<'EOF'
[ req ]
distinguished_name = dn
prompt             = no
x509_extensions    = codesign
[ dn ]
CN = agent-notify self-signed
[ codesign ]
basicConstraints = critical,CA:false
keyUsage         = critical,digitalSignature
extendedKeyUsage = critical,codeSigning
EOF

openssl req -x509 -newkey rsa:2048 -nodes -days 3650 -config codesign.cnf \
        -keyout key.pem -out cert.pem
openssl pkcs12 -export -out identity.p12 -inkey key.pem -in cert.pem \
        -name "agent-notify self-signed" -passout pass:secret \
        -keypbe PBE-SHA1-3DES -certpbe PBE-SHA1-3DES -macalg sha1
security import identity.p12 -k ~/Library/Keychains/login.keychain-db \
        -P secret -T /usr/bin/codesign -A
```

The `-keypbe` / `-certpbe` / `-macalg` flags are not decoration: OpenSSL 3 otherwise writes
a PKCS#12 whose MAC macOS refuses with *"MAC verification failed"*.

`security find-identity -p codesigning` lists what you have afterwards, spelled the way
`--sign` wants it. `install` checks the name against that list and refuses a name the
keychain does not hold, naming the ones it does.

**Do this before the first run.** An identifier macOS has already decided about cannot be
undecided — see [Ad-hoc is not signed enough](#ad-hoc-is-not-signed-enough).

### 3. Run `install`

```sh
agent-notify install macos-notifications --sign "agent-notify self-signed"
# or, equivalently, the program by its own name:
agent-notify-macos-notifications install --sign "agent-notify self-signed"
```

Two things happen, and only one of them touches your disk.

A signed `.app` bundle is written to `~/Applications/agent-notify-macos-notifications.app`,
holding a **copy** of the binary, an `Info.plist` with the bundle identifier
`io.github.lassocolombo.agent-notify-notifications` and `LSUIElement` so that it never
appears in the Dock or the app switcher, and an icon drawn at install time. Then the whole
bundle is signed. Writing it is idempotent: re-running `install` repairs it, and the copy
goes in through a temporary file and a rename, so a display that is currently running is
upgraded rather than hit with `ETXTBSY`.

And the table you need is printed **on stdout**, with everything else on stderr, so that
somebody who has already decided can redirect it:

```console
$ agent-notify install macos-notifications --sign "agent-notify self-signed"
[integration.macos-notifications]

[integration.macos-notifications.settings]
sign = "agent-notify self-signed"

The bundle is at /Users/you/Applications/agent-notify-macos-notifications.app. Nothing else was written:
put the table above in /Users/you/.config/agent-notify/config.toml when you want this configurable,
and load the launch agent below when you want it running.
…
```

Flags:

| Flag | Default | What it does |
| --- | --- | --- |
| `--sign IDENTITY` | whatever `sign` in the config already says, else ad-hoc | the code-signing identity, as `security find-identity -p codesigning` names it |
| `--app DIR` | `~/Applications` | where to put the `.app` |

There is no `--print`, and there is nothing for one to select: `install` only ever prints
the table, and never writes to your config. Passing it exits 2 with *"flag provided but
not defined"*.

`--sign` is remembered in the printed table so that re-running `install` after a rebuild
signs the same way without the flag. When the flag is absent, `install` reads the identity
back out of your config — reading is not writing, and the value there is yours.

### 4. Add the table to agent-notify's config

`install` deliberately does not write it. Everything in agent-notify's config file is yours,
and the test for whether something belongs there is whether only you can know the answer.
Whether this display should be running is exactly that — and since `enabled` defaults to
true, **the table being there is what turns it on**.

```sh
agent-notify install macos-notifications >> ~/.config/agent-notify/config.toml
# then put the launch agent it printed in ~/Library/LaunchAgents and:
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/io.github.lassocolombo.agent-notify-notifications.plist
```

**agent-notify does not start this display, and that is deliberate.** Every other
integration is a program core runs and waits for. A notifier cannot be written that way,
and posting is not the reason — posting is XPC and a one-shot could do it. The reason is
the tap: macOS delivers it to the process that posted the banner, on that process's main
thread, so a poster that has exited leaves banners nobody can click. There has to be a
process, and launchd keeps one: at login, restarted if it dies, inside your logged-in
session.

That is also why its table has no `binary`. `binary` means "core may run this", and
nothing core runs could answer a tap.

### 5. Grant the permission

Run `check` once, by hand, in your own graphical session:

```console
$ agent-notify-macos-notifications check
binary           /Users/you/Applications/agent-notify-macos-notifications.app/Contents/MacOS/agent-notify-macos-notifications
bundle           io.github.lassocolombo.agent-notify-notifications
focus-session    /Users/you/go/bin/agent-notify
permission       not-determined
permission       provisional (after asking)
delivery         quietly, to Notification Centre only
answers          yes — a tap on a banner reaches this program
notifications    a test banner has been posted

Provisional means DELIVERED QUIETLY: the notification is in Notification
Centre, with no banner and no sound. To get banners, open Notification
Centre, press "Keep…" on one of them and choose "Deliver Prominently".
Until you answer that question the app is not in System Settings either.
```

The program asks for `Alert | Sound | Provisional`, and **provisional is what makes it work
at all**: asking for Alert alone is answered *"Notifications are not allowed for this
application"* with no prompt and no entry in System Settings. With provisional the grant is
immediate and silent, the notifications arrive quietly in Notification Centre, and the app
finally appears in System Settings → Notifications — which is where you promote it.

So the last step is yours and it is in the UI: open Notification Centre, find the test
banner, press **Keep…** and choose **Deliver Prominently**. After that `check` says
`authorized` and `delivery banners, with the default chime`, and System Settings →
Notifications → agent-notify has the usual controls.

`check` reads no config at all — the thing you run when something is broken must not be the
next thing that breaks — so it is also safe to run before there is any config to read.

### 6. Check it

```console
$ agent-notify doctor
agent-notify 0.x.y   darwin/arm64

paths
  root        (default — AGENT_NOTIFY_ROOT is not set)
  config      /Users/you/.config/agent-notify/config.toml
  …
config       ok    2 agent(s), 2 integration(s), keep-ended-sessions 10m0s
…
integrations ok    reported by pid 4711 at 2026-09-23T10:14:02Z
             macos-notifications    connected, pid 4820 [display]
```

`connected` with the `display` role is the whole of the handshake working. Two things
`doctor` will tell you that are worth knowing how to read:

- **`no report yet`** — the session-watcher has not swept since you edited the config. Run
  `agent-notify watcher reload`.
- **`installed -- 1 on your PATH and not mentioned here: macos-notifications`** — the
  program is built and on your PATH and you never added its table. Step 4 did not happen.

To see what it is reacting to, without any notifications in the way:

```sh
agent-notify tail --wake-on kernel,detail,rank,name,cwd,message
```

That is the same subscription this display makes, printed. If a state change shows up there
and no banner arrives, the problem is macOS's side of it — go back to `check`.

### Upgrading, and uninstalling

The bundle holds a signed **copy** of the binary, so rebuilding does not reach it:

```sh
cd ~/projects/agent-notify/tool-integrations/agent-notify-macos-notifications
go install ./...
agent-notify install macos-notifications      # re-signs the bundle with the remembered identity
agent-notify watcher reload
```

To remove it, delete its table from the config, run `agent-notify watcher reload`, and drag
`~/Applications/agent-notify-macos-notifications.app` to the bin. `enabled = false` in the
table does the same thing reversibly. macOS keeps its decision about the identifier either
way; reinstalling later does not have to ask again.

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

### `[integration.macos-notifications]` — core's half

| Key | Type | Default | What it does |
| --- | --- | --- | --- |
| `enabled` | bool | `true` | absence means yes: writing the table is how you ask for it. `false` keeps the table and stops it running |

There is deliberately no `capture-environment` here, and no list of environment variables:
what an integration reads is knowledge it already holds in its own source, and a second copy
in a file you edit is a copy that can disagree.

### `[…settings]` — this display's half

```toml
[integration.macos-notifications.settings]
sign  = "agent-notify self-signed"   # written by `install --sign`
sound = "Submarine"                  # true, false, or the name of a sound

[integration.macos-notifications.settings.preview]
lines = 8
width = 60

[integration.macos-notifications.settings.colors]
blocked-on-you = "0xffeb6f92"
```

| Key | Type | Default | What it changes |
| --- | --- | --- | --- |
| `sign` | string | — | the code-signing identity `install` uses. Nothing at runtime reads it |
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
$ agent-notify-macos-notifications check --sound Sumbarine
agent-notify-macos-notifications check: "Sumbarine" is not a sound on this machine.
There is: Basso, Blow, Bottle, Frog, Funk, Glass, Hero, Morse, Ping, Pop, Purr, Sosumi, Submarine, Tink.
A sound of your own goes in ~/Library/Sounds.
```

What that check *cannot* tell you is whether the daemon will take a name this program did
find on disk. Only your ears can:

```sh
agent-notify-macos-notifications check --sound Submarine
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
[integration.macos-notifications.settings.preview]
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
[integration.macos-notifications.settings.colors]
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

### Environment

This program reads no environment variable of its own. Three reach it anyway:

| Variable | Effect |
| --- | --- |
| `AGENT_NOTIFY_ROOT` | moves agent-notify's state directory, runtime directory and config file beneath it — one variable, so that running an isolated instance is one step |
| `XDG_CONFIG_HOME` | where the config file is looked for, when the root is not set |
| `TMPDIR` | holds `agent-notify-invaders/`, the fixed directory the coloured PNGs are cached in — one file per colour, about thirty kilobytes each, reused across restarts |

The path to `agent-notify` itself is not found on PATH, because a launchd job's PATH is
`/usr/bin:/bin` and nothing else. It comes from core's own `agent-notify-binary` setting, and
it is needed twice over: it is what tapping a banner runs, and it is what this display watches
(`agent-notify tail --json`). A display that cannot locate it refuses to start rather than
discovering it when somebody taps a banner.

## Commands

```
agent-notify-macos-notifications                    stay connected and notify
agent-notify-macos-notifications check              will macOS deliver? post a test banner
agent-notify-macos-notifications check --sound NAME hear a sound before configuring it
agent-notify-macos-notifications install            build the bundle, print the table
agent-notify-macos-notifications install --sign ID  …signed with that identity
agent-notify-macos-notifications install --app DIR  …with the bundle somewhere else
agent-notify-macos-notifications --help             the same list
```

With no arguments it is the long-lived subprocess the session-watcher runs, and you do not
normally run it yourself. It refuses to start, with a message, in each of the three ways this
cannot work: no logged-in window session, no bundle around it, or macOS declining the
permission request outright. Otherwise it logs one line saying what will happen —

```
level=INFO msg=notifying delivery="banners, with Submarine"
```

— and, a few seconds later, warns if it turns out not to be answering on its main thread.

`check` is the diagnostic, and it exists because every way this fails is silent: an ad-hoc
signature is refused without a prompt, a bare binary aborts the process, and a provisional
grant delivers to Notification Centre and shows nothing at all. A program that posts
notifications nobody ever sees looks exactly like one that is broken, and there is nowhere
to look it up. It reports the binary, the bundle identifier, whether there is a session to
notify into, where `focus-session` resolves to, the permission status before and after
asking, how banners will arrive, and whether a tap would reach the process — then posts a
test banner and closes with a paragraph about what that permission status means. It exits
non-zero when it cannot get that far at all: no bundle, no logged-in session, a sound name
that is not on this machine. A `denied` it *could* ask about is reported and exits zero,
because the answer is a fact about your machine rather than a fault in the run.

## Why macOS makes this hard

**Without a bundle identifier, `UNUserNotificationCenter` does not fail — it terminates the
process.** `bundleProxyForCurrentProcess is nil`, raised inside a `dispatch_once`, with
nothing to catch. So the bare binary cannot ask for permission, cannot post anything, and
cannot even find out that it cannot. It refuses to start, and every call that could reach
the notification centre is guarded before it gets there. One of those guards exists because
a test posted from the bare test binary and killed the suite.

The executable inside the bundle is a **copy**, not a symlink, and that is not the obvious
choice: `codesign` refuses a bundle whose main executable is a symlink — *"the main
executable or Info.plist must be a regular file"* — and an unsigned bundle is invisible to
the notification system.

### Ad-hoc is not signed enough

This is the part worth knowing before you debug it yourself. All measured on macOS 26.6.2,
2026-09-18:

- **Ad-hoc signature** (`codesign -s -`, which is what you get without `--sign`):
  `requestAuthorization` answers *"Notifications are not allowed for this application"*. No
  prompt appears, the status goes to `denied` without anybody being asked, and the app never
  appears in `com.apple.ncprefs` — so it is not in System Settings either, and there is
  nowhere to go and turn it on. Tested from `/private/tmp` and from `~/Applications`,
  registered with `lsregister`, with a valid signature. The location is not the variable;
  the identity is.
- **A self-signed certificate** with the code-signing extended key usage, imported into the
  login keychain and *not* trusted: the prompt appears. That is the whole difference. macOS
  wants an identity, not a trusted one and not Apple's. It is also stable across rebuilds
  where an ad-hoc signature is not, so the permission you grant survives the next
  `go build`.
- **`UNAuthorizationOptionProvisional` is required as well.** Asking for Alert alone is
  refused even when signed.
- **A decision macOS makes about a bundle identifier cannot be unmade.** An identifier that
  was ever used with an ad-hoc signature keeps its `denied` after being signed properly, is
  absent from System Settings, and has no reset. Two identifiers were burned finding that
  out; the one in `bundle.go` is the third. Pick it before the first run and sign from the
  start.

### Posting is not the same as being talked to

The program has to **be an application**, and for a day it was not. Nothing called
`[NSApplication sharedApplication]`, so `NSApp` was nil, `[NSApp run]` was a message to nil
that returned at once, and the main queue was never drained.

Everything still worked. Permission was granted, banners arrived, `check` was green —
because posting a notification is XPC and does not need a run loop. Only the way *back* was
dead: every callback macOS has arrives on the main queue, so clicking a banner was answered
with *"the application is not responding properly"*, while LaunchServices quietly launched a
second copy of the app to look for somebody at home.

So `check` asks the question outright, and the program asks it about itself a few seconds
after starting:

```
answers          yes — a tap on a banner reaches this program
```

It is asked from a goroutine while the main thread runs the loop, because that is the
arrangement being tested: asking from the thread that is supposed to be answering would say
"no" however healthy it was.

## How it is built

Everything that decides whether there is anything to say is in Go — `notify.go`, which is
the whole of the edge-making, and `preview.go`, which is the whole of what a banner says.
`notifier.m` posts what it is handed and answers questions about what macOS has decided; it
is the only place in the program that knows AppKit exists. The main goroutine is locked to
the starting thread in `init`, because NSApplication checks, and AppKit owns that thread from
`Run` until `Stop`.

`notifier.m` also draws the app icon, because that icon is the picture on every banner: a
rounded plum tile with a space invader on it, eleven cells across, in Rosé Pine's Iris. It
is drawn at install time rather than checked in — a binary in a repository is a thing nobody
can review in a diff — and written in sRGB rather than the calibrated space AppKit draws in,
because a PNG straight out of AppKit holds `#b693e1` where `#c4a7e7` was asked for.

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

The coloured invader beside the text is an attachment, and macOS **moves** the file into its
own store when the request is added — so the path is gone afterwards, and the cache in
`invaderpngs.go` is built around redrawing whatever has been taken away, at one `stat` per
notification.

`go test ./...` runs against the real macOS on this machine. There is no fake worth writing:
everything here is a fact about `UNUserNotificationCenter`, and a stub would only agree with
whatever it already assumed. The test binary is not in a bundle, which is the interesting
half — every guard in `notifier.m` is exercised by the case that would otherwise kill the
process.

## What is unfinished

- **Nothing is published.** There are no releases, no tags and no remote: you clone the
  monorepo and build. The `replace` line in `go.mod` pointing at `../../agent-notify` comes
  out when core is published, which is the milestone this whole distribution story belongs
  to.
- **The installation is not one step.** `install` builds the bundle and prints a table you
  then paste into a file yourself, and the signing identity is yours to make first. That is
  deliberate about the table and merely unfinished about the certificate.
- **macOS only.** There is no Linux notifier, and this module is not the place one would go.
- **No notification actions.** A banner can be tapped and that focuses the session; it
  carries no buttons, no reply field and no way to answer an agent from Notification Centre.
- **`doctor` does not check this integration's own half** — whether the bundle is signed,
  whether macOS will deliver, whether a tap would be answered. `check` does, and you have to
  know to run it.
</content>
</invoke>
