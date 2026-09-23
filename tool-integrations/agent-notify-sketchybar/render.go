package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	agentnotify "github.com/lassoColombo/agent-notify"
)

// This file is pure. It turns the sessions into one sketchybar command line and
// returns it as data; nothing here runs anything.
//
// One invocation per render, and that is not a micro-optimisation: measured at
// 5-8ms for a hundred arguments, against about 4ms of process start for each
// one you split it into. It also makes a render atomic from a person's point of
// view — the bar never shows half of a change.

// Bar is everything the renderer needs that is not the sessions.
type Bar struct {
	Prefix   string
	Position string
	Rows     int
	// Sketchybar is the absolute path the hover and click scripts run, which is
	// not necessarily the one on PATH: those scripts are run by sketchybar
	// itself, from whatever environment it happened to be started in.
	Sketchybar string
	// Core is the agent-notify a click runs to focus a session.
	Core    string
	Glyphs  agentnotify.Palette
	Colours agentnotify.Palette
	Now     time.Time

	// Announce is how long a state change stays lit. Zero leaves the bar
	// passive: counters that only count.
	Announce time.Duration
	// Preview is how much of an agent's last message a hover shows.
	Preview Preview

	// Popup is how the chip behind a counter is drawn. It has to be drawn at
	// all: a popup with no background of its own is transparent, and what shows
	// through is whatever the window underneath happens to be — a terminal full
	// of text, in the case that found this. The rows were rendered perfectly
	// and were unreadable.
	Popup Popup

	// Before and After put these items somewhere specific in the bar, named by
	// an item somebody else made.
	//
	// They are needed because sketchybar orders a side by the order things were
	// added to it, and this display is not there when your sketchybarrc runs —
	// the session-watcher starts it afterwards, so everything it owns lands at
	// the far end of whichever side it is on. Naming a neighbour is the only
	// way to say "here" rather than "last".
	Before string
	After  string
}

// Popup is the look of one chip.
//
// Three values rather than sketchybar's twenty, because the rest are either
// decided by the content (height, width) or things nobody changes. An empty
// Border means no border rather than a black one.
type Popup struct {
	Background string
	Border     string
	Radius     int
}

// Render is the whole display: what the bar should say, as sketchybar's own
// arguments.
//
// Every render is IDEMPOTENT and complete: it names every item it owns, adds it
// whether or not it exists, and sets everything about it. That is possible
// because of how sketchybar behaves when asked for something that is not there,
// which was worth measuring rather than assuming — `--add` on an existing item
// warns and carries on, `--set` on a missing one warns and carries on, and both
// exit 0. So a display that says everything every time needs no bookkeeping and
// heals itself the moment somebody reloads their bar out from under it.
func Render(bar Bar, sessions []agentnotify.Record, was agentnotify.Arrival, seen bool) ([]string, agentnotify.Arrival) {
	byKernel := map[agentnotify.Kernel][]agentnotify.Record{}
	for _, record := range sessions {
		if record.Kernel == agentnotify.Ended {
			continue
		}
		byKernel[record.Kernel] = append(byKernel[record.Kernel], record)
	}
	// Sorted HERE rather than inside each counter, because an announcement
	// names a row and the row it names has to be the row that gets drawn.
	for _, here := range byKernel {
		agentnotify.ByUrgency(here)
	}
	now := agentnotify.JustArrived(sessions, bar.Now, bar.Announce)

	var arguments []string
	for _, kernel := range Painted() {
		arguments = append(arguments, bar.counter(kernel, byKernel[kernel], now)...)
	}
	arguments = append(arguments, bar.announcing(now, was, seen, byKernel)...)
	return arguments, now
}

