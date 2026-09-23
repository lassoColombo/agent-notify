# agent-notify-macos-notifications

macOS's own **display** integration for [agent-notify](../../agent-notify), and
the second of two: [agent-notify-macos-bar](../agent-notify-macos-bar) draws the
semaphore on your menu bar, and this one interrupts you. A real notification
when one of your agents wants you, and tapping it takes you to that session.

```
  ┌────────────────────────────────────────────────┐
  │ 👾   lenny                             👾      │
  │      blocked on you                            │
  │      Shall I delete the branch?                │
  └────────────────────────────────────────────────┘
      ↑                                    ↑
   the app, in Iris              the state, in its own colour
```

The session's name, what happened to it, and what it last said. A second
notification about the same session **replaces** the first rather than stacking
under it — a semaphore with eight banners about one agent is a worse semaphore
than no banners at all.

**The mark on the right is the same alien the menu bar wears, in the same
colour**: Love for `blocked-on-you`, Gold for `broke`, Foam for
`finished-a-turn`. A banner and the bar are about the same thing at the same
moment, and an agent that is one colour on one and another colour on the other
is two programs telling you two stories.

It is an **attachment**, which is the only per-notification picture macOS has —
the app icon on the left is fixed for the whole app, and it is drawn in Iris
precisely so that it never reads as a state. macOS *moves* an attachment into
its own store when the request is added, so the file is gone afterwards and the
cache that draws them (`marks.go`) is built around redrawing whatever has been
taken away.

Everything else about them is macOS's: banner or alert, sound, grouping, Do Not
Disturb, all in System Settings → Notifications → agent-notify.

## Two displays, installed separately

`agent-notify-macos-notifications` and [`agent-notify-macos-bar`](../agent-notify-macos-bar)
are **two displays**, not two halves of one thing. Each:

- is its own repository and its own binary, in its own `.app` bundle with its
  own identifier;
- is installed by its own `install`, which adds its own table to
  `~/.config/agent-notify/config.toml` and touches nothing else —
  `[integration.macos-notifications]` here, `[integration.macos-bar]` there;
- is configured in its own `[…settings]` section, and refuses a key it did not
  declare, the other's included;
- is started, supervised and retired separately by the session-watcher, and
  neither knows the other exists.

Install either, both, or neither. To remove one, delete its table and its
bundle; the other does not notice. Both are handed the same state by the
watcher, so they cannot disagree about what is happening — only about how to
show it.

## Why this is not part of the menu bar display

[agent-notify-macos-bar](../agent-notify-macos-bar) puts the semaphore on your
menu bar, and for a while it posted the notifications too. They are two
repositories now for one reason, and it is not tidiness: **macOS files its
decision about notifications against a bundle identifier, and that decision
cannot be unmade.** An identifier that was ever refused stays refused. The
program that asks a person for permission should therefore be the program that
needs it, with an identifier of its own and nothing else riding on it.

And they are genuinely separable. Somebody may want banners and no item on their
bar; somebody else the item and no interruptions. Run both if you want both —
neither knows the other exists, they read the same store, and core's own rule
for *what wants you* is written in both, so they cannot disagree about it.

## What earns a notification

Anything more urgent than working: `blocked-on-you`, `broke`, `finished-a-turn`.
That is core's own ordering rather than a list written here, so an
agent-integration that adds a state does not have to come back and edit this —
and it is the same predicate the menu bar uses to decide what flickers.

A notification is the one **edge** in a system that is otherwise level-triggered
(R22), so it is the one place that remembers anything: the `state_since` it last
spoke about, per session. The first view after starting is never announced —
otherwise a restart opens with a banner for every agent that happens to be
blocked, which is the past telling you about itself. A session that has been
working for an hour and then asks you something is announced the moment it does,
because what is watched is `state_since` moving and not how long ago it moved.

## Installing

