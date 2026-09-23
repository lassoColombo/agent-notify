# agent-notify-zellij-display

zellij's **display** integration for [agent-notify](../../agent-notify): it
paints agent state onto pane and tab titles, and does nothing else.

Its sibling, [agent-notify-zellij-container](../agent-notify-zellij-container),
is the half that can take your cursor somewhere. They are two programs on
purpose: a display writes titles, a container *moves your focus*, and wanting
the first is not the same decision as allowing the second. Install either, both,
or neither — each reads the environment it needs for itself.

```
  pane 4   ↻ agent-notify        working
  pane 5   ▢ lenny               finished a turn, your move
  tab      ▲ notes               the most urgent thing inside it
```

(The glyphs that ship are Nerd Font marks; the ASCII above is for a README.)

It is a long-lived process that connects to the session-watcher once, declares
itself a `display`, and is handed the current state whenever something moves. It
never talks to an agent, and nothing an agent does waits on it.

## Installing

```
agent-notify install zellij-display    # prints the table; writes nothing
agent-notify install zellij-display >> ~/.config/agent-notify/config.toml
```

That prints one table for agent-notify's config file. Putting it there is yours
to do, because the table being there is what turns this on (D-66) — your
comments and your ordering survive, and a file it cannot parse is refused rather
than added to:

```toml
[integration.zellij-display]
binary              = "agent-notify-zellij-display"
capture-environment = true
```

`capture-environment` is the load-bearing line. Only a process running inside
the agent can see which pane it is in, so the hook runs this program there and
stores what it returns under this integration's name. Without it every record
arrives with nowhere to paint and the display is correct and invisible.

**Which** variables it reads is not in your config file and is not yours to set:
they live in this program's source, where the code that reads them and the code
that reads them back both already are. The line used to be
`capture = ["ZELLIJ_SESSION_NAME", "ZELLIJ_PANE_ID"]`, and a typo in it broke
the display without a word of complaint anywhere.

If you have that older line, delete it and run the install again.

Then run it:

```
agent-notify-zellij-display            # stays connected and paints
agent-notify-zellij-display repaint    # paint once from the store, no session-watcher
```

Until M13 nothing starts it for you. Start it from your shell rc, a launchd
job, or by hand.

## Configuring

Everything optional, and all of it in this integration's own table. A key
nobody declared is refused **by name**, because a misspelled glyph that changes
nothing and says nothing is the config bug people give up on.

```toml
[integration.zellij-display.settings]
zellij = "/opt/homebrew/bin/zellij"    # required, absolute; install resolves it

[integration.zellij-display.settings.glyphs]
working                            = "↻"
"working/compacting"               = "~"    # keyed on kernel, or kernel/detail
"blocked-on-you/permission-prompt" = "?"
idle                               = ""     # empty is a choice, not a miss
```

## What it does, exactly

- **A pane whose session is live** says `<glyph> <name>`. The name is whatever
  the agent calls the session, reported by its agent-integration, falling back
  to the last component of its working directory — which is core's rule and not
  this program's.
- **A tab** says `<glyph> <its own name>`, where the glyph is the most urgent
  agent inside it. That is the same `max(rank)` an LED over the whole machine
  computes, from the same function.
- **A pane whose session ended** is handed back to zellij's own title. This
  display asks to be told about ended sessions it will never draw for exactly
  that reason: it owns a piece of a UI it did not create, and the record is the
  only thing that remembers which piece.
- **A tab's name is the user's**, so it is recovered from what the tab says now
  minus any glyph of ours, never remembered. A tab you rename by hand keeps its
  new name, and restarting this program does not stack a second glyph on it.
- **Anything it did not paint, it does not touch.** Panes with no agent in
  them, tabs with no agent in them, and other zellij sessions are left alone.

## Two facts about zellij worth knowing

Both measured on zellij 0.45.1, both load-bearing:

- **The exit code cannot answer "is that session there".** Asking for a session
  that does not exist exits **0** when some other detached session happens to be
  alive — writing the list of sessions that *do* exist where the JSON should be
  — and exits **1** when none is. A status that depends on unrelated state is
  not a signal, so the parse decides. (A missing *pane* inside a live session
  does exit 2, reliably; that is a different question.) Every render therefore
  reads before it writes, which is also what guarantees a vanished zellij
  session gets no rename attempted against it at all.
- **`rename-pane --pane-id` is what makes this possible.** Without it a display
  can only rename the focused pane, which is useless: the pane that needs a
  glyph is by definition the one you are not looking at.

## Testing

```
env -u GOROOT go test ./...
```

The pure half — what should be renamed, and to what — is a table of records and
a table of panes, so most of it needs no zellij at all. The rest runs against a
real one: a detached session, real panes, real renames, cleaned up afterwards,
skipped where there is no zellij to talk to. `TestPanesCarryLiveStateGlyphs`
is the whole chain with only the agent replaced by a script.

## Building

Go 1.26. Core is not published yet, so `go.mod` has a `replace` pointing beside
this repository; that line comes out when core is.

If your shell exported `GOROOT` from an outer context it overrides the toolchain
pinned in `.tool-versions` — `env -u GOROOT go build ./...` is the fix.