// Scaffold is every item this display owns, its wiring, and the scripts that do
// not depend on what is happening: the counters, the rows, the preview under
// them, the two hidden items that hold a scroll's position and a pointer's
// mark, and the event a wheel is forwarded to.
//
// It is SEPARATE FROM THE PAINT and sent far less often, which is a change M14
// did not need and this one does. A counter and eight rows is forty-odd items
// and they can be re-declared on every paint for nothing; a preview forty lines
// deep behind five counters is two hundred more, and `--add`-ing all of them
// several times a second is a thousand arguments an agent event. Measured: the
// bar stopped answering a --query underneath it.
//
// Nothing here is ever WRONG, only sometimes redundant — `--add` on an item
// that exists is informational (measured, `[?]`) — so the display re-sends it
// whenever the bar says it has lost something, and the structure repairs
// itself without anybody restarting anything.
func Scaffold(bar Bar) []string {
	arguments := bar.closer()
	arguments = append(arguments, bar.wheel()...)
	for _, kernel := range Painted() {
		name := bar.item(kernel)
		arguments = append(arguments,
			"--add", "item", name, bar.Position,
			"--set", name, "script="+bar.open(kernel), "popup.align=left")
		arguments = append(arguments, bar.Popup.on()...)
		arguments = append(arguments, "--subscribe", name, "mouse.entered")
		for row := range bar.Rows {
			slot := bar.rowName(kernel, row)
			arguments = append(arguments,
				"--add", "item", slot, "popup."+name,
				"--subscribe", slot, "mouse.entered", "mouse.scrolled")
		}
		arguments = append(arguments, bar.slots(kernel)...)
	}
	return append(arguments, bar.placed()...)
}

// announcing is everything true of an announcement and of nothing else on the
// bar: the chip opens, the preview fills with what the agent said, and the
// whole lot is put away again when it is over.
//
// The OPENING and the CLOSING are edges, and they are the only edges in this
// display. Everything else is level-triggered and complete — say what is true,
// every time, so that a bar which lost its mind recovers on the next paint
// (R22). A popup cannot be painted that way: setting `popup.drawing` on every
// paint would shut a chip somebody had opened with their pointer, one agent
// event later. So this compares with the previous paint and touches a popup
// only where the answer has actually changed.
//
// SEEN is why closing takes an argument. A pointer that has arrived owns the
// chip from that moment, and nothing here may shut one under somebody who came
// to read it — so the hover scripts leave a mark, the display reads it, and an
// announcement that was looked at expires without taking the chip away. What
// closes it then is the pointer leaving the bar, which closes every other chip
// too.
func (b Bar) announcing(now, was agentnotify.Arrival, seen bool, byKernel map[agentnotify.Kernel][]agentnotify.Record) []string {
	if now.Same(was) {
		return nil
	}
	var arguments []string
	if was.Announced() && (!now.Announced() || now.Record.Kernel != was.Record.Kernel) && !seen {
		arguments = append(arguments, "--set", b.item(was.Record.Kernel), "popup.drawing=off")
	}
	if !now.Announced() {
		return arguments
	}

	// Opened on the agent's own words with nobody's pointer in it — so the
	// preview goes as plain argv here, where a hover has to bake it into a
	// shell command. Same slots, same decision, two spellings of it.
	arguments = append(arguments, "--set", b.item(now.Record.Kernel), "popup.drawing=on")
	for _, kernel := range Painted() {
		if kernel != now.Record.Kernel {
			arguments = append(arguments, "--set", b.item(kernel), "popup.drawing=off")
		}
	}
	// The record itself, rather than a row number to look up in the chip. An
	// announcement used to carry the index it sat at and the preview was
	// filled from `here[row]`, which was two ways of naming one session and a
	// bounds check to go with them.
	arguments = append(arguments, b.fill(now.Record.Kernel, now.Record)...)
	// Reset rather than cleared: a hover during THIS announcement has to be
	// distinguishable from one during the last.
	return append(arguments, "--set", b.seen(), "icon="+now.Record.Key.String())
}

// placed moves the counters next to an item somebody else made.
//
// The first one is put where it was asked for and the rest follow it, so the
// group stays in rank order however many times this runs. Repeating it on every
// render is deliberate: a `--move` to where something already is costs nothing,
// and the alternative is remembering whether this display has ever been placed,
// which is state that would be wrong the first time sketchybar restarts.
//
// A reference item that does not exist is a complaint from sketchybar and
// nothing more — the render still lands, in the place it would have anyway.
func (b Bar) placed() []string {
	var moves []string
	switch {
	case b.Before != "":
		moves = []string{"--move", b.item(Painted()[0]), "before", b.Before}
	case b.After != "":
		moves = []string{"--move", b.item(Painted()[0]), "after", b.After}
	default:
		return nil
	}
	painted := Painted()
	for i := 1; i < len(painted); i++ {
		moves = append(moves, "--move", b.item(painted[i]), "after", b.item(painted[i-1]))
	}
	return moves
}

