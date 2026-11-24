# agent-notify-tmux-cockpit

A tab of tmux given over to your agents. It has two panes:

- **Left, the list:** every agent running in a pane of this tmux server,
  grouped by what it wants from you (needs you, broke, finished, working,
  idle), one line each: its name, and how long it has been in that state.
  The wait is bold for an agent waiting on you, and turns gold for one that
  has been working for more than fifteen minutes, which is more often stuck
  than busy. Under them, in a group of their own, **in background**: the
  sessions running outside every tmux server, each with its state's glyph,
  its name dimmed, and its wait. These are shown, not selected, because there
  is no pane to borrow (see below).
- **Right, the stage:** the selected agent's own pane. Not a copy of it: the
  pane itself, borrowed from the window it lives in for as long as the tab is
  on screen.

```
── agents ──────────────┬── ▲ alpha ──────────────────────────────────
 ▲ needs you            │ Do you want to proceed?
▌ alpha             2s  │ ❯ 1. Yes
                        │   2. No
 ↻ working              │
  beta             31s  │
  gamma            24m  │

 ⏎ type   o go to   q close
```

That is all the list says, on purpose (D-132). Where an agent lives is what
`o` is for, what it said is on the stage, and its model, spend and history
are in the picker.

Because the stage holds the real pane, everything in it is tmux's own: what
you type reaches the agent as it would anywhere, Shift+Enter included; copy
mode, the wheel and the mouse work; and the agent draws itself for the size
it is given rather than being re-wrapped. `agent-notify focused` is right
about it too, since the pane in front of you is the agent's.

## In background: provisional

Claude Code runs some sessions in a background daemon: the one a `/compact`
hands its conversation to, and the one `/resume` opens when the session it
picked is already running there. Such a session has no `$TMUX`, and a window
can show it from any pane, but Claude writes nowhere which window shows it. So
it cannot be placed on a pane, and the list shows it apart instead. A session
belongs in this group when tmux's capture found no server and no other
integration placed it. That rule is a stand-in, and a session in a plain
terminal would fall under it too. It should be revisited once the window
showing a session can be known (plan.md D-128).

## Keys

| Where | Key | Does |
| --- | --- | --- |
| list | `j` `k`, arrows, `Ctrl j/k/n/p`, `Alt j/k` | move the cursor; the stage follows once it rests |
| list | `g` `G` | first, last |
| list | `Enter` | onto the stage, like `Alt Tab` |
| list | `o` | go to the window the agent lives in, leaving the tab |
| list | `q` | close the tab |
| list | `F12` | look at the server now; the tab's hooks type it (see below) |
| stage | anything | it is the agent's pane |
| either | `Alt Tab` | switch between the list and the stage |
| anywhere | `Alt i` | show the tab; on the tab, go back to the window before |

`Alt i`, `Alt Tab` and `Alt j/k` come from your tmux configuration, not from
this program (see below).

## Borrowing a pane

Selecting an agent swaps its pane with the **stand-in** (`swap-pane -d -Z`),
an empty pane: one tmux made with no program in it. The stand-in takes the
agent's cell in the window it lives in, so that window keeps its layout, its
zoom, its active pane and its name; the agent takes the stand-in's cell on the
stage. Giving it back is the same swap again. Moving the cursor to another
agent sends the first one home and brings the next in a single tmux
invocation.

Four rules hold it together:

- **An agent is away from home only while the tab is on screen and the window
  it lives in is not.** Leave the tab, by any key, and the agent is home
  before the window you arrive at is drawn. tmux does that by itself, from
  hooks the borrow wrote as it swapped (see below), so it holds whether or
  not this program is running. The window an agent lives in looks untouched
  whenever you are in it.
- **Nothing is taken from under a terminal.** An agent whose window some
  terminal is showing stays there; the stage says where it is, and takes it
  once that terminal has left. A second terminal that goes to the window of
  the agent on your stage finds the agent, not a stand-in.
- **The cursor has to rest, and so do you.** A pane is swapped in about a
  tenth of a second after the cursor stops on it, so going down a list of six
  does not resize six agents; and as long after you arrive at the tab, so
  passing through it on the way to another window resizes none.
- **Nothing arrives under your hands.** While the focus is on the stage the
  cursor does not move by itself. If the agent you are typing into ends, its
  pane stays on the stage until you go back to the list.

### What it costs

An agent is resized when it comes onto the stage and again when it goes home,
so it redraws twice per visit. And the stage is a pane of the tab's window:
`kill-window` on the tab kills the agent it is showing, and tmux runs nothing
before a window dies that could save it. Close the tab with `q`, or with
`agent-notify-tmux-cockpit close`, which is what the bindings below do.

## When something goes wrong

