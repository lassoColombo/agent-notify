# agent-notify-tmux

tmux's integration for [agent-notify](../../agent-notify). It writes what every
agent on your machine is doing on the tmux pane it runs in, works out which
pane a session lives in, brings that pane in front of a terminal when somebody
asks, and says whether you are looking at it.

```
  pane %3     ↻ monomodules     working
  pane %5     ▢ lenny           finished a turn, your move
  pane %7       notes-8e        idle — a quiet agent shows its name and nothing else
  window 1    ▲ ↻ api           every agent in it, looped out of its panes by tmux

$ agent-notify focus-session lenny
  aerospace-container    skipped — aerospace-container has no coordinates for this session
  tmux                   focused
lenny is in front.
```

(The glyphs that ship are Nerd Font marks; the ASCII above is for a README.)

It is the tmux half of plan.md's third promise — switch tool and nothing else
changes — and one program for both halves (D-83), because they read the same
two environment variables and drive the same tool. Marks without focus is a
configuration, not a separate install: leave `tmux` out of `containers` and
nothing will ever ask it to move you.

Core runs it. There is no daemon: the hook runs `capture-environment` inside
the agent, the session-watcher runs `render` when something a mark shows moves
and `interpret-environment` once per capture, and `agent-notify focus-session`
runs `focus`. Each is one JSON object on stdin, one on stdout.

## Installation

- **tmux 3.2 or later** (pane options, `#{P:}` loops; measured on 3.8).
- **agent-notify core**, built and on your PATH.
- **Go 1.26** to build. A **Nerd Font** if you want the default glyphs.

```sh
make install                    # at the root: every module, into $(go env GOPATH)/bin
```

Being on PATH is the whole of installing it (plan.md D-102). tmux itself is
found the same way, at runtime; `integrations.tmux.settings.tmux` is the
override for a process that was not started from your shell.

If this is your only container it is the whole of `containers`, and you need
not write one. Otherwise write the list, outermost first, then
`agent-notify watcher reload` and `agent-notify doctor`.

## The marks

**This program writes three user options, on panes only**, and only on a pane
that holds a live agent:

| Option | Holds |
| --- | --- |
| `@agent-notify-state` | the agent's state: `blocked-on-you`, `broke`, `finished-a-turn`, `working` or `idle` |
| `@agent-notify-glyph` | the glyph for that state, and unset when the glyph is empty, as idle's is |
| `@agent-notify-name` | the session's name |

They are unset again when the agent ends, or whenever a pane carries them and
no live agent claims it. Nothing shows until your tmux configuration names them
in a format. For example:

```tmux
# a pane's border: the agent's glyph and name, or the pane's own title
set -g @agent "#{?#{@agent-notify-glyph},#{@agent-notify-glyph} ,}#{q/h:@agent-notify-name}"
set -g pane-border-status top
set -g pane-border-format ' #{?#{@agent-notify-name},#{E:@agent},#{q/h:pane_title}} '

# the window list: the glyph of every agent in the window, before its name
set -g @agents "#{P:#{?#{@agent-notify-glyph},#{@agent-notify-glyph} ,}}"
set -g window-status-format         '#{E:@agents}#I #W'
set -g window-status-current-format '#{E:@agents}#I #W'

# a colour for an agent waiting on you
set -g @agent_style "#{?#{==:#{@agent-notify-state},blocked-on-you},#[fg=red],}"
```

A name is the agent's, and is stored as it is. Draw it as `#{q/h:…}`, as
above: tmux reads `#[…]` in what it draws as a style and `##` as one `#`, so
a session called `fix #[bold] ##3` would otherwise restyle the border and
lose a character. `q/h` doubles every `#`, and the name is drawn as itself.

**Nothing is written on a window, and that is the design** (D-126). What a
window shows is looped out of the panes it holds while tmux draws it, and a
pane's options go wherever the pane goes. So a pane moved with `break-pane`,
`join-pane` or `swap-pane` — or borrowed onto the stage of
`agent-notify-tmux-cockpit`'s tab — takes its glyph with it at once, and the
window it left shows nothing of it, with no render in between. A mark computed
for a window by this program would be right only until the next move, and
nothing would wake it to put it right.

**The cost**: a window with two agents shows both glyphs, in pane order, rather
than only the most urgent one. "The most urgent" is a maximum, and a tmux loop
can be sorted by index, name or time but not by an option.

Why options rather than renames: a
pane title is the program's to rewrite, and an agent rewrites its terminal
title constantly; and `rename-window` turns the window's `automatic-rename`
off for good. Nobody else writes these options, so giving a pane back is
`set-option -u`.

**How a render writes them.** For each tmux server it reads every pane's
options in one `list-panes`, plans the difference from what the records say,
and runs every `set-option` in one invocation. Each says `-q`: tmux stops an
invocation at the first command it refuses, and a write to a pane that closed
between the read and the write would be one, where with `-q` it is passed over
and the rest are written. A record whose capture names another server — the
same socket, but a server started again since, with its pane ids handed out
again from `%0` — marks nothing.