// closer is a hidden item whose only job is to put the popups away again.
//
// Hover opens a popup and there is no "the mouse left the bar entirely" event
// on an item — only `mouse.exited.global`, which any item can hear. So one
// invisible item hears it and closes everything. Without it a chip opened by a
// glance stays open until something else opens.
func (b Bar) closer() []string {
	name := b.Prefix + ".away"
	return []string{
		"--add", "item", name, b.Position,
		"--set", name, "drawing=off", "updates=on", "script=" + b.closeEverything(),
		"--subscribe", name, "mouse.exited.global",
	}
}

func (b Bar) closeEverything() string {
	var shut strings.Builder
	shut.WriteString(quote(b.Sketchybar))
	for _, kernel := range Painted() {
		shut.WriteString(" --set " + quote(b.item(kernel)) + " popup.drawing=off")
	}
	return shut.String()
}

// counter is one state's item and the chip behind it.
func (b Bar) counter(kernel agentnotify.Kernel, here []agentnotify.Record, now agentnotify.Arrival) []string {
	name := b.item(kernel)

	drawing := "on"
	if len(here) == 0 {
		// Nothing in this state, so nothing on the bar. Its position is still
		// reserved, which is what keeps the counters from shuffling about as
		// the day goes on — a bar whose items move is a bar nobody can click.
		drawing = "off"
	}

	sample := agentnotify.Record{Kernel: kernel, Rank: kernel.Rank()}
	arguments := []string{
		"--set", name,
		"icon=" + b.Glyphs.For(sample),
		"icon.color=" + b.Colours.For(sample),
		"label=" + strconv.Itoa(len(here)),
		"label.color=" + b.Colours.For(sample),
		"drawing=" + drawing,
	}
	arguments = append(arguments, b.lit(kernel, now.Announced() && now.Record.Kernel == kernel)...)
	if len(here) > 0 {
		// Clicking the counter itself goes to the one that matters most in it,
		// which is what you want nine times in ten and saves the hover.
		arguments = append(arguments, "--set", name, "click_script="+b.focus(here[0]))
	}

	for row := range b.Rows {
		arguments = append(arguments, b.row(kernel, row, here, now)...)
	}
	return arguments
}

// lit is the counter's own highlight: the announcement, said in colour.
//
// The tint is worked out from the state's own colour rather than configured,
// because a highlight that did not match the glyph beside it would be a second
// vocabulary to learn. Alpha only — the hue is the state's.
func (b Bar) lit(kernel agentnotify.Kernel, on bool) []string {
	name := b.item(kernel)
	if !on {
		return []string{"--set", name, "background.drawing=off", "background.border_width=0"}
	}
	hue := b.Colours.For(agentnotify.Record{Kernel: kernel, Rank: kernel.Rank()})
	return []string{"--set", name,
		"background.drawing=on",
		"background.color=" + tint(hue, 0x33),
		"background.corner_radius=6",
		"background.height=22",
		"background.border_width=1",
		"background.border_color=" + tint(hue, 0xaa)}
}

// slots is the preview: a pool of lines under the rows, and the footer that
// says how much more there is.
//
// The paint only guarantees they EXIST and know how to pass a wheel on. It
// never writes their text and never draws them, and that is deliberate: the
// preview answers to the pointer, not to the state of the world, and a paint
// that reset it would blank what somebody was reading the moment any agent
// anywhere did anything.
func (b Bar) slots(kernel agentnotify.Kernel) []string {
	if !b.Preview.on() {
		return nil
	}
	var arguments []string
	for line := range b.Preview.Depth {
		name := b.slot(kernel, line)
		arguments = append(arguments,
			"--add", "item", name, "popup."+b.item(kernel),
			"--set", name, "icon.drawing=off", "label.padding_left=8",
			"script="+b.forward(),
			"--subscribe", name, "mouse.scrolled")
	}
	more := b.more(kernel)
	return append(arguments,
		"--add", "item", more, "popup."+b.item(kernel),
		"--set", more, "icon.drawing=off", "label.padding_left=8", "drawing=off",
		"label.color="+b.Colours.For(agentnotify.Record{Kernel: agentnotify.Idle, Rank: agentnotify.RankIdle}),
		"script="+b.forward(),
		"--subscribe", more, "mouse.scrolled")
}

