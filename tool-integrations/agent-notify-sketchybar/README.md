# agent-notify-sketchybar

sketchybar's **display** integration for [agent-notify](../../agent-notify): the
semaphore on your menu bar — one counter per state, a chip behind each one
listing the sessions in it, and a click that takes you to the session.

```
  A blocked-on-you  1    A agent-notify  permission-pr…  now
  O finished-a-turn 1    O lenny                         now
  ~ working         2    ~ agent-notify   2m | ~ nushell  1s
```

(Shown as ASCII; the glyphs that ship are Nerd Font marks and the colours are
what actually tell the states apart.)

## Installing

```
agent-notify install sketchybar            # prints the table; writes nothing
agent-notify install sketchybar >> ~/.config/agent-notify/config.toml
```

```toml
[integration.sketchybar]
binary = "/opt/homebrew/bin/agent-notify-sketchybar"
```

That is all. **Nothing goes in your sketchybarrc**: the session-watcher starts
this, it puts its own items on the bar when it connects, and it takes them off
again when it stops. A counter left behind would say "two agents are working"
for ever, and there is no way to tell a stale number from a current one.

## Configuring

Everything optional, all of it in this integration's own table. A key nobody
declared is refused **by name**, because a misspelled colour that changes
nothing and says nothing is the config bug people give up on.

```toml
[integration.sketchybar.settings]
sketchybar = "/opt/homebrew/bin/sketchybar" # required, absolute; install resolves it
position = "left"                            # left, right, center, e, q
rows     = 8                                 # sessions per chip before "and n more"
announce = 8000000000                        # how long a state change stays lit:
                                             # nanoseconds, because that is what a
                                             # Go time.Duration is. 0 = never.
before   = "sep_agents"                      # an item of yours to sit next to

[integration.sketchybar.settings.preview]
lines = 6                                    # lines of the last message on screen
depth = 40                                   # lines written — how far a scroll goes
width = 64                                   # characters per line

[integration.sketchybar.settings.colors]
working                            = "0xff31748f"
"blocked-on-you/permission-prompt" = "0xffeb6f92"   # kernel, or kernel/detail

[integration.sketchybar.settings.glyphs]
idle = ""
```

## When something happens

A bar is a thing in the corner of an eye, and a counter going from 1 to 2 is not
enough to notice. So a state that has just arrived says so:

- its **counter lights up**, in that state's own colour;
- its **chip opens by itself**, on the agent's own words;
- the **row inside it lights up** — the counter says which state changed, the
  row says which agent.

Eight seconds later all of it goes away again. `working` never announces itself
— an agent getting on with it is the state the bar is in most of the day, and a
bar lit up all day means nothing — and neither does idle; anything more urgent
than working does. `announce = 0` turns the whole thing off and leaves you
with counters that only count.

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
  be quoted. Writing only the visible lines is what makes a scroll reveal blanks.
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
will ever tell this display that four minutes have become five — because
nothing changed. Elapsed time is derivable by every subscriber from the record
it already holds, so it is rendering and never state (R26): storing it would
mean a write per second per session, and every one of those writes would wake
every display to say nothing at all. So the age is computed here, and here is
where the clock lives.

## Five things about sketchybar worth knowing

All measured on v2.24.0, all load-bearing:

- **Everything is forgiving, which is what makes a render idempotent.** `--add`
  on an item that exists is informational (`[?]`); `--set` on one that does not
  is a complaint (`[!]`) and the rest of the batch still lands. So this names
  every item it owns and sets everything about it, every time, with no
  bookkeeping — and when the bar says it has lost something, the next paint
  declares the whole structure again and repairs itself. The structure and the
  values are sent separately only because a preview forty lines deep behind five
  counters is two hundred items, and re-declaring those several times a second
  is a thousand arguments an agent event.
- **A backslash in any property makes that item's `--query` answer invalid
  JSON.** sketchybar does not escape it, so anything that reads the item back
  fails to parse. It cost a test that could not read an item it had just
  written, and it is why the scripts here name their items one at a time rather
  than with the regex sketchybar would accept in place of a name.
- **The exit code reports only the LAST message in the batch.** `--set missing
  --set real` exits 0; `--set real --set missing` exits 1. It says nothing about
  the hundred messages in between, so it cannot be a verdict on a render. The
  complaints are the truth, and their severity is sketchybar's own: `[?]` is
  informational ("already exists", which arrives on every render after the
  first) and `[!]` is a problem.
- **A non-zero exit with nothing marked `[!]` is a wholesale refusal**, and that
  distinction is not pedantry. `sketchybar` needs `USER` set to find its
  per-user service and exits with "'env USER' not set! abort" when it is
  missing — no `[!]`, nothing per-message, and a display that only read `[!]`
  lines would paint nothing in complete silence for ever. That is exactly what
  happened while this was being built (plan.md D-43).
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

```
env -u GOROOT go test ./...
```

What the bar should say is a table of sessions in and a command line out, so
most of it needs no sketchybar. The rest runs against the real one under its own
item prefix and cleans up afterwards, skipping itself where sketchybar is not
running.

## Building

Go 1.26. Core is not published yet, so `go.mod` has a `replace` pointing beside
this repository; that line comes out when core is.
