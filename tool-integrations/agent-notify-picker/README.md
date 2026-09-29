<div align="center">
  <h1>agent-notify-picker</h1>
  <p><strong>Every agent on your machine, most urgent first, and enter to be taken to one</strong></p>
  <p>
    A tool-integration for <a href="../../agent-notify">agent-notify</a><br>
    Type to filter, read what the agent said underneath, press enter<br>
    fzf owns the screen; the rows, the preview and the palette are ours
  </p>
</div>

---

```
  ⌕  filter sessions…                                                          3/5
 ──────────────────────────────────────────────────────────────────────────────────
 ▌  ⚠ blocked/permission-prompt  12m  agent-notify  ~/projects/agent-notify  ⎇ browse
    ✕ broke/tool-error           52m  lenny-62      ~/projects/work/lenny    ⎇ main
    ↻ working/compacting          2m  wholeness     ~/projects/agent-notify  ⎇ view-changed
 ── agent-notify · blocked-on-you/permission-prompt · 12m ──────────────────────────
 May I run rm -rf ./build ?

 ▌ What it would remove

  ./build — 2.1G of cached object files

 ──────────────────────────────────────────────────────────────────────────────────
 where    ~/projects/agent-notify  ⎇ browse
 running  claude-opus-5  ·  127k tokens  ·  pid 44182
 ──────────────────────────────────────────────────────────────────────────────────
 earlier
   12m  working → blocked-on-you
   31m  tests are green
```

---