// on is the chip's background, as arguments to the counter it hangs from.
//
// `popup.background.drawing=on` is stated rather than relied upon. Setting the
// colour turns it on by itself — measured — but that is a behaviour nobody
// wrote down, and the failure it would cause if it ever changed is the one this
// exists to fix.
func (p Popup) on() []string {
	if p.Background == "" {
		return nil
	}
	set := []string{
		"popup.background.drawing=on",
		"popup.background.color=" + p.Background,
		"popup.background.corner_radius=" + strconv.Itoa(p.Radius),
	}
	if p.Border == "" {
		return append(set, "popup.background.border_width=0")
	}
	return append(set,
		"popup.background.border_width=1",
		"popup.background.border_color="+p.Border)
}

// row is one line of a chip: a session, how long it has been like that, and a
// click that takes you there.
//
// The rows are a fixed pool that is drawn or not, rather than items added and
// removed as sessions come and go. Adding and removing would churn the popup's
// membership on every change, and a pool costs a few hidden items and nothing
// else.
func (b Bar) row(kernel agentnotify.Kernel, index int, here []agentnotify.Record, now agentnotify.Arrival) []string {
	name := b.rowName(kernel, index)
	var arguments []string

	last := index == b.Rows-1 && len(here) > b.Rows
	switch {
	case last:
		// The final row says how many did not fit rather than silently
		// dropping them. A count that is quietly wrong is worse than a count.
		arguments = append(arguments, "--set", name,
			"icon=", "label="+fmt.Sprintf("and %d more", len(here)-b.Rows+1),
			"label.color="+b.Colours.For(agentnotify.Record{Kernel: agentnotify.Idle, Rank: agentnotify.RankIdle}),
			"click_script=", "drawing=on")
	case index < len(here):
		record := here[index]
		arguments = append(arguments, "--set", name,
			"icon="+b.Glyphs.For(record),
			"icon.color="+b.Colours.For(record),
			"label="+b.line(record),
			"click_script="+b.focus(record),
			// Hovering a row fills the preview under it, and a wheel on a row
			// scrolls that preview — because the one you mean to scroll is the
			// one you are pointing at, not the one you walked past.
			"script="+b.hover(kernel, record),
			"drawing=on")
		hot := now.Announced() && now.Record.Key == here[index].Key
		arguments = append(arguments, b.marked(name, kernel, hot)...)
	default:
		arguments = append(arguments, "--set", name, "drawing=off")
	}
	return arguments
}

// marked is the announced row's own highlight, inside the chip.
//
// The counter says WHICH state just changed and this says WHICH agent, which
// are different questions the moment two sessions are in one state.
func (b Bar) marked(name string, kernel agentnotify.Kernel, on bool) []string {
	if !on {
		return []string{"--set", name, "background.drawing=off"}
	}
	hue := b.Colours.For(agentnotify.Record{Kernel: kernel, Rank: kernel.Rank()})
	return []string{"--set", name,
		"background.drawing=on",
		"background.color=" + tint(hue, 0x33),
		"background.corner_radius=4"}
}

// line is what one session reads as on a chip.
//
// The age is computed here and never stored, which is R26: a fact every
// subscriber derives identically from the record it already holds is rendering,
// not state. It is also why this display repaints on its own clock — nothing
// will tell it that four minutes have become five, because nothing changed.
func (b Bar) line(record agentnotify.Record) string {
	name := record.DisplayName()
	if detail := record.Detail; detail != "" {
		name += "  " + detail
	}
	return fmt.Sprintf("%-28s %4s", truncate(name, 28), agentnotify.Ago(record.Elapsed(b.Now)))
}