What the tab knows is written in tmux and nowhere else: options on the panes,
and the hooks that send a borrowed pane home. The list never acts on a memory
of what it did: whenever something prompts it, and every five seconds
besides, it reads the server and does whatever is left to do. So a tab found
in any state comes right, and no failure takes an agent with it:

| What happens | What follows |
| --- | --- |
| The list dies, however it dies | The tab's window is left without its list, and a hook on it sends the agent home, takes the hooks away and closes the stand-in, which closes the tab. |
| The stand-in is closed | On the stage, the list makes another. Holding an agent's place, see the next line. |
| The window the agent lives in is killed | The agent, on the stage, survives it. It gets a window of its own in the session it came from. |
| The agent's pane closes on the stage | The stand-in comes back to the tab from the place it was holding, and the next agent takes the stage. |
| The session the tab was opened in is killed | The tab lives in a hidden session of its own, `_agents`, and is only linked into yours, so it is unlinked and nothing else. |
| The hooks are wiped | The list's own look sends home what should be home, within five seconds, and the next borrow writes them again. |
| Anything else | `agent-notify-tmux-cockpit restore` sends every borrowed pane home, with or without a tab. |

The options, if you want to look:

| Option | On | Holds |
| --- | --- | --- |
| `@agent-notify-tab` | the tab's window | `1` |
| `@agent-notify-tab-role` | the tab's two panes | `list` or `stand-in` |
| `@agent-notify-stand-in` | a borrowed pane | the pane holding its place |
| `@agent-notify-home-session` | a borrowed pane | the session it came from |
| `@agent-notify-renamed` | a borrowed pane | the window whose `automatic-rename` is off until it is back |

And the hooks, each a member named `agent-notify-tab` of the hook it is on, so
that yours are left as they are (`tmux show-hooks -g | grep agent-notify-tab`):

| Hook | Runs |
| --- | --- |
| `session-window-changed`, `client-session-changed`, `client-attached`, `client-detached`, `after-swap-window`, on the server | With no terminal on the tab, the borrowed pane goes home. With one, the pane goes home all the same if a terminal has come to the window it lives in, and the list is woken: `F12` is typed into its pane (`send-keys`). |
| `window-layout-changed`, on the tab's window | The list is woken; or, if the list is what went, the tab closes itself. |

They are written whole by every borrow, with the ids of the pane and its
stand-in in them, and taken away when the tab closes. Ones left behind by a
tab that was killed do nothing and say nothing: every test in them names the
window or pane it is about, and is false of all the others.

`agent-notify-tmux`'s marks are options of the agent's pane, so they come onto
the stage with it: while an agent is borrowed, its glyph is on the tab's window
in the status line, and back on its own window once it goes home.

## Commands

```
agent-notify-tmux-cockpit open [client]   show the tab, making it if there is none; on the tab, go back
agent-notify-tmux-cockpit close           send every agent home and close the tab
agent-notify-tmux-cockpit restore         send every agent home
```

`list` is what the tab runs in its own pane, and its only program. There is
one tab per tmux server. It lists the agents of that server only, plus the
background group above: an agent in another server or in a plain
terminal has no pane to borrow, and the picker already lists everything.

## The tmux side

```tmux
bind -n M-i run-shell "agent-notify-tmux-cockpit open '#{client_name}'"
bind -n M-Tab if -F "#{@agent-notify-tab}" { select-pane -t :.+ } { send-keys M-Tab }
bind -n M-j if -F "#{==:#{@agent-notify-tab-role},list}" { send-keys M-j } { select-pane -D }
bind -n M-k if -F "#{==:#{@agent-notify-tab-role},list}" { send-keys M-k } { select-pane -U }
```

Outside the tab, `Alt Tab` reaches the program in the pane as usual and
`Alt j/k` move between panes. And wherever you bind `kill-window`, tmux's own
prefix `&` included, let the tab close itself instead:

```tmux
bind & if -F "#{@agent-notify-tab}" { run-shell "agent-notify-tmux-cockpit close" } { confirm-before kill-window }
```

Two things to keep out of your configuration. A line that sets one of the
hooks above without an index (`set-hook -g session-window-changed …`) replaces
the whole hook, this program's member with it, every time the file is loaded:
give yours an index or a name (`session-window-changed[mine]`). And nothing
here needs `focus-events`; with it on the list hears of the focus moving
between its two halves a little sooner.

## Configuration

Under `integrations.tmux-cockpit` in `~/.config/agent-notify/config.yaml`
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
shapes, that the server sends an agent home with nothing of this program
running, that an agent outlives the window it was borrowed from — runs against
real tmux servers on private sockets with no configuration file, with a pane
of a second server as the attached terminal, and skips without tmux.

## Building

`make install` at the root, like every module. It is run from a key and never
by core: `agent-notify doctor` lists it as "yours to start".
