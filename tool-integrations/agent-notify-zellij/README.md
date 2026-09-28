# agent-notify-zellij

zellij's integration for [agent-notify](../../agent-notify). It paints what
every agent on your machine is doing onto zellij's pane and tab titles, works
out which pane a session lives in, brings that pane to the front when somebody
asks, and says whether you are already looking at it.

```
  pane 3   ↻ monomodules      working
  pane 5   ▢ lenny            finished a turn, your move
  pane 7     notes-8e         idle — a quiet agent shows its name and nothing else
  tab  1   ▲ work             the most urgent thing inside it

$ agent-notify focus-session lenny
  aerospace-container    focused
  zellij                 focused
lenny is in front.
```

(The glyphs that ship are Nerd Font marks; the ASCII above is for a README.)

It is one program for both halves (plan.md D-83): they read the same two
environment variables and drive the same tool. Titles without focus is a
configuration, not a separate install — leave `zellij` out of
`[container] order` and nothing will ever ask it to move you.

Core runs it. There is no daemon: the hook runs `capture-environment` inside
the agent, the session-watcher runs `render` when something a title shows
moves and `interpret-environment` once per capture, and `agent-notify
focus-session` runs `focus`. Each is one JSON object on stdin, one on stdout.

## Installation

- **zellij** whose `rename-pane` takes `--pane-id` (measured on 0.45.1).
- **agent-notify core**, built and on your PATH.
- **Go 1.26** to build. A **Nerd Font** if you want the default glyphs.

```sh
cd tool-integrations/agent-notify-zellij
env -u GOROOT go build ./...
cp agent-notify-zellij /opt/homebrew/bin/      # anywhere absolute
agent-notify install zellij
```

`install` prints the tables and writes nothing; put them in
`~/.config/agent-notify/config.toml` yourself:

```toml
[integration.zellij]
binary = "/opt/homebrew/bin/agent-notify-zellij"

[integration.zellij.settings]
zellij = "/opt/homebrew/bin/zellij"

[container]
order = ["aerospace-container", "zellij"]
```

`binary` and `zellij` are absolute because the programs that run this do not
have your shell's PATH. `zellij` is resolved at install time, in your shell,
for the same reason. If you already have a `[container]` table, add `"zellij"`
to its `order` rather than pasting a second one; outermost first.

Then `agent-notify watcher reload` so the session-watcher asks this program
what it answers, and `agent-notify doctor` to see it listed:

```
integrations ok    reported by pid 41022 at 2026-09-28T08:14:02Z
             zellij                 drawn when something it watches moves
                                    answers interpret-environment, focus, focused, render — built against 0.0.0-dev
```

A rebuilt binary is picked up on the next render; a rebuild that changes the
wake list needs a reload.

## Configuration

| Key | Default | What it does |
| --- | --- | --- |
| `binary` | required | this program, absolute |
| `enabled` | `true` | `false` keeps the table and stops core running it |
| `settings.zellij` | required | absolute path to zellij |
| `settings.glyphs` | see below | what each state looks like, keyed on `kernel` or `kernel/detail` |

A settings key nobody declared is refused by name.

```toml
[integration.zellij.settings.glyphs]
working                            = "↻"
"working/compacting"               = "~"
"blocked-on-you/permission-prompt" = "?"
idle                               = ""
```

Most specific first: the `kernel/detail` pair, then the `kernel`, then the
nearest known rank for a state this build has never met. The defaults are Font
Awesome marks in Nerd Fonts, told apart by shape because a title has no colour:
`blocked-on-you` U+F071, `broke` U+F00D, `finished-a-turn` U+F075, `working`
U+F021, `idle` and `ended` empty. They live in the Private Use Area and a lot
of tooling drops them silently; check the result after pasting your own.

No timeout is configurable: one `zellij action` is bounded at 2s, and on
expiry a render skips that zellij session and a focus answers `not-running`.

## What a title says

A pane holding a live agent says `<glyph> <name>`, the name being what the
agent calls the session or core's fallback (`Record.DisplayName`), the same
string `agent-notify list` prints. A tab says `<glyph> <its own name>` with the
glyph of the most urgent agent in it; the name is recovered from what the tab
says now, minus any glyph this configuration would have written, so a tab you
renamed keeps its name. A pane whose session ended is handed back to zellij's
own title.

Every render reads the titles before writing and issues only the renames that
change something, so it is correct after a restart and after a pane moves.

## Focus

`focus` re-reads where the pane is rather than trusting the stored tab, asks
whether you are already there (zellij refuses to focus an already-focused pane,
exit 2), and reads the state back afterwards, because zellij's exit code says
nothing about whether anything moved. Three facts measured on 0.45.1 shape it:

- The focus actions against a session nobody is attached to exit 0 and move
  the stored focus with nothing on any screen. `list-clients` is the only
  place a viewer is visible, so with nobody attached the answer is
  `not-attached`: `zellij attach <session>` first.
- When nobody is attached and the focus was asked from a terminal in another
  zellij session — the picker under its keybinding — that terminal is the
  missing viewer, and `switch-session` moves it there.
- A tab has two focused panes, tiled and floating; which is in front depends
  on whether the floating layer is shown. `focus-pane-id` on the tiled one is
  what lowers the layer.

Failures are words core can act on: `place-is-gone`, `not-running`,
`never-placed`, `refused`, and this program's own `not-attached`.

## Testing

`env -u GOROOT go test ./...`. The planning and the focus rule are tables; the
rest runs against a real zellij in detached sessions named `an-test-*` and
`an-ctr-*`, and skips without one.