// open shows this chip and puts the others away, all in one invocation so that
// two chips are never both open for a frame.
//
// It also BLANKS this chip's preview, because the preview belongs to the last
// row somebody hovered and opening a chip from its counter hovers no row: what
// would otherwise be under the rows is whatever agent you last looked at, which
// is the most confusing thing a panel of text can be.
func (b Bar) open(kernel agentnotify.Kernel) string {
	var script strings.Builder
	script.WriteString(`[ "$SENDER" = mouse.entered ] || exit 0; exec `)
	script.WriteString(quote(b.Sketchybar))
	for _, other := range Painted() {
		state := "off"
		if other == kernel {
			state = "on"
		}
		script.WriteString(" --set " + quote(b.item(other)) + " popup.drawing=" + state)
	}
	// Named one at a time, and NOT with the regex sketchybar accepts in place of
	// an item name. The regex would need a backslash to mean a literal dot, and
	// a backslash in any property makes that item's own --query answer invalid
	// JSON — measured, and it cost a test that could not read back an item it
	// had just written. An unescaped dot works and matches one character too
	// many. This script goes with the structure rather than with every paint,
	// so forty arguments is a price nobody pays twice.
	for line := range b.Preview.Depth {
		script.WriteString(" --set " + quote(b.slot(kernel, line)) + " drawing=off")
	}
	script.WriteString(" --set " + quote(b.more(kernel)) + " drawing=off")
	script.WriteString(" --set " + quote(b.seen()) + " icon=seen")
	return script.String()
}

// hover is one row's preview, as the command that will show it.
//
// Built at PAINT time with the agent's words already wrapped, so the hover
// itself does no thinking and reads nothing: one sh and one client. The
// alternative — a script that goes and looks the session up — is a store read
// on the path of a pointer moving across a bar.
//
// The wheel is forwarded FIRST and the hover's own guard comes after it,
// because a row is also where a wheel lands.
func (b Bar) hover(kernel agentnotify.Kernel, record agentnotify.Record) string {
	if !b.Preview.on() {
		// Still forward the wheel: a bar with no preview has nothing to scroll,
		// and a script that says so is one fewer thing to reason about than a
		// row that behaves differently from its neighbours.
		return b.forward()
	}
	var script strings.Builder
	script.WriteString(b.forward())
	script.WriteString("\n" + `[ "$SENDER" = mouse.entered ] || exit 0` + "\n")
	script.WriteString("exec " + quote(b.Sketchybar))

	slots, held, rest := b.previewOf(kernel, record)
	for _, slot := range slots {
		if !slot.filled {
			script.WriteString(" --set " + quote(slot.name) + " drawing=off")
			continue
		}
		// The ONE place an agent's words meet a shell, and the only reason
		// quotable exists. Everything inside these quotes is literal (§A15).
		script.WriteString(" --set " + quote(slot.name) +
			" 'label=" + quotable(slot.label) + "'" +
			" label.color=" + slot.colour + " drawing=" + drawn(slot.on))
	}
	for _, argument := range b.tail(kernel, held, rest) {
		script.WriteString(" " + quote(argument))
	}
	return script.String() + " --set " + quote(b.seen()) + " icon=seen"
}

// fill is the same preview as plain argv, for the announcement — which opens a
// chip with nobody's pointer in it and therefore has no shell in the way. The
// agent's words go through exactly as written.
func (b Bar) fill(kernel agentnotify.Kernel, record agentnotify.Record) []string {
	if !b.Preview.on() {
		return nil
	}
	slots, held, rest := b.previewOf(kernel, record)
	var arguments []string
	for _, slot := range slots {
		if !slot.filled {
			arguments = append(arguments, "--set", slot.name, "drawing=off")
			continue
		}
		arguments = append(arguments, "--set", slot.name,
			"label="+slot.label, "label.color="+slot.colour, "drawing="+drawn(slot.on))
	}
	return append(arguments, b.tail(kernel, held, rest)...)
}

// tail is what a filled preview needs besides its words: where the window is,
// and how much is under it. NUMBERS AND ITEM NAMES ONLY — no agent text — which
// is what lets one builder serve the hover, the announcement and the wheel
// alike, and what keeps a scroll from ever having to quote anything.
func (b Bar) tail(kernel agentnotify.Kernel, held, rest int) []string {
	arguments := []string{"--set", b.scroll(),
		fmt.Sprintf("icon=pv:%s:%d:0", b.item(kernel), held)}
	if rest <= 0 {
		return append(arguments, "--set", b.more(kernel), "drawing=off")
	}
	return append(arguments, "--set", b.more(kernel),
		fmt.Sprintf("label=%d more lines", rest), "drawing=on")
}

// slotted is one line of the preview, decided once and rendered twice.
type slotted struct {
	name   string
	label  string
	colour string
	filled bool
	on     bool
}

