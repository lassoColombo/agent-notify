# agent-notify-tmux-manager

A tab of tmux given over to your agents. It has two panes:

- **Left, the list:** every agent running in a pane of this tmux server,
  grouped by what it wants from you (needs you, broke, finished, working,
  idle). Each entry shows its name and age, and below that the
  `session:window` and directory it lives in.
- **Right, the stage:** the selected agent's own pane. Not a copy of it: the
  pane itself, borrowed from the window it lives in for as long as the tab is
  on screen.

```
── agents ──────────────┬── ▲ alpha ──────────────────────────────────
 ▲ needs you 1          │ Do you want to proceed?
▌ alpha             2s  │ ❯ 1. Yes
▌ home:1  agent-notify  │   2. No
                        │
 ↻ working 1            │
  beta             31s  │
  home:2  monomodules   │

 ⏎ type   o go to   q close
```

Because the stage holds the real pane, everything in it is tmux's own: what
you type reaches the agent as it would anywhere, Shift+Enter included; copy
mode, the wheel and the mouse work; and the agent draws itself for the size
it is given rather than being re-wrapped. `agent-notify focused` is right
about it too, since the pane in front of you is the agent's.

## Keys

| Where | Key | Does |
| --- | --- | --- |
| list | `j` `k`, arrows, `Ctrl j/k/n/p`, `Alt j/k` | move the cursor; the stage follows once it rests |
| list | `g` `G` | first, last |
| list | `Enter` | onto the stage, like `Alt Tab` |
| list | `o` | go to the window the agent lives in, leaving the tab |
| list | `q` | close the tab |
| stage | anything | it is the agent's pane |
| either | `Alt Tab` | switch between the list and the stage |
| anywhere | `Alt i` | show the tab; on the tab, go back to the window before |

`Alt i`, `Alt Tab` and `Alt j/k` come from your tmux configuration, not from
this program (see below).

## Borrowing a pane

Selecting an agent swaps its pane with the **stand-in**, a pane of this
program's (`swap-pane -d -Z`). The stand-in takes the agent's cell in the
window it lives in, so that window keeps its layout, its zoom and its active
pane; the agent takes the stand-in's cell on the stage. Giving it back is the
same swap again. Moving the cursor to another agent sends the first one home
and brings the next in a single tmux invocation.

Three rules hold it together:

- **An agent is away from home only while the tab is on screen.** Leave the
  tab, by any key, and within a second everything is where it lives. The
  window an agent lives in looks untouched whenever you are in it.
- **The cursor has to rest.** A pane is swapped in about a tenth of a second
  after the cursor stops on it, so going down a list of six does not resize
  six agents.
- **Nothing arrives under your hands.** While the focus is on the stage the
  cursor does not move by itself. If the agent you are typing into ends, its
  pane stays on the stage until you go back to the list.

A second terminal looking at an agent's window while the first has it on the
stage sees the stand-in there, saying where the agent went. Enter in the
stand-in brings the agent back to its own place; the tab then leaves it there
until you select it again.

### What it costs

An agent is resized when it comes onto the stage and again when it goes home,
so it redraws twice per visit. And the stage is a pane of the tab's window:
`kill-window` on the tab kills the agent it is showing. Close the tab with `q`,
or with `agent-notify-tmux-manager close`, which is what the binding below
does.

## When something goes wrong

What the tab knows is written in tmux and nowhere else, as options on the
panes. The list never acts on a memory of what it did: about once a second,
and whenever something prompts it, it reads the server and does whatever is
left to do. So a tab found in any state comes right, and no failure takes an
agent with it:

| What happens | What follows |
| --- | --- |
| The list dies, however it dies | The stand-in sees its link to the list close, gives the agent back and closes itself. |
| The stand-in's program dies | Its pane stays (`remain-on-exit`), with the place it holds, and is started again there. |
| The window the agent lives in is killed | The agent, on the stage, survives it. It gets a window of its own in the session it came from. |
| The agent's pane closes on the stage | The stand-in comes back to the tab from the place it was holding. |
| The session the tab was opened in is killed | The tab lives in a hidden session of its own, `_agents`, and is only linked into yours, so it is unlinked and nothing else. |
| Anything else | `agent-notify-tmux-manager restore` sends every borrowed pane home, with or without a tab. |

The options, if you want to look:

| Option | On | Holds |
| --- | --- | --- |
| `@agent-notify-tab` | the tab's window | `1` |
| `@agent-notify-tab-role` | the tab's two panes | `list` or `stand-in` |
| `@agent-notify-stand-in` | a borrowed pane | the pane holding its place |
| `@agent-notify-home-session` | a borrowed pane | the session it came from |

`agent-notify-tmux`'s marks are options of the agent's pane, so they come onto
the stage with it: while an agent is borrowed, its glyph is on the tab's window
in the status line, and back on its own window once it goes home.

## Commands

```
agent-notify-tmux-manager open [client]   show the tab, making it if there is none; on the tab, go back
agent-notify-tmux-manager close           send every agent home and close the tab
agent-notify-tmux-manager restore         send every agent home
```

`list` and `stand-in` are what the tab runs in its own two panes. There is one
tab per tmux server. It lists the agents of that server only: an agent in
another server, in zellij or in a plain terminal has no pane to borrow, and
the picker already lists everything.

## The tmux side

```tmux
bind -n M-i run-shell "agent-notify-tmux-manager open '#{client_name}'"
bind -n M-Tab if -F "#{@agent-notify-tab}" { select-pane -t :.+ } { send-keys M-Tab }
bind -n M-j if -F "#{==:#{@agent-notify-tab-role},list}" { send-keys M-j } { select-pane -D }
bind -n M-k if -F "#{==:#{@agent-notify-tab-role},list}" { send-keys M-k } { select-pane -U }
```

Outside the tab, `Alt Tab` reaches the program in the pane as usual and
`Alt j/k` move between panes. And wherever you bind `kill-window`, let the tab
close itself instead:

```tmux
bind X if -F "#{@agent-notify-tab}" { run-shell "agent-notify-tmux-manager close" } { confirm-before kill-window }
```

`focus-events` should be `on`. The tab is told the moment you arrive at it or
at a stand-in, through the focus reports tmux sends its panes; without them it
finds out at its next look, up to a second later.

## Configuration

Under `integrations.tmux-manager` in `~/.config/agent-notify/config.yaml`
(plan.md D-103).

| Key | Default | What it does |
| --- | --- | --- |
| `settings.tmux` | `tmux` on PATH | where tmux is, when this process's PATH does not say (D-101) |
| `settings.glyphs` | agent-notify-tmux's | what each state's header wears, keyed on `kernel` |
| `settings.list_width` | `36` | the list's width in columns when the tab is made; resize it as any pane after that |

No timeout is configurable: one tmux invocation is bounded at 2s, and one that
expires is left for the next look.

## Testing

`env -u GOROOT go test ./...`. The planning, the list and the cursor's rules
are tables. Everything about tmux itself — that a swap keeps both windows'
shapes, that an agent outlives the window it was borrowed from — runs against
real tmux servers on private sockets with no configuration file, with a pane
of a second server as the attached terminal, and skips without tmux.

## Building

`make install` at the root, like every module. It is run from a key and never
by core: `agent-notify doctor` lists it as "yours to start".