**A render that cannot paint fails, and is drawn again.** A server that is
not running is nothing to paint: an ended agent's record may name one for a
while. A server that is running and does not answer within 2s, or answers
something that is not what it was asked, fails the render, and core hands the
same view over again at its next sweep, five seconds later at most, until it
is drawn (D-130). Before that, one slow answer left a mark saying
`finished-a-turn` on an agent that needed you, until its state moved again.

## Configuration

Everything of this program's lives under `integrations.tmux` in
`~/.config/agent-notify/config.yaml` (plan.md D-103).

| Key | Default | What it does |
| --- | --- | --- |
| `integrations.tmux.enabled` | `true` | `false` leaves the binary on PATH and stops core running it (D-102) |
| `integrations.tmux.settings.tmux` | `tmux` on PATH | where tmux is, when this process's PATH does not say (D-101) |
| `integrations.tmux.settings.glyphs` | Nerd Font marks, one per state | what each state looks like, keyed on `kernel` or `kernel/detail` |

A settings key nobody declared is refused by name. No timeout is configurable:
one tmux invocation is bounded at 2s, and on expiry a render fails, to be
drawn again at core's next sweep, and a focus answers `not-running`.

## Where a session lives

`$TMUX` is `<socket>,<server pid>,<session index>` and `$TMUX_PANE` is
`%<n>`; both are captured as they are. The coordinates are the socket, the
server's pid and the pane id, read out of the capture without asking tmux
anything, so placing a session cannot fail because tmux was slow. Every tmux
invocation passes the socket as `-S`, because the session-watcher's own
`$TMUX` — if it has one — is whatever pane happened to start it. Which session
and window hold the pane is never stored: `move-pane` would make it wrong. It
is asked of tmux when it is needed, and so is whether the server is still the
one with that pid.

A process of tmux that is not in a pane — a popup, a `run-shell` — has `$TMUX`
and no `$TMUX_PANE`, and is nowhere.

## Focus

A focus is somebody wanting to see a pane, so it starts by deciding whose
screen that is:

- **Asked from inside the pane's server**, the terminal it was asked from: the
  one showing `$TMUX_PANE` when it was asked from a pane, or, from a popup like
  the picker's, the one on the session `$TMUX` names. Even when another
  terminal already shows the agent, because that one is not in front of the
  person asking.
- **Asked from anywhere else** (a banner, a script), the terminal that has the
  focus, and failing that the one used last.
- **No terminal at all**: `not-attached`, naming the `tmux attach` that would
  help.

All of that is what happens with no window manager to help. With one, and
another terminal already on the agent's session, that terminal is the one to
go to, as long as its window can be brought forward first (D-141):

- **`windows-showing`** answers, for a pane, the terminals on a session its
  window is in, each as the window it is drawn in: the client's name, the pid
  of its `tmux attach`, and the title tmux gives the window. tmux gives a title
  only with `set-titles on`, and the title is what tells two windows of one
  terminal application apart, so set it to something that names the session,
  such as `set -g set-titles-string "#S / #W"`. The terminal asked from comes
  first, if it is one of them; then the one used last.
- Core asks the window manager to bring one of those windows forward, and if
  one comes forward, it hands it to `focus`, which moves that terminal and not
  the one asked from. If none does, `focus` is not handed a window, and the
  terminal is chosen as above.

If that terminal already shows the pane, that is it. Otherwise
`switch-client -c <terminal> -t <session>:<window>.<pane>` brings the session,
the window and the pane in one command, unzooming a window zoomed on another
pane. The session is named, and it is the terminal's own when the pane's
window is linked into it, else the one a terminal was attached to last. Given
the pane alone, tmux picks the session that was active last, and when the
pane is on the agents tab's stage that is the tab's hidden session `_agents`
whenever the tab was just made: the terminal was left in it. Then the terminal
is read back, its session and its pane, because an exit code says nothing
about whether anybody's screen changed.

**Focused** is yes when a terminal shows the pane — its current window's
active pane — and that terminal has the focus. tmux knows the last from the
terminal's focus reports, which `focus-events on` turns on; without them it
never hears the focus leave, and answers as if the terminal had it. What no
format says is whether a popup is open over the pane.

Every tmux invocation passes `-u` as well as `-S`. The session-watcher runs
with no `LANG`, and a tmux that doesn't believe its output is UTF-8 prints
control characters and every non-ASCII character as `_`, including the field
separator, so nothing it answered would parse (D-123).

Failures are words core can act on: `place-is-gone` (the pane, or the server
it was in, is gone), `not-running`, `never-placed`, `refused`, and this
program's own `not-attached`.

## Testing

`env -u GOROOT go test ./...`. The planning, the choice of terminal and the
focused rule are tables. The rest runs against real tmux servers on private
sockets with no configuration file, and skips without tmux. A terminal there
is a pane of a second server running `tmux attach`, and its focus reports are
the escape sequences a terminal sends, typed into that pane.