// previewOf decides what every slot should say, and is the whole of the
// difference between what is WRITTEN and what is DRAWN.
//
// Every line the agent wrote is written; only the first screenful is drawn.
// Conflating the two is what makes a scroll reveal blank rows. Slot 0 is the
// session's name and is always drawn — it does not scroll, because the preview
// sits below every row of the chip and whose words these are would otherwise be
// a guess.
func (b Bar) previewOf(kernel agentnotify.Kernel, record agentnotify.Record) (slots []slotted, held, rest int) {
	lines := b.Preview.Of(record)
	window := b.Preview.window()

	for line := range b.Preview.Depth {
		slot := slotted{name: b.slot(kernel, line)}
		if line < len(lines) {
			slot.filled, slot.label, slot.on = true, lines[line], line <= window
			slot.colour = b.Preview.Text
			if line == 0 {
				slot.colour = b.Colours.For(record)
			}
		}
		slots = append(slots, slot)
	}
	held = min(len(lines)-1, b.Preview.Depth-1)
	return slots, held, held - window
}

// forward passes a wheel on to the one item that knows where the window is.
//
// Every row and every preview line carries this, because a mouse event reaches
// ONLY the item under the pointer and that item is a different one every time.
// The thinking lives once, on the item that holds the position.
func (b Bar) forward() string {
	return fmt.Sprintf(`[ "$SENDER" = mouse.scrolled ] && exec %s --trigger %s SCROLL_DELTA=$SCROLL_DELTA`,
		quote(b.Sketchybar), b.event())
}

// wheel is the hidden machinery of scrolling: the event a row forwards to, the
// item that holds the position, and the mark a pointer leaves behind.
func (b Bar) wheel() []string {
	return []string{
		"--add", "event", b.event(),
		"--add", "item", b.scroll(), b.Position,
		"--set", b.scroll(), "drawing=off", "updates=on", "script=" + b.scrolling(),
		"--subscribe", b.scroll(), b.event(),
		"--add", "item", b.seen(), b.Position,
		"--set", b.seen(), "drawing=off",
	}
}

// scrolling is the whole of a scroll, in one script written from settings and
// nothing else — there is nothing per-paint in it, because the paint has
// already put the text on the bar and this only decides which lines are drawn.
//
// It reads the position out of its own icon, works out the new top from the
// wheel's momentum, and sends ONLY THE SLOTS THAT CHANGE: two for a nudge, a
// handful for a shove, never the whole preview. Everything it computes is a
// number, so the message it builds cannot contain anything an agent wrote.
//
// The position lives in an item's icon because a script is given $SENDER,
// $NAME, $BUTTON and $SCROLL_DELTA and nothing else: a property is the only
// place one scroll can leave something for the next. Reading it back is one
// --query, measured at 4-12ms against a wheel the bar throttles to a few events
// a second.
func (b Bar) scrolling() string {
	sb := quote(b.Sketchybar)
	lines := []string{
		fmt.Sprintf(`[ "$SENDER" = %s ] || exit 0`, b.event()),
		fmt.Sprintf(`q=$(%s --query %s)`, sb, quote(b.scroll())),
		// The quote characters are themselves quoted, or the shell this is
		// handed to never finds the end of the string.
		`r=${q#*'"pv:'}`,
		`[ "$r" = "$q" ] && exit 0`,
		`r=${r%%'"'*}`,
		`t=${r%%:*}; r=${r#*:}; n=${r%%:*}; p=${r#*:}`,
		fmt.Sprintf(`w=%d`, b.Preview.window()),
		`[ "$n" -gt "$w" ] || exit 0`,
		`d=${SCROLL_DELTA:-0}`,
		`[ "$d" -eq 0 ] && exit 0`,
		`m=${d#-}`,
		// A nudge moves one line and a shove moves several: the bar reports
		// momentum, not notches, and a fixed step would make a long answer take
		// a minute to read at the few events a second a wheel is throttled to.
		fmt.Sprintf(`s=$((m / %d + 1))`, momentum),
		`case "$d" in -*) k=$((p+s)) ;; *) k=$((p-s)) ;; esac`,
		`x=$((n-w))`,
		`[ "$k" -lt 0 ] && k=0`,
		`[ "$k" -gt "$x" ] && k=$x`,
		`[ "$k" -eq "$p" ] && exit 0`,
		`a=""; i=1`,
		`while [ "$i" -le "$n" ]; do o=0; v=0;` +
			` if [ "$i" -gt "$p" ] && [ "$i" -le $((p+w)) ]; then o=1; fi;` +
			` if [ "$i" -gt "$k" ] && [ "$i" -le $((k+w)) ]; then v=1; fi;` +
			` if [ "$o" -ne "$v" ]; then` +
			` if [ "$v" -eq 1 ]; then a="$a --set $t.pv.$i drawing=on";` +
			` else a="$a --set $t.pv.$i drawing=off"; fi; fi;` +
			` i=$((i+1)); done`,
		`b=$((n-k-w)); c=on`,
		`[ "$b" -le 0 ] && c=off`,
		fmt.Sprintf(`exec %s $a --set $t.more label="$b more lines" drawing=$c --set %s icon=pv:$t:$n:$k`,
			sb, quote(b.scroll())),
	}
	return strings.Join(lines, "; ")
}

