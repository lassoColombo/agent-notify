# agent-notify-tmux

tmux's integration for [agent-notify](../../agent-notify). It marks what every
agent on your machine is doing on tmux's panes and windows, works out which
pane a session lives in, brings that pane to the front when somebody asks, and
says whether you are already looking at it.

```
  pane %3   ↻ monomodules      working
  pane %5   ▢ lenny            finished a turn, your move
  pane %7     notes-8e         idle — a quiet agent shows its name and nothing else
  window 1  ▲                  the most urgent thing inside it

$ agent-notify focus-session lenny
  aerospace-container    skipped — aerospace-container has no coordinates for this session
  tmux                   focused
lenny is in front.
```

(The glyphs that ship are Nerd Font marks; the ASCII above is for a README.)

It is the tmux half of plan.md's third promise — switch tool and nothing else
changes — and it is built the way `agent-notify-zellij` is: one program for
both halves (D-83), because they read the same two environment variables and
drive the same tool. Titles without focus is a configuration, not a separate
install — leave `tmux` out of `containers` and nothing will ever ask it to move
you.

Core runs it. There is no daemon: the hook runs `capture-environment` inside
the agent, the session-watcher runs `render` when something a mark shows moves
and `interpret-environment` once per capture, and `agent-notify focus-session`
runs `focus`. Each is one JSON object on stdin, one on stdout.

## Installation

- **tmux 3.2 or later** (pane options, `set-option -p`; measured on 3.8).
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

## Drawing the marks

**This program writes two user options and nothing else**, so what it says
shows only where your tmux configuration asks for it:

| Option | Set on | Holds |
| --- | --- | --- |
| `@agent-notify-pane` | a pane holding a live agent | `<glyph> <name>`, or just the name for a state whose glyph is empty |
| `@agent-notify-window` | a window holding one | the glyph of the most urgent agent in it |

Both are unset again when there is nothing to say. For example:

```tmux
# the window list: the glyph in front of the window's own name
set -g window-status-format         '#{?#{@agent-notify-window},#{@agent-notify-window} ,}#I #W'
set -g window-status-current-format '#{?#{@agent-notify-window},#{@agent-notify-window} ,}#I #W'

# pane borders: the agent's mark, or the pane's own title
set -g pane-border-status top
set -g pane-border-format ' #{?#{@agent-notify-pane},#{@agent-notify-pane},#{pane_title}} '
```

Why options rather than renames, which is what zellij's integration does:
a pane title is the program's to rewrite, and an agent rewrites its terminal
title constantly, so a title painted on a render would be gone by the next
one; and `rename-window` turns the window's `automatic-rename` off for good,
which nothing could give back. Nobody else writes these options, so there is
nothing to strip off a name and nothing to recover — giving a pane back is
`set-option -u`.

## Configuration

Everything of this program's lives under `integrations.tmux` in
`~/.config/agent-notify/config.yaml` (plan.md D-103).

| Key | Default | What it does |
| --- | --- | --- |
| `integrations.tmux.enabled` | `true` | `false` leaves the binary on PATH and stops core running it (D-102) |
| `integrations.tmux.settings.tmux` | `tmux` on PATH | where tmux is, when this process's PATH does not say (D-101) |
| `integrations.tmux.settings.glyphs` | zellij's | what each state looks like, keyed on `kernel` or `kernel/detail` |

A settings key nobody declared is refused by name. No timeout is configurable:
one tmux invocation is bounded at 2s, and on expiry a render skips that server
and a focus answers `not-running`.

## Where a session lives

`$TMUX` is `<socket>,<server pid>,<session index>` and `$TMUX_PANE` is `%<n>`;
both are captured as they are. The socket names the server, and every tmux
invocation here passes it as `-S`, because the session-watcher's own `$TMUX`
— if it has one — is whatever pane happened to start it. The pane id is unique
per server for the life of the pane, which makes it the coordinate. The session
index is NOT used to place anything: it is where the pane was started, and
`move-pane` makes it stale, so the session and window are always asked of tmux.

A process of tmux that is not in a pane — a popup, a `run-shell` — has `$TMUX`
and no `$TMUX_PANE`, and is nowhere.

## Focus

`focus` re-reads where the pane is, asks whether you are already there, acts,
and reads the state back, because an exit code says nothing about whether
anybody's screen changed. In order:

- **Asked from inside the same server** (the picker in a pane or a popup):
  the terminal that asked is the screen you are looking at, and
  `switch-client -c <it> -t <pane>` takes it there, session, window and pane
  in one command. This holds even when another terminal already shows the
  pane's session.
- **Somebody is attached to a session holding the pane**: that session's
  window is selected, then the pane, in the terminal that shows it.
- **Somebody is attached to the server at all**: the most recently active
  client is switched to the pane. This is how a notification clicked outside
  tmux still lands somewhere (D-123).
- **Nobody is**: `not-attached`, naming the `tmux attach` that would help.

Every tmux invocation passes `-u` as well as `-S`. The session-watcher runs
with no `LANG`, and a tmux that doesn't believe its output is UTF-8 prints
control characters and every non-ASCII character as `_`. That includes the
field separator, so nothing it answered would parse.

"In front" means: some session holding the pane has a client, has the pane's
window as its current one, and has the pane as that window's active one. What
no format says is whether a popup is open over it.

Failures are words core can act on: `place-is-gone`, `not-running`,
`never-placed`, `refused`, and this program's own `not-attached`.

## Testing

`env -u GOROOT go test ./...`. The planning and the focus rule are tables, the
focus paths also run against a stand-in tmux that records what it was asked,
and the rest runs against real tmux servers on private sockets with no
configuration file — a client for them is a pane of a second server running
`tmux attach` — and skips without tmux.