```
agent-notify-macos-notifications install --sign "agent-notify self-signed"
agent-notify-macos-notifications install            # builds the bundle, prints the table
agent-notify-macos-notifications install --app DIR  # put the bundle somewhere else
agent-notify-macos-notifications check              # will macOS deliver? post a test banner
agent-notify-macos-notifications check --sound Submarine   # hear one before configuring it
```

Two things happen. A signed `.app` bundle is written to `~/Applications`, and
this goes into agent-notify's config:

```toml
[integration.macos-notifications]
binary = "/Users/you/Applications/agent-notify-macos-notifications.app/Contents/MacOS/agent-notify-macos-notifications"

[integration.macos-notifications.settings]
sign = "agent-notify self-signed"
```

The `sign` line is remembered so that re-running `install` after a rebuild signs
the same way. The session-watcher starts it; there is nothing else to install.

## Why there is a bundle

**Without a bundle identifier, `UNUserNotificationCenter` does not fail — it
terminates the process.** `bundleProxyForCurrentProcess is nil`, raised inside a
`dispatch_once`, with nothing to catch. So the bare binary refuses to start, and
every call that could reach the notification centre is guarded before it gets
there. One of those guards exists because a test posted from the bare test
binary and killed the suite.

The executable inside the bundle is a signed **copy**, not a symlink: `codesign`
refuses a bundle whose main executable is a symlink — *"the main executable or
Info.plist must be a regular file"* — and an unsigned bundle is invisible to the
notification system. Rebuilding does not refresh the bundle; re-run `install`.


## Ad-hoc is not signed enough

This is the part worth knowing before you debug it yourself. All measured on
macOS 26.6.2, 2026-09-18:

- **Ad-hoc signature**: `requestAuthorization` answers *"Notifications are not
  allowed for this application"*. No prompt appears, the status goes to `denied`
  without anybody being asked, and the app never appears in System Settings — so
  there is nowhere to go and turn it on. Location makes no difference;
  `lsregister` makes no difference.
- **A self-signed certificate**, untrusted, in the login keychain: works. macOS
  does not require the certificate to be trusted or to come from Apple, only
  that the signature carries an identity rather than being ad-hoc.
- **`UNAuthorizationOptionProvisional` is required as well.** Asking for Alert
  alone is refused even when signed. With Provisional it is granted immediately
  and silently, notifications arrive quietly in Notification Centre, and macOS
  then asks *"Keep receiving notifications from the agent-notify app?"* — which
  is where you promote them to banners.
- **A decision macOS makes about a bundle identifier cannot be unmade.** An
  identifier that was ever used with an ad-hoc signature keeps its `denied`
  after being signed properly, is absent from System Settings, and has no reset.
  Pick the identifier before the first run and sign from the start.

Making a self-signed identity, once:

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

The `-keypbe/-certpbe/-macalg` flags are not decoration: OpenSSL 3 writes a
PKCS#12 whose MAC macOS refuses with *"MAC verification failed"*.

Then:

```
agent-notify-macos-notifications install --sign "agent-notify self-signed"
```

which records it in the config, so re-running `install` after a rebuild keeps
signing the same way. Without `--sign` the bundle is ad-hoc and **nothing is
delivered at all**. `agent-notify-macos-notifications check` reports which you
have, and posts a test banner.


## Configuring

Everything optional, in this integration's own table. A key nobody declared is
refused **by name**, because a misspelled setting that changes nothing and says
nothing is the config bug people give up on.

```toml
[integration.macos-notifications.settings]
sign = "agent-notify self-signed"   # written by `install --sign`
sound = "Submarine" # true (the usual chime), false (silence), or a name

[integration.macos-notifications.settings.preview]
lines = 8           # lines of the agent's last message a banner carries
width = 60          # characters per line

[integration.macos-notifications.settings.colors]
blocked-on-you = "0xffeb6f92"   # 0xAARRGGBB only — see below
```

The defaults are the menu bar's, and the point of them is that the two agree:
`0xffeb6f92` (love), `0xfff6c177` (gold), `0xff9ccfd8` (foam). Change them here
and on the bar together, or the two displays start telling you different
stories. **Hex only**: AppKit's colour names are no use, because the mark is
drawn into a PNG that macOS composites onto a surface of its own, where a colour
that adapts to the appearance has nothing to adapt to. A name that cannot be
drawn is refused when the config is read.