- [What it is](#what-it-is)
  - [A display that is not a daemon](#a-display-that-is-not-a-daemon)
  - [fzf owns the screen, as a library](#fzf-owns-the-screen-as-a-library)
- [Installation](#installation)
  - [Prerequisites](#prerequisites)
  - [1. Clone the monorepo and build](#1-clone-the-monorepo-and-build)
  - [2. Put the binary somewhere, by replacing it](#2-put-the-binary-somewhere-by-replacing-it)
  - [3. Register it with agent-notify](#3-register-it-with-agent-notify)
  - [4. Bind it to a key](#4-bind-it-to-a-key)
  - [5. Check it worked](#5-check-it-worked)
- [Configuration](#configuration)
  - [Where the file is](#where-the-file-is)
  - [The table core reads](#the-table-core-reads)
  - [Keys: a key is yours, an action is not](#keys-a-key-is-yours-an-action-is-not)
  - [Colours: the hues are yours, where they go is not](#colours-the-hues-are-yours-where-they-go-is-not)
  - [Environment variables](#environment-variables)
  - [What a bad configuration does](#what-a-bad-configuration-does)
  - [What is deliberately not configurable](#what-is-deliberately-not-configurable)
- [The command line](#the-command-line)
- [The list](#the-list)
- [The preview](#the-preview)
- [One hue, one meaning](#one-hue-one-meaning)
- [What it reads, and what it runs](#what-it-reads-and-what-it-runs)
- [Testing](#testing)
- [What is unfinished](#what-is-unfinished)

---

## What it is

Every session [agent-notify](../../agent-notify) knows about, most urgent first,
with what the highlighted one is working on and last said underneath it. Type to
filter, press enter, and you are in that agent's pane.

### A display that is not a daemon

It reads through the SDK, declares itself in the config, and renders the answer
core computed — exactly like the menu bar and the zellij pane titles. What
differs is that it needs a terminal, so it is run when you press a key and it
goes away when you have chosen.

That is why **its table has no `binary`**, and the absence is the statement.
Naming one means "core may run this" — the hook runs it on the path an agent is
waiting on, the session-watcher runs it to render or to focus — and every one of
those is a picker with nowhere to draw. The path lives in the keybinding
instead, which is the only thing that runs this and where you would look for it.

### fzf owns the screen, as a library

This program is about a thousand lines and none of them draw anything. It builds
the rows, hands them to [fzf](https://github.com/junegunn/fzf) through a channel
— the same fzf, imported as a Go package, not shelled out to — and gets back the
one that was chosen.

That is the third answer to the same question. The first version was a terminal
UI of its own: a model, an update, a view, a keymap, and three separate layout
bugs that only a real terminal could find. The second used a fuzzy-finder
library whose preview is hardcoded to the right half of the window. fzf gives
the screen away and still leaves everything that is actually agent-notify's:

- **`--no-sort`, which is the rule this picker keeps.** Typing narrows the list
  and never reorders it. Core computes one urgency order for every display
  (§A5.5), and a list that re-sorts by match score moves the row you were about
  to press enter on, on every keystroke.
- **`--layout=reverse`**, so the most urgent session is the first line you read
  rather than the last.
- **A horizontal split, sized by how many sessions there are.** `down,<lines>`
  rather than `down,72%`: the number of rows is known before fzf is started, so
  three sessions do not sit in a third of a tall pane with air under them, and
  twelve do not scroll while the preview below has rows to spare. The list is
  capped at two fifths of the pane, because the message is what the window is
  for, and a pane too short to divide falls back to `down,68%`.
- **`--highlight-line`, and a solid pointer.** Every row arrives pre-coloured
  under `--ansi`, so fzf's `fg+` and `hl+` have nothing left to repaint: the
  selected row is lifted by its background across the whole width and marked by
  `▌` rather than by a glyph a font may not have.
- **No header and no footer.** A header saying how many sessions there are
  spends the brightest row on the screen on a number fzf already prints beside
  the prompt; a footer is drawn below the list and above a `down` preview, which
  splits the window at the exact point the eye moves from one to the other. The
  empty query's ghost text carries the only thing left — whether this run
  included ended sessions. The one thing that does get a header is a complaint
  about the config file, which is worth a row precisely because it is temporary.
- **`bg-transform-preview-label` on `focus`**, which is what lets the pane's
  identity live in its border rather than in its first two lines.

## Installation

Four things have to be true before a keypress opens a picker: the binary exists
somewhere a keybinding can reach, agent-notify's config file names it, something
binds a key to it, and core itself is installed and finding sessions. This
section does all four.

### Prerequisites

- **[agent-notify](../../agent-notify) core**, built and on `PATH`. The picker
  reads sessions through core's SDK and jumps by running
  `agent-notify focus-session`, so a picker without core is a picker with
  nothing to list and nowhere to go.
- **At least one agent-integration** — [claude](../../agent-integrations/agent-notify-claude)
  or [codex](../../agent-integrations/agent-notify-codex) — installed and
  reporting, so that `agent-notify list` has rows.
- **A container integration**, if you want enter to actually take you
  somewhere: [zellij](../agent-notify-zellij) and
  [aerospace](../agent-notify-aerospace-container). Without one, enter runs
  `focus-session` and `focus-session` says it cannot.
- **Go 1.26 or newer** to build. `.tool-versions` pins 1.26.2.
- **A Nerd Font in your terminal.** The state glyphs and the branch mark are
  Font Awesome codepoints out of the private-use area — the same marks the bar
  and the zellij tab titles wear. Without one you get boxes where the badges
  should be; everything else still reads, because every glyph sits beside its
  word.
- **No fzf binary is needed.** fzf is compiled in as a library, so there is
  nothing to install and nothing whose version can drift from what this was
  built against.

### 1. Clone the monorepo and build

Core and every integration live in one repository, and `make install` at the
root builds every module into `$(go env GOPATH)/bin`. That directory has to be
on `PATH`: `agent-notify install picker` finds this program there by name, and
the keybinding it prints names the absolute path.

```sh
git clone git@github.com:lassoColombo/agent-notify.git ~/projects/agent-notify
cd ~/projects/agent-notify
make install
```

`go install` replaces the binary rather than writing over it, which matters on
macOS: `cp` over a running path reuses the inode, and the signature macOS
cached no longer matches, so the next exec is killed with SIGKILL and no
message. The symptom is a keybinding that opens a pane which closes instantly.

### 2. Register it with agent-notify

```sh
agent-notify install picker
```

`install` writes `conf.d/picker.toml` beside your `config.toml` and prints the
keybinding, which is KDL and belongs in a different file of yours:

```
$ agent-notify install picker
wrote /Users/you/.config/agent-notify/conf.d/picker.toml

And this in zellij's config.kdl to open it with a key:

    bind "Alt a" {
        Run "/Users/you/go/bin/agent-notify-picker" {
            name "agents"
            floating true
            close_on_exit true
            width "90%"
            height "90%"
            x "5%"
            y 1
        }
    }
```

The table has no `binary`, and that is what says core must never run this: a
picker needs a terminal, and nothing core runs has one. `install` takes no
options. `agent-notify uninstall picker` removes the drop-in.

### 3. Bind it to a key

The keybinding is printed rather than written, and for the same reason the table
is: a zellij config is KDL, full of your own comments and your own key choices,
and an integration that rewrote it would be guessing at both. Paste it into
`~/.config/zellij/config.kdl`, inside your keybindings block:

```kdl
bind "Alt a" {
    Run "/opt/homebrew/bin/agent-notify-picker" {
        name "agents"
        floating true
        close_on_exit true
        width "90%"
        height "90%"
        x "5%"
        y 1
    }
}
```

`close_on_exit true` is what makes enter feel like a jump rather than like
starting a program: focusing a pane hides the floating layer by itself, and the
picker exits a moment later, so the pane it was in goes away behind you.

The absolute path is not decoration. A keybinding's `PATH` is not your shell's,
which is why every integration here writes one.

**Not in zellij?** The picker is an ordinary terminal program that wants a
terminal and exits when it is done, so anything that can open one works. In tmux
a popup is the closest equivalent:

```sh
tmux bind-key -n M-a display-popup -E -w 90% -h 90% /opt/homebrew/bin/agent-notify-picker
```

And from a shell, a widget that jumps without leaving your prompt is just the
program:

```nu
# Nushell — alt-a opens the picker, enter jumps, escape gives your prompt back
$env.config.keybindings ++= [{
  name: agent_picker
  modifier: alt
  keycode: char_a
  mode: [emacs vi_normal vi_insert]
  event: {send: executehostcommand cmd: "agent-notify-picker"}
}]
```

### 4. Check it worked

```sh
agent-notify list              # there are sessions to pick from at all
agent-notify doctor            # the picker's table is seen, its binary resolves
agent-notify-picker --print    # opens the window, prints the key you chose
```

`--print` is the one to reach for while setting up: it exercises the whole
window — the config, the rows, the preview, the keys — and answers with the
session's key on stdout instead of trying to jump, so a broken container
integration cannot be mistaken for a broken picker.

If the window opens with a gold line across the top, that is a complaint about
your configuration; see [What a bad configuration
does](#what-a-bad-configuration-does). If a keypress opens a pane that vanishes
instantly, it is almost always a binary copied over the inode rather than
replaced, which macOS kills on exec; `make install` replaces it.

## Configuration

Nothing needs configuring. Running with no configuration file at all is the
supported case: the defaults below are what this program shipped with and they
are what you get.

### Where the file is

One file, and it is core's:

| | path |
| --- | --- |
| default | `~/.config/agent-notify/config.toml` |
| with `XDG_CONFIG_HOME` set | `$XDG_CONFIG_HOME/agent-notify/config.toml` |
| with `AGENT_NOTIFY_ROOT` set | `$AGENT_NOTIFY_ROOT/config.toml` |

The picker never locates, reads or parses that file itself — it asks the SDK,
which is what keeps the layout core's to change (§A10.4). `agent-notify-picker
install` prints the resolved path in its explanation, and `agent-notify doctor`
prints it too, so there is no need to work it out by hand.

### The table core reads

Two keys, and both are core's rather than this program's:

```toml
[integration.picker]
```

| key | type | default | what it changes |
| --- | --- | --- | --- |
| `binary` | — | absent, deliberately | Do not add one. It means "core may run this", and a picker run by core is a picker with nowhere to draw. The path belongs in your keybinding. |
| `enabled` | bool | `true` when absent | `false` turns the picker off — which now means what it says, since nothing was ever going to start it. |

Everything else this program understands is under
`[integration.picker.settings]`, which core hands over verbatim and never reads
(§A17 R7). There are exactly two tables in it, `keys` and `colors`, and **a key
nobody declared is refused by name** — because a misspelled setting that changes
nothing and says nothing is the config bug people give up on.

### Keys: a key is yours, an action is not

Typing filters; there is no key to press first. The preview is what needs
scrolling — the list is three rows and the message under it can be a hundred —
so the four keys nearest the home row scroll it:

| what it does | the action | default keys |
| --- | --- | --- |
| filter | — | type |
| move through the list | `down` `up` | ↑ ↓, ctrl-n / ctrl-p |
| scroll the message, a line | `preview-down` `preview-up` | ctrl-j / ctrl-k |
| scroll the message, half a page | `preview-page-down` `preview-page-up` | ctrl-d / ctrl-u |
| go to that session | `choose` | enter |
| leave | `leave` | esc, ctrl-c |

Those four scrolling keys are fzf's defaults for something else, and each thing
they displace has another way: ctrl-j/k also move the list (↑↓ and ctrl-n/p
still do), ctrl-u clears the query (backspace still does), and ctrl-d quits on
an empty query (esc and ctrl-c still do). Scrolling the preview had no other way
at all.

The middle column is the table, and it is the colours table the other way round:
a hue is yours and where it is spent is this program's, while a **key is yours
and what there is to ask for is this program's**.

```toml
[integration.picker.settings.keys]
down              = ["down", "ctrl-n"]
up                = ["up", "ctrl-p"]
preview-down      = "ctrl-j"
preview-up        = "ctrl-k"
preview-page-down = "ctrl-d"
preview-page-up   = "ctrl-u"
choose            = "enter"
leave             = ["esc", "ctrl-c"]
```

Every key is optional and every value is either one key or a list of them:

| key | type | default | binds fzf's |
| --- | --- | --- | --- |
| `down` | string or list of strings | `["down", "ctrl-n"]` | `down` |
| `up` | string or list of strings | `["up", "ctrl-p"]` | `up` |
| `preview-down` | string or list of strings | `"ctrl-j"` | `preview-down` |
| `preview-up` | string or list of strings | `"ctrl-k"` | `preview-up` |
| `preview-page-down` | string or list of strings | `"ctrl-d"` | `preview-half-page-down` |
| `preview-page-up` | string or list of strings | `"ctrl-u"` | `preview-half-page-up` |
| `choose` | string or list of strings | `"enter"` | `accept` |
| `leave` | string or list of strings | `["esc", "ctrl-c"]` | `abort` |

Something small, to see the shape of it — vim keys for the preview and a second
way out:

```toml
[integration.picker.settings.keys]
preview-down = ["ctrl-j", "ctrl-e"]
preview-up   = ["ctrl-k", "ctrl-y"]
leave        = ["esc", "ctrl-c", "ctrl-g"]
```

- **Keyed on the action, never on the key.** The eight are declared, so a
  misspelled `preivew-down` is refused **by name**; a table keyed on keys is an
  open namespace where the same typo binds nothing and says nothing. It is also
  what keeps `toggle-sort` out: fzf's `--bind` grammar passed through would let
  you re-sort the list by match score, and narrowing without reordering is the
  one rule this picker keeps.
- **A value replaces the default rather than adding to it**, and `""` or `[]`
  gives the key back. Only what *this program* binds is yours to move: fzf's own
  defaults stand underneath, so `preview-page-down = []` is ctrl-d quitting on
  an empty query again.
- **A key asked to do two things refuses the whole table**, with the two actions
  named, and the keys this program shipped with are the ones in force. Half a
  keymap — the half that did not collide — is a window where some keys do what
  you asked and some do what this program asked, which is worse to sit in front
  of than either. It catches the ordinary version of the mistake, too, which is
  a key moved onto an action while the action that had it was left where it was.
- **fzf checks the key names, not this program.** `ctrl-j`, `alt-up`, `f13` are
  fzf's vocabulary, and a second copy of it here would be one that drifts from
  the library this is built against. The binds go over as you wrote them and
  fzf's answer is the validation: a table it will not take is dropped for the
  keys this program shipped with, and the window opens with fzf's own words in
  its header. Nobody presses a key and gets a pane that appears and vanishes.
- **A table that binds nothing at all is not passed at all.** `--bind=` is a
  thing fzf refuses, and somebody who has given every key back has asked for an
  fzf with its own keys rather than for an error.

Typing is not in the table. There is no key to press before you filter, which is
the whole shape of the thing.

### Colours: the hues are yours, where they go is not

The twelve slots this program paints with are a table, and nothing else about
the palette is configurable. The asymmetry is the whole design: you say what a
slot **is**, `theme.go` says what a slot is **for**.

```toml
[integration.picker.settings.colors]
base02 = "#26233a"  # overlay     — the row you are on, the chip behind inline code
base03 = "#6e6a86"  # muted       — rules, borders, scrollbars, the marks between words
base04 = "#8c88a6"  # subtle      — ages, fact names, earlier messages, unmatched rows, idle
base05 = "#e0def4"  # text        — names and prose
base08 = "#eb6f92"  # love        — blocked-on-you
base09 = "#f6c177"  # gold        — broke, a string in a fenced block, the complaint header
base0A = "#ebbcba"  # rose        — branches, matched characters
base0B = "#31748f"  # pine        — a keyword, inside a fenced code block and nowhere else
base0C = "#9ccfd8"  # foam        — finished-a-turn
base0D = "#c4a7e7"  # iris        — paths, the prompt, the pane label, links in a message
base13 = "#fff0ee"  # bright rose — the pointer rail, headings in a message
base14 = "#609fbb"  # bright pine — working
```

Every key is optional, every value is a string, and **the only accepted spelling
is `#rrggbb`**. Write one line or none: an absent key keeps the default, which
is the Rosé Pine above.

| key | default | what it paints |
| --- | --- | --- |
| `base02` | `#26233a` | the selected row's background, the chip behind inline code |
| `base03` | `#6e6a86` | every rule, border, separator, scrollbar and middot; the dimmed part of a path; an elapsed time in the timeline |
| `base04` | `#8c88a6` | ages, fact names, earlier messages, unmatched rows, `idle`, h5/h6 and quotes in a message |
| `base05` | `#e0def4` | session names, the query, prose in a message |
| `base08` | `#eb6f92` | `blocked-on-you`, glyph and detail |
| `base09` | `#f6c177` | `broke`, a string inside a fenced block, the complaint header |
| `base0A` | `#ebbcba` | branches, the characters your query matched, h3/h4 in a message |
| `base0B` | `#31748f` | a keyword inside a fenced code block, and nowhere else on screen |
| `base0C` | `#9ccfd8` | `finished-a-turn`, glyph and detail |
| `base0D` | `#c4a7e7` | the leaf of a path, the prompt, the spinner, the pane label, links in a message |
| `base13` | `#fff0ee` | the pointer rail and the marker, h1/h2 in a message |
| `base14` | `#609fbb` | `working`, glyph and detail |

A whole palette, pasted from the file you already keep:

```toml
[integration.picker.settings.colors]
base02 = "#303446"
base03 = "#51576d"
base04 = "#626880"
base05 = "#c6d0f5"
base08 = "#e78284"
base09 = "#ef9f76"
base0A = "#e5c890"
base0B = "#a6d189"
base0C = "#81c8be"
base0D = "#8caaee"
base13 = "#f2d5cf"
base14 = "#99d1db"
```

- **The comments are also the list of what moves together.** Change `base09` and
  you change `broke` in the list and a string inside a fenced block, in one
  line. That is what a twelve-line table costs, and it is only safe because
  `theme.go` is careful about which things share a slot in the first place — see
  [One hue, one meaning](#one-hue-one-meaning).
- **The slots are named rather than hue-worded** — no `blue`, no `red` — because
  the hue words lie on this machine: the "blue" slot is iris, which is purple,
  and `base0A` is annotated ANSI yellow while being rose. A slot name is what
  `scheme.yaml` is written in, so this table is a paste from a file you already
  have.
- **Twelve, because twelve is what gets painted.** A slot nothing spends is not
  offered, since a key that changes nothing is the same silence the strictness
  exists to break. `base00` is absent for a different reason: the canvas is
  never painted at all.
- **One table covers the whole window, not only the rows.** The embedded glamour
  stylesheet is written in those same slot names and resolved before the
  renderer sees it, so moving `base0D` moves the links in a message along with
  the paths in a row. A window half in your palette and half in mine would be
  worse than one wholly in mine.
- **`#rrggbb` and nothing else**, though fzf and lipgloss would each take more —
  a colour number, a name, an ANSI index. Every one of these values is also
  handed to glamour, which takes fewer of them, and a table where one line says
  `#eb6f92` and the next says `red` is a table nobody can read as a palette.

### Environment variables

This program reads no variable of its own. What it obeys, it obeys through core
and through fzf:

| variable | read by | effect |
| --- | --- | --- |
| `AGENT_NOTIFY_ROOT` | core's SDK | moves the config file, the state directory and the runtime directory beneath one root — the one override the design allows (D-20). Set it and this picker lists that instance's sessions. |
| `XDG_CONFIG_HOME` | core's SDK | where `agent-notify/config.toml` is looked for, when `AGENT_NOTIFY_ROOT` is unset. |
| `XDG_STATE_HOME` | core's SDK | where session records are read from on Linux. macOS uses `~/Library/Application Support/agent-notify` and ignores it. |
| `PATH` | core's SDK | how `agent-notify` is found for the jump, unless `agent-notify-binary` names it in the config file. |
| `FZF_PREVIEW_COLUMNS` | this program's `--preview` child | how wide to wrap the message. fzf sets it; the fallback is 80, narrow on purpose, because text that is too narrow is readable and text that is too wide is a staircase. |

One core-level key matters here and is not in this integration's table, because
it is not this integration's business:

```toml
agent-notify-binary = "/opt/homebrew/bin/agent-notify"
```

That is what enter runs. A keybinding's `PATH` is not your shell's (D-33), so if
`agent-notify` is somewhere unusual, name it there once and every integration
finds it.

`keep-ended-sessions` — core's, seven days by default — is what bounds the set
`--all` shows you.

### What a bad configuration does

It complains in the header and opens anyway. That is §A14's rule — a malformed
configuration must never break the thing — applied to the one program here that
is opened by a keypress: `close_on_exit true` means a picker that refuses to
start is a floating pane which appears and vanishes, indistinguishable from a
crash, and the thing you file a bug about rather than fix.

```
 [integration.picker.settings.colors] base09 = "gold" is not a colour, so base09 is still #f6c177 (and 2 more)
  ⌕  filter sessions…                                                          5/5
```

One line rather than all of them, in gold, with a count of the rest — a file
with a dozen typos in it would otherwise push the sessions off the screen, which
is the picker refusing to open by another route.

| what is wrong | what happens |
| --- | --- |
| a colour that is not `#rrggbb` | that slot keeps its default, the others still apply, complaint in the header |
| a key nobody declared, anywhere under `settings` | the whole `settings` table fails to decode: every default stands, and the offending key is named in the header |
| a key bound to two actions | the whole keymap is refused, this program's own keys are in force, both actions named in the header |
| a key name fzf does not know | fzf refuses the binds, this program's own keys are in force, fzf's own words in the header |
| a config file core refuses whole | every default stands, complaint in the header |

### What is deliberately not configurable

- **The order of the list.** Core computes one urgency order for every display
  (§A5.5). A picker that sorted differently would disagree with the bar beside
  it, and `toggle-sort` is kept out of the keymap for the same reason.
- **Which slot paints what.** That is the discipline the palette rests on, and a
  table that let you spend a state hue on a session name would let you undo it
  in one line.
- **The background.** `bg:-1` everywhere. This opens in a floating pane over
  your work, and a picker that repaints the canvas reads as a different
  application rather than as a layer on top of one.
- **The layout** — the split, the widths, the column caps, the glyphs, the
  prompt, the ellipsis, the scrollbar. All of it is measured against the list
  actually in front of you, and none of it is something only you can know, which
  is the test §A14 sets for what belongs in a config file at all.
- **`focus`, which redraws the pane label.** It is machinery rather than a key,
  and keeping it in its own `--bind` is what makes it impossible for a keymap to
  take it away.
- **Anything about where a session is.** Which containers there are and what
  order they nest in is core's (§A11.2), and configured there.

## The command line

```
agent-notify-picker            the live sessions
agent-notify-picker --all      the ended-but-resumable ones too — the set bounded
                               by keep-ended-sessions (§A7.5)
agent-notify-picker --print    answer with the session's key on stdout instead of
                               going to it, for binding it to something of your own
agent-notify-picker --help     the above
agent-notify install picker    print the table to add and the keybinding to bind
```

```
$ agent-notify-picker --print
claude-01K5X9QF7T2M3N4P5R6S7T8V9W
```

Exit 0 when you chose something, and also when you changed your mind — escape
and ctrl-c are not failures. Exit 1 when there is nothing to list, when the
store cannot be read, or when the jump failed; exit 2 for an option that is not
one.

`--preview` and `--label` are on the command line too, and they are not yours to
run: fzf runs this program again with them, once per row you look at and once
per move of the cursor.

## The list

One line reads left to right as: what state this is, what precisely it is
waiting on, how long it has been that way, who it is, and where it lives.

```
 ▌  ⚠ blocked/permission-prompt  12m  agent-notify  ~/projects/agent-notify  ⎇ browse
```

The first three are a narrow status gutter that answers "which of these wants
me", and they are **adjacent on the left** because they are one question. The
age spent a version on the far side of the name, where it floated in the middle
of the row with whitespace on both sides of it and belonged to neither
neighbour.

**The state is a word as well as a shape, and the two together are a badge.** A
glyph-only column was tried first, with a legend above the list naming the six
shapes; a screenshot settled it. The legend read as a sixth row of the list, and
three identical speech bubbles in a list of five still had to be looked up
against it. A shape resolves faster than a word, which is why the glyph is
there; a word needs no key, which is why the word is there too. The word is
core's own, shortened only where core's own is a sentence —
`blocked/permission-prompt` rather than `blocked-on-you/permission-prompt`, and
the canonical string is spelled out in full in the pane label for the row you
are on.

| glyph | state | hue |
| --- | --- | --- |
| warning triangle | `blocked` | love |
| a cross | `broke` | gold |
| speech bubble | `finished` | foam |
| circular arrows | `working` | bright pine |
| a dot | `idle` | subtle |
| a hollow dot | `ended` | muted |

**Only the badge is coloured.** A session's name is `base05` whatever it is
doing, so that a name can be learned: a row painted end to end in its state's
hue makes identity a moving target, and four such rows read as four kinds of
object rather than four instances of one.

**The columns are capped, not fixed** — the rule `agent-notify list` already
uses (R24). A cap stops one enormous name pushing everything after it off the
row; measuring the rest against what is actually in the list is what keeps a
screenful of short names from leaving a gutter of air. Names cap at 24, states
at 26 (because `blocked/permission-prompt` is 25, and the state that wants you
is exactly the one that must not be cut short), paths at 44.

**A path is dimmed down to its leaf** and elided from the left when it is long,
a whole component at a time: `…/modules/nu-http-client-generator`. Five
components in one colour is the longest and loudest thing on a row, competing
with the name for a glance it does not deserve, and only the last of them tells
two sessions apart. Cutting the tail instead would take the repository's name
off, which is the one part that identifies it. The branch follows behind a dim
`⎇`, in the two colours a shell prompt already writes a path and a branch in.

The whole row is also what the matcher matches against, so typing `browse` finds
the session on that branch and typing `blocked` finds everything that wants you.

## The preview

`--preview` runs `agent-notify-picker --preview <key>` and `focus` runs
`agent-notify-picker --label <key>`. That is how every fzf preview works, and it
is what makes the pane a full renderer — markdown, metadata and history — rather
than a string squeezed through a callback. It re-reads the store per row, which
costs a few milliseconds and is the only way the pane can show history at all:
history is read on demand, for one session, and never arrives with a delta
(§A7.7).

The message leads, because it is the thing being decided on:

- **What it said, as markdown.** [glamour](https://charm.land) renders it:
  headings, lists, quotes, fenced code and tables come out as headings, lists,
  quotes, code and tables. A picker that printed the source would show a wall of
  asterisks at the moment somebody is deciding which of four agents to answer. A
  message that cannot be rendered is printed as it arrived — the point is
  reading what the agent said, and a renderer that fails should not eat it.
- **What it is working on and spending**, as a two-column block rather than a
  line of values between middots, so a name in the margin says what the value
  beside it is: `where` is the directory and the branch checked out there, which
  core reads on the hook path rather than taking the agent's word for it
  (§A7.4.4); `running` is the model, the tokens and the pid. Each part
  disappears when there is nothing to say, because a zero token count almost
  always means an adapter that reports no usage rather than a session that has
  spent nothing (§A7.4.3).
- **What it said before**, from the session's own history, newest first: the
  messages and the state changes interleaved, one line each, cut rather than
  wrapped. This is a timeline and not a transcript — an agent's last-but-one
  message can be four hundred words, and wrapping it buries the state changes it
  sits between. The message already at the top of the pane is not repeated down
  here.
- **Who it is, in the border rather than in the body.** The name, core's
  canonical `(kernel, detail)` string and the age are the pane's *label*, drawn
  by a second re-run of this program on fzf's `focus` event. They used to be the
  pane's first line, two rows under the row that already said them.

**There were two meters here and they are gone.** How full the context window
was, and the tightest of the account's allowances with how long it had left,
with their digits riding in the pane label beside the three facts. Both left the
record in D-76, because only one of the two agents could fill either, so both
bars drew for codex and never for Claude. Nothing in the chrome replaced them.

A session that ends between the keypress and the read gets `that session is
gone` rather than an error, and one that has not said anything yet gets `nothing
said yet`.

## One hue, one meaning

The palette is this machine's own Rosé Pine. What `theme.go` is careful about is
not which hues it spends but **how many meanings each one carries**, because a
reader cannot hold three meanings for one colour in the two seconds this window
is open — and an earlier version of this program asked them to. Gold was `broke`
in the list, tokens in the facts, inline code in the message and the header
across the top. Foam was `finished-a-turn` and also the model's name. Iris was
paths and also every markdown heading. So:

- **The four state hues — love, gold, foam, bright pine — appear on the state
  glyph and on the state's detail, and nowhere else.** Not on the name, not on a
  heading.
- **Identity and prose are the neutral ramp, always.**
- **Iris is paths and chrome, rose is branches and matched characters**, and
  neither is ever a state. That pair is the one a shell prompt already writes a
  path and a branch in.

Two of the greys were measured rather than chosen, and both were wrong. `base02`
is a *background* slot — 1.16:1 against this canvas — and every rule, border,
separator and scrollbar was drawn in it, which is not a dim line but no line at
all; they are `base03` now (3.42:1), which is what `scheme.yaml` says `base03`
is for. And `working` was pine at 3.38:1 while `ended` was muted at 3.42:1 — the
two commonest rows in a real list, the same weight, told apart by hue alone at a
contrast where hue barely resolves. Working is **bright pine** (`base14`,
6.03:1), which keeps the family and can be seen. `base04` (5.2:1) is the
readability floor and the lowest thing any actual word is drawn in.

The rule that a calm number should be grey outlived the meters it was written
for: a context window at 92% and one at 12% are different news, and only the
first should have been competing with the glyphs. The next thing here that is a
fraction is drawn by it.

## What it reads, and what it runs

Nothing here walks agent-notify's directory layout or drives a container:

- `Read` (or `ReadIncludingEnded` for `--all`) for the sessions, already ordered
  by urgency and with the liveness decision already applied — a session whose
  process was killed is not offered as working (D-30).
- `History` for the preview of the one under the cursor.
- `Settings` for this integration's own table, and `ConfigFile` for the one line
  of `install` that says where to put the table.
- `CoreBinary`, then `agent-notify focus-session <key>` for the jump. Which
  containers there are, what order they nest in and what each one answers are
  core's business (§A11.2) — a display that learned any of that would be one
  that disagrees with the bar beside it. Its output is left on the terminal on
  purpose: a jump that fails says why, in the words the container that refused
  it wrote.

It never opens the subscriber socket. A picker lives for as long as it takes to
choose, and one cold read at startup is the whole of its relationship with the
store.

## Testing

```sh
env -u GOROOT go test ./...
```

What is tested is what this program decides — what a row says, what the preview
says, what the keymap resolves to, and what fzf is asked for — because the
screen itself is fzf's and a test of it would be a test of somebody else's
library. The cheapest one pays the most: it hands the option list to fzf's own
parser, so a mistyped colour key or a preview-window spec fzf will not accept is
a failing test rather than a picker that refuses to open on the one keypress
somebody makes.

## What is unfinished

- **Nothing is published.** There is no remote, no release, no `go install`
  path and no package. `go.mod` carries `replace github.com/lassoColombo/agent-notify
  => ../../agent-notify`, which is what makes a build from inside the monorepo
  work and a clone of this directory alone fail to compile. That line comes out
  when core is published (M18).
- **The list does not refresh while it is open.** One cold read at startup, and
  a session that changes state while you are reading its preview keeps the row
  it had. The preview under the cursor is re-read per row, so it is current; the
  row above it may not be.
- **`install` files its table and prints the keybinding** (D-85), so one
  manual paste stays, into a KDL file in a language a TOML writer cannot edit.
- **zellij is the only multiplexer with a printed keybinding.** The tmux and
  shell-widget snippets above work, and they are not generated by `install`.
- **The glyphs assume a Nerd Font**, and there is no ASCII fallback. A terminal
  without one draws boxes in the badge column; every badge still carries its
  word, so nothing becomes unreadable.
- **The container side is somebody else's.** Enter is only as good as the
  container integrations installed beside it, and a `focus-session` that cannot
  find your window fails in the container's words, not this program's.