// momentum is how much wheel makes one line.
//
// The bar reports how hard the wheel was turned rather than how many notches:
// deltas arrive anywhere from single digits to the high hundreds, so this gives
// one line for a nudge and several for a shove.
const momentum = 40

func drawn(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// tint puts a new alpha on a 0xAARRGGBB colour, which is how a highlight is
// built out of the state's own hue rather than out of a second palette nobody
// asked to configure.
func tint(colour string, alpha int) string {
	if len(colour) != 10 || !strings.HasPrefix(colour, "0x") {
		return colour
	}
	return fmt.Sprintf("0x%02x%s", alpha, colour[4:])
}

// focus is what a click runs. The key is quoted because it is built from an
// agent's own session id, which is not ours to trust (§A15).
func (b Bar) focus(record agentnotify.Record) string {
	return fmt.Sprintf("%s --set %s popup.drawing=off; exec %s focus-session --quiet %s",
		quote(b.Sketchybar), quote(b.item(record.Kernel)), quote(b.Core), quote(record.Key.String()))
}

func (b Bar) item(kernel agentnotify.Kernel) string {
	return b.Prefix + "." + string(kernel)
}

func (b Bar) rowName(kernel agentnotify.Kernel, index int) string {
	return fmt.Sprintf("%s.row.%d", b.item(kernel), index)
}

func (b Bar) slot(kernel agentnotify.Kernel, line int) string {
	return fmt.Sprintf("%s.pv.%d", b.item(kernel), line)
}

func (b Bar) more(kernel agentnotify.Kernel) string { return b.item(kernel) + ".more" }

func (b Bar) scroll() string { return b.Prefix + ".scroll" }
func (b Bar) seen() string   { return b.Prefix + ".seen" }

// event is a name rather than a dotted one: this is an event, not an item, and
// the two namespaces are different.
func (b Bar) event() string { return strings.ReplaceAll(b.Prefix, ".", "_") + "_scrolled" }

func truncate(text string, width int) string {
	runes := []rune(text)
	if len(runes) <= width {
		return text
	}
	if width < 2 {
		return string(runes[:width])
	}
	return string(runes[:width-1]) + "…"
}

// quote wraps a string for the shell that sketchybar runs scripts through.
//
// Single quotes, with the one escape single quotes need, because everything
// interpolated here came from somewhere else: a session's name is whatever an
// agent was told to call itself, and a path is whatever a person configured.
func quote(text string) string {
	return "'" + strings.ReplaceAll(text, "'", `'\''`) + "'"
}

// Remove takes every item this display owns off the bar.
//
// It runs on the way out, and it matters more than it looks: a counter left
// behind says "two agents are working" for ever, and there is no way for a
// person to tell a stale number from a current one. An empty bar is honest; a
// frozen one is not.
func Remove(bar Bar) []string {
	arguments := []string{
		"--remove", bar.Prefix + ".away",
		"--remove", bar.scroll(),
		"--remove", bar.seen(),
	}
	for _, kernel := range Painted() {
		for row := range bar.Rows {
			arguments = append(arguments, "--remove", bar.rowName(kernel, row))
		}
		for line := range bar.Preview.Depth {
			arguments = append(arguments, "--remove", bar.slot(kernel, line))
		}
		arguments = append(arguments, "--remove", bar.more(kernel))
		arguments = append(arguments, "--remove", bar.item(kernel))
	}
	return arguments
}