`sound` takes three things and they are one key rather than two, because a
`sound = false` and a `sound-name = "Submarine"` that disagreed would have to
have a winner, and nobody would guess which:

- `true`, or no line at all — the usual notification chime.
- `false` — no sound is attached at all, which is silence whatever else is set:
  there is nothing for macOS to play.
- a name — `"Submarine"`, `"Glass"`, anything in `/System/Library/Sounds`, and
  anything you put in `~/Library/Sounds`. Written however you like
  (`submarine`, `Submarine`, `Submarine.aiff` are the same file); macOS is
  handed the file's real name, which is what its lookup matches on.

**A name that is not on this machine is refused when the config is read**, and
the reason is worth knowing before you try to debug it yourself.
`[UNNotificationSound soundNamed:]` accepts any string whatever, returns an
object, and reports nothing — the object is byte-identical for a real name and
a nonsense one [measured 2026-09-19]. Resolution happens later, inside the
notification daemon, and a name it cannot find becomes **the default chime**.
So a misspelling that got through would be the wrong noise forever with nothing
anywhere to read about it, which is the same failure the colours are refused
for.

What that check cannot tell you is whether the daemon will take a name this
program *did* find on disk. Only your ears can, so:

```
agent-notify-macos-notifications check --sound Submarine
```

posts a test banner with it. `check` reads no config at all — the thing you run
when something is broken must not be the next thing that breaks — so this flag
is how you audition a sound before writing it down.

Sound is here, and the rest of how a banner is presented is not, because it is
the one of those a person decides per MACHINE rather than once: the same config
wants one noise on the desktop and none on the laptop you take into meetings,
and macOS's own switch is per-app with nothing between chime and silence.
System Settings → Notifications still has the last word; sound turned off there
is silence whatever this file says.

There is deliberately nothing else. What a banner looks like, how long it stays
and whether it interrupts a full-screen app are all macOS's business, and this
is not the place to reimplement System Settings.

## How it is built

Everything that decides whether there is anything to say is in Go —
`notify.go`, which is the whole of the edge-making, and `preview.go`, which is
the whole of what a banner says. `notifier.m` posts what it is handed and
answers questions about what macOS has decided. It draws the app icon as well,
because that icon is the picture on every banner: a rounded plum tile with a
space invader on it, eleven cells across, in Rosé Pine's Iris.

The icon is drawn at install time rather than checked in — a binary in a
repository is a thing nobody can review in a diff — and written in sRGB rather
than in the calibrated space AppKit draws in, because a PNG straight out of
AppKit holds `#b693e1` where `#c4a7e7` was asked for.

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

## Posting is not the same as being talked to

The program has to **be an application**, and for a day it was not. Nothing
called `[NSApplication sharedApplication]`, so `NSApp` was nil, `[NSApp run]`
was a message to nil that returned at once, and the main queue was never
drained.

Everything still worked. Permission was granted, banners arrived, `check` was
green — because posting a notification is XPC and does not need a run loop.
Only the way BACK was dead: every callback macOS has arrives on the main queue,
so clicking a banner was answered by macOS with *"the application is not
responding properly"*, while LaunchServices quietly launched a second copy of
the app to look for somebody at home.

So `check` now asks the question outright, and the program asks it about itself
a few seconds after starting:

```
answers          yes — a tap on a banner reaches this program
```

It is asked from a goroutine while the main thread runs the loop, because that
is the arrangement being tested: asking from the thread that is supposed to be
answering would say "no" however healthy it was.

## Tests

`go test ./...` runs against the real macOS on this machine. There is no fake
worth writing: everything this repository believes is a fact about
`UNUserNotificationCenter`, and a stub would only agree with whatever it already
assumed. The test binary is not in a bundle, which is the interesting half —
every guard in `notifier.m` is exercised by the case that would otherwise kill
the process.
