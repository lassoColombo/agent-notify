# agent-notify-tmux-manager

A tab of tmux given over to your agents, the way zellij's inbox is. It has two
panes:

- **Left:** every agent running in a pane of this tmux server, grouped by what
  it wants from you (needs you, broke, finished, working, idle). Each entry
  shows its name and age, and below that its `session:window` and directory.
- **Right:** a live mirror of the selected agent's pane. It takes what you
  type, so you can answer a prompt or send a message without leaving the
  tab.

```
── agents ──────────────┬── alpha ────────────────────────────────────
   needs you 1          │ Do you want to proceed?
▌ alpha             2s  │ ❯ 1. Yes
▌ home:1 alpha          │   2. No
                        │
   working 1            │
  beta             31s  │
  home:2 beta           │
```

Nothing moves. The agent's pane stays in the tab it lives in. The mirror reads
its screen with `capture-pane` about ten times a second and types into it with
`send-keys`. Closing the tab, or either half of it, never takes an agent with
it.

## Keys

| Where | Key | Does |
| --- | --- | --- |
| list | `j` `k`, arrows, `Ctrl j/k/n/p`, `Alt j/k` | move; the mirror follows |
| list | `g` `G` | first, last |
| list | `Enter` | into the mirror, like `Alt Tab` |
| list | `o` | go to the agent's own pane, leaving the tab |
| list | `q` | close the tab |
| mirror | anything | goes to the agent |
| mirror | `Alt v`, the wheel | scroll through the agent's history: `j/k` `J/K` `d/u` `g/G`, `Esc` or `q` back; typing anything else leaves scrolling and is sent |
| either | `Alt Tab` | switch between the list and the mirror |

`Alt i`, `Alt Tab` and `Alt j/k` come from your tmux configuration, not from
this program (see below). `Alt v` is the mirror's own; a tmux binding on it
has to let it through to the mirror.

## How it fits together

There are three commands in one binary, because the tab is two panes:

```
agent-notify-tmux-manager        lay the tab out: this pane becomes the list
agent-notify-tmux-manager list   the list
agent-notify-tmux-manager view   the mirror
```

The two halves talk only through tmux:
- The list writes the selected pane's id into the tab's `@agents_selected`
  option, and gives the mirror's pane the agent's name as its title.
- The mirror reads the selection on every frame, in the same tmux call that
  reads the agent's screen.
- Both panes carry `@agents_role` (`list` or `view`), which is what the tmux
  bindings test.

An agent's pane comes from its record: agent-notify-tmux's capture of `$TMUX`
and `$TMUX_PANE`. A pane that has since closed is left out, and so is an agent
in another tmux server, in zellij, or in a plain terminal.

Every tmux call says `-u`, for the same reason agent-notify-tmux does (D-123).

## The mirror's limits

- **It re-wraps rather than resizes.** The agent's pane is usually wider than
  the mirror, and resizing it would rearrange the tab it lives in. Lines wider
  than the mirror break at its edge, so nothing is cut off, though a
  full-width rule breaks in two. A pane taller than the mirror shows its
  bottom, which is where an agent's prompt and newest output are.
- **Keys arrive as plain bytes,** so Shift+Enter is an Enter. In Claude Code,
  `\` then Enter is a newline that survives.
- **A paste goes through tmux's `paste-buffer -p`.** It arrives as one paste,
  bracketed if the agent asked for bracketed paste, so a multi-line paste
  doesn't send itself line by line.

## The tmux side

```tmux
bind -n M-i if -F "#{W:#{?#{==:#{window_name},agents},1,}}" { select-window -t :=agents } { new-window -n agents agent-notify-tmux-manager }
bind -n M-Tab if -F "#{@agents_role}" { if -F "#{==:#{@agents_role},list}" { select-pane -R } { select-pane -L } } { send-keys M-Tab }
bind -n M-j if -F "#{==:#{@agents_role},list}" { send-keys M-j } { select-pane -Z -D }
bind -n M-k if -F "#{==:#{@agents_role},list}" { send-keys M-k } { select-pane -Z -U }
```

Outside the tab, `Alt Tab` reaches the program in the pane as usual and
`Alt j/k` move between panes. In the list they move between agents instead; in
the mirror they still move between panes. If Alt v enters copy mode, pass it
to the mirror:

```tmux
bind -n M-v if -F "#{==:#{@agents_role},view}" { send-keys M-v } { copy-mode }
```

## Building

`make install` at the root, like every module. It is run from a keybinding and
never by core: `agent-notify doctor` lists it as "yours to start".
