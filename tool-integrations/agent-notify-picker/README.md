# agent-notify-picker

Every session [agent-notify](../../agent-notify) knows about, most urgent first,
with what the highlighted one is working on and last said underneath it. Type to
filter, press enter, and you are in that agent's pane.

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

## It is a display that is not a daemon

It reads through the SDK, declares itself in the config, and renders the answer
core computed — like the bar and the pane titles. What differs is that it needs
a terminal, so it is run when you press a key and it goes away when you have
chosen.

That is what `enabled = false` in its table means. The session-watcher starts
every integration that is enabled and has a binary (§A9.4), which is right for a
bar and wrong for this: a picker started behind your back is a process with
nowhere to draw. The table still names the binary, for whatever binds the key
and for `doctor`.

## fzf owns the screen, as a library

This program is about a thousand lines and none of them draw anything. It builds
the rows, hands them to [fzf](https://github.com/junegunn/fzf) through a channel
— the same fzf, imported, not shelled out to — and gets back the one that was
chosen.

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
  three sessions do not sit in a third of a tall pane with air under them and
  twelve do not scroll while the preview below has rows to spare. The list is
  capped at two fifths, because the message is what the window is for. A pane
  too short to divide falls back to the fraction.
- **`--highlight-line`, and a solid pointer.** Every row arrives pre-coloured
  under `--ansi`, so fzf's `fg+` and `hl+` have nothing left to repaint: the
  selected row has to be lifted by its background across the whole width, and
  marked by `▌` rather than by a glyph a font may not have. This is the one
  place the skim mapping this palette came from does not transfer — skim's rows
  are uncoloured paths, so there `fg+` does the work.
- **No header and no footer.** Both were tried and both were worse. A header
  saying how many sessions there are spends the brightest row on the screen on a
  number fzf already prints beside the prompt; a footer is where fzf draws a
  legend *below* the list and *above* a `down` preview, which splits the window
  at the exact point the eye moves from one to the other. The empty query's
  ghost text carries the only thing left — whether this run included ended
  sessions. The one thing that does get a header is a complaint about the config
  file, which is worth a row precisely because it is temporary: it is gone the
  moment the file is fixed.
- **`bg-transform-preview-label` on `focus`**, which is what lets the pane's
  identity live in its border rather than in its first two lines.

## Keys

Typing filters; there is no key to press first. The preview is what needs
scrolling — the list is three rows and the message under it can be a hundred —
so the four keys nearest the home row scroll it:

| what it does | the action | keys |
| --- | --- | --- |
| filter | — | type |
| move through the list | `down` `up` | ↑ ↓, ctrl-n / ctrl-p |
| scroll the message, a line | `preview-down` `preview-up` | ctrl-j / ctrl-k |
| scroll the message, half a page | `preview-page-down` `preview-page-up` | ctrl-d / ctrl-u |
| go to that session | `choose` | enter |
| leave | `leave` | esc, ctrl-c |

Those four are fzf's defaults for something else, and each thing they displace
has another way: ctrl-j/k also move the list (↑↓ and ctrl-n/p still do), ctrl-u
clears the query (backspace still does), and ctrl-d quits on an empty query
(esc and ctrl-c still do). Scrolling the preview had no other way at all.

The middle column is a table in the config file, and it is the colours table the
other way round: a hue is yours and where it is spent is this program's, while a
**key is yours and what there is to ask for is this program's**.

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

- **Keyed on the action, never on the key.** The eight are declared, so a
  misspelled `preivew-down` is refused **by name**; a table keyed on keys is an
  open namespace where the same typo binds nothing and says nothing. It is also
  what keeps `toggle-sort` out: fzf's `--bind` grammar passed through would let
  you re-sort the list by match score, and narrowing without reordering is the
  one rule this picker keeps.
- **A value replaces the default rather than adding to it**, and `""` or `[]`
  gives the key back. Only what *this program* binds is yours to move: fzf's own
  defaults stand underneath, so a `preview-page-down = []` is ctrl-d quitting on
  an empty query again.
- **A key asked to do two things refuses the whole table**, with the two actions
  named. Half a keymap — the half that did not collide — is a window where some
  keys do what you asked and some do what this program asked, which is worse to
  sit in front of than either.
- **fzf checks the key names, not this program.** `ctrl-j`, `alt-up`, `f13` are
  fzf's vocabulary, and a second copy of it here would be one that drifts from
  the library this is built against. The binds go over as you wrote them and
  fzf's answer is the validation: a table it will not take is dropped for the
  keys this program shipped with, and the window opens with the complaint in its
  header. Nobody presses a key and gets a pane that appears and vanishes.

Typing is not in the table. There is no key to press before you filter, which is
the whole shape of the thing.

## The preview is this program, run again

`--preview` runs `agent-notify-picker --preview <key>` and `focus` runs
`agent-notify-picker --label <key>`, which is how every fzf preview works and is what makes the pane a renderer rather than a string
squeezed through a callback. It reads one session and shows:

- **What it is working on**: the directory, and the branch checked out there —
  which core reads on the hook path rather than taking the agent's word for it
  (§A7.4.4).
- **What it is spending**: the model, the tokens and the pid, as a two-column
  block rather than a line of values between middots, so that a name in the
  margin says what the value beside it is. Each part disappears when there is
  nothing to say, because a zero token count almost always means an adapter that
  reports no usage rather than a session that has spent nothing.

  There were two meters here — how full the context window was, and the tightest
  of the account's allowances with how long it had left. Both left the record in
  D-76, because only one of the two agents could fill either, so both bars drew
  for codex and never for Claude.
- **What it said, as markdown.** [glamour](https://charm.land) renders it:
  headings, lists, quotes, fenced code and tables come out as headings, lists,
  quotes, code and tables. A picker that printed the source would show a wall of
  asterisks at the moment somebody is deciding which of four agents to answer.

  The style beside this file states every decision rather than recolouring
  glamour's dark one, which is how a rendered h2 once still had `## ` in front
  of it — and how a fenced block with no language once came out as solid red
  bars across the pane, because glamour guesses a language, chroma marks what it
  then cannot lex as an error token, and the style painted error tokens on a red
  background: headings are a bar and a colour, a quote is a bar in the margin, inline
  code is gold on a raised surface, and the document has no margin because the
  pane is already inset and two more columns cost a word a line.
- **Who it is — in the border, not in the body.** The name, core's canonical
  `(kernel, detail)` string and the age are the pane's *label*, drawn by a
  second re-run of this program on fzf's `focus` event. They used to be the
  pane's first line, two rows under the row that already said them. The two
  percentages rode there too, for a good reason — the facts block can be off the
  bottom of a pane when a message runs to eighty lines — and they went with the
  meters they were the digits of.
- **What it said before**, from the session's own history — read on demand, for
  one session, because history never arrives with a delta (§A7.7).

## Rosé Pine, one hue one meaning

The palette is this machine's own. What this file is careful about is not which
hues it spends but how many meanings each one carries, because a reader cannot
hold three meanings for one colour in the two seconds this window is open — and
an earlier version of this program asked them to. Gold was `broke` in the list,
tokens in the facts, inline code in the message and the header across the top.
Foam was `finished-a-turn` and also the model's name. Iris was paths and also
every markdown heading. So:

- **The four state hues — love, gold, foam, bright pine — appear on the state
  glyph and on the state's detail, and nowhere else.** Not on the name, not on a
  heading.
- **Identity and prose are the neutral ramp, always.** A session's name is
  `base05` whatever it is doing, so that a name can be learned; a row painted end
  to end in its state's hue makes identity a moving target, and four such rows
  read as four kinds of object rather than four instances of one.
- **Iris is paths and chrome, rose is branches and matched characters**, and
  neither is ever a state. The pair is the one the shell prompt already writes a
  path and a branch in.

Two of the greys were measured rather than chosen, and both were wrong.
`base02` is a *background* slot — 1.16:1 against this canvas — and every rule,
border, separator and scrollbar was drawn in it, which is not a dim line but no
line at all; they are `base03` now, which is what `scheme.yaml` says `base03` is
for. And `working` was pine at 3.38:1 while `ended` was muted at 3.42:1 — the
two commonest rows in a real list, the same weight, told apart by hue alone at a
contrast where hue barely resolves. Working is **bright pine** (`base14`,
6.03:1), which keeps the family and can be seen.

Each row opens with the state's **glyph and its word** — the glyph is the same
Font Awesome mark the bar and the zellij tab titles wear, and the word is core's
own, shortened only where core's own is a sentence: `blocked/permission-prompt`
rather than `blocked-on-you/permission-prompt`. The two together are a badge,
and the badge is the only coloured thing on the row.

A glyph-only column was tried first, with a legend above the list naming the six
shapes. It does not work, and a screenshot is what settled it: the legend read
as a sixth row of the list, and three identical speech bubbles in a list of five
still had to be looked up against it. A shape resolves faster than a word, which
is why the glyph is there; a word needs no key, which is why it is there too.
Core's canonical string is still spelled out in full, once, in the label of the
pane for the row you are on.

The three narrow columns are **adjacent on the left** — glyph, state, age —
because they are one question. The age spent a version on the far side of the
name, where it floated in the middle of the row with whitespace on both sides of
it and belonged to neither neighbour.

A path is **dimmed down to its leaf** and elided from the left when it is long,
a whole component at a time: `…/modules/nu-http-client-generator`. Five
components in one colour is the longest and loudest thing on a row, competing
with the name for a glance it does not deserve, and only the last of them tells
two sessions apart. Cutting the tail instead would take the repository's name
off, which is the one part that identifies it.

The meters in the preview were the same idea applied to a number, and a calm one
was **grey**: a context window at 92% and one at 12% are different news, and only
the first of them should have been competing with the glyphs. Both meters are
gone (D-76); the rule survives them, and the next thing here that is a fraction
is drawn by it.

The background is left alone. This opens in a floating pane over your work, and
a picker that repaints the canvas reads as a different application rather than
as a layer on top of one.

## The hues are yours; where they go is not

The twelve slots this program paints with are a table in agent-notify's config
file. Nothing else about the palette is configurable, and the asymmetry is the
whole design: you say what a slot **is**, `theme.go` says what a slot is **for**.

```toml
[integration.picker.settings.colors]
base02 = "#26233a"  # overlay     — the row you are on, the chip behind inline code
base03 = "#6e6a86"  # muted       — rules, borders, scrollbars, the marks between words
base04 = "#8c88a6"  # subtle      — ages, fact names, earlier messages, unmatched rows
base05 = "#e0def4"  # text        — names and prose
base08 = "#eb6f92"  # love        — blocked-on-you
base09 = "#f6c177"  # gold        — broke, and a string in a fenced block
base0A = "#ebbcba"  # rose        — branches, matched characters
base0B = "#31748f"  # pine        — a keyword, inside a fenced code block and nowhere else
base0C = "#9ccfd8"  # foam        — finished-a-turn
base0D = "#c4a7e7"  # iris        — paths, the prompt, the pane label
base13 = "#fff0ee"  # bright rose — the pointer rail, headings in a message
base14 = "#609fbb"  # bright pine — working
```

Write one line or none: an absent key keeps the default, which is the Rosé Pine
above. The comments are also the list of what **moves together** — change
`base09` and you change `broke` in the list and a string inside a fenced block,
in one line. That is what the short table costs,
and it is only safe because the section above it is careful about which things
share a slot in the first place.

The slots are named rather than hue-worded — no `blue`, no `red` — because the
hue words lie on this machine: the "blue" slot is iris, which is purple, and
`base0A` is annotated ANSI yellow while being rose. A slot name is what
`scheme.yaml` is written in, so this table is a paste from the file you already
keep.

Twelve, because twelve is what gets painted. A slot nothing spends is not
offered — a key that changes nothing is the silence the strictness is there to
break, and a key nobody declared is refused **by name**. `base00` is absent for
a different reason: the canvas is never painted at all.

The embedded glamour style is written in those same slot names and resolved
before the renderer sees it. That is what makes one table cover the whole
window rather than only the rows: move `base0D` and the links in a message move
with the paths in a row. A window half in your palette and half in mine would
be worse than one wholly in mine.

A value that is not `#rrggbb` keeps its default, and the window opens with the
complaint in its header — the first one, and how many others there were. The
alternative was refusing to start, and `close_on_exit true` makes that a
floating pane which appears and vanishes: indistinguishable from a crash, and
the thing you would file a bug about rather than fix.

## Installing

```
agent-notify install picker            # appends to agent-notify's config
agent-notify install picker --print    # show what it would add
```

```toml
[integration.picker]
binary  = "/opt/homebrew/bin/agent-notify-picker"
enabled = false
```

Install also prints the zellij keybinding to add, and does not write it: a
zellij config is KDL, full of your own comments and your own key choices, and an
integration that rewrote it would be guessing at both.

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

**Installing the binary: replace it, never overwrite it.** `cp` over a running
path reuses the inode, and macOS then kills the new binary on exec with SIGKILL
— exit 137, no output, no message — because the signature it cached no longer
matches what is there. The symptom is a keybinding that opens a pane which
closes instantly, which looks exactly like a program that crashed on startup.

```
rm -f /opt/homebrew/bin/agent-notify-picker
install -m 755 agent-notify-picker /opt/homebrew/bin/
```

`close_on_exit true` is what makes enter feel like a jump: focusing a pane hides
the floating layer by itself, and this exits a moment later, so the pane it was
in goes away behind you.

```
agent-notify-picker            the live sessions
agent-notify-picker --all      the ended-but-resumable ones too — the set you
                               can still resume (§A7.5)
agent-notify-picker --print    answer with the session's key instead of going
                               to it, for binding it to something of your own
```

## What it reads, and what it runs

Nothing here walks agent-notify's directory layout or drives a container:

- `me.Read` (or `ReadIncludingEnded`) for the sessions, with the liveness
  decision already applied — a session whose process was killed is not offered
  as working (D-30).
- `me.History` for the preview of the one under the cursor.
- `agent-notify focus-session` for the jump. Which containers there are, what
  order they nest in and what each one answers are core's business (§A11.2) — a
  display that learned any of that would be one that disagrees with the bar
  beside it. Its output is left on the terminal, so a jump that fails says why,
  in the words the container that refused it wrote.

## Testing

```
env -u GOROOT go test ./...
```

What is tested is what this program decides — what a row says, what the preview
says, and what fzf is asked for — because the screen itself is fzf's and a test
of it would be a test of somebody else's library. The cheapest one pays the
most: it hands the option list to fzf's own parser, so a mistyped colour key or
a preview-window spec fzf will not accept is a failing test rather than a picker
that refuses to open on the one keypress somebody makes.

## Building

Go 1.26, [fzf](https://github.com/junegunn/fzf) as a library, and two of Charm's
for what is left: [glamour](https://charm.land/glamour) renders the markdown and
[lipgloss](https://charm.land/lipgloss) does the styling and the columns. The
first version of this file's neighbours built SGR escapes by hand and padded
with `strings.Repeat` over runes, which is two width models in one program —
mine and the one fzf lays out with. lipgloss measures the way a terminal does,
and it was already here underneath glamour. Core is not published
yet, so `go.mod` has a `replace` pointing beside this repository; that line comes
out when core is.
