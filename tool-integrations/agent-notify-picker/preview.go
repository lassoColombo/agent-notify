package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"

	"github.com/lassoColombo/agent-notify/session"
)

// previewOf is this program run again by fzf, once per row somebody looks at,
// with the key of that row.
//
// Reading the store per preview rather than holding it is the right trade: a
// few milliseconds against a pane that is correct when it is looked at — and
// it is the only way the pane can show history, which is read on demand, for
// one session, and never delivered with a delta (§A7.7).
func previewOf(arguments []string) int {
	return oneSession(arguments, func(record session.Record) string {
		history, err := me.History(record.Key)
		if err != nil {
			history = session.History{}
		}
		return Preview(record, history, now(), previewWidth())
	})
}

// labelOf is the same program run a third time, by the `focus` binding, for the
// preview pane's border label.
//
// The label is where this session's IDENTITY lives: its name, the canonical
// (kernel, detail) string in full, and how long it has been that way. It is
// chrome rather than content, which is exactly right for it — those three facts
// used to be the pane's first line, two rows under the row that already said
// them, and a reader who has just moved the cursor does not need to be told
// twice where the cursor is.
func labelOf(arguments []string) int {
	return oneSession(arguments, func(record session.Record) string {
		return " " + Label(record, now()) + " "
	})
}

// oneSession finds the record a key names and prints what is asked of it. A
// key that no longer names anything is a session that ended between the
// keypress and the read, which is ordinary and not an error.
func oneSession(arguments []string, render func(session.Record) string) int {
	if len(arguments) == 0 {
		return 2
	}
	key, err := session.ParseKey(arguments[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	sessions, err := me.ReadIncludingEnded()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for _, record := range sessions {
		if record.Key == key {
			fmt.Print(render(record))
			return 0
		}
	}
	fmt.Println(Rule.Render("that session is gone"))
	return 0
}

// previewWidth is what fzf says the pane is, which it puts in the environment
// of every preview it runs. The fallback is narrow on purpose: text that is
// too narrow is readable and text that is too wide is a staircase.
func previewWidth() int {
	if columns, err := strconv.Atoi(os.Getenv("FZF_PREVIEW_COLUMNS")); err == nil && columns > 20 {
		return columns
	}
	return 80
}

// Label is the preview pane's border label: who this is, what it is doing in
// core's own words, and for how long.
//
// It is chrome rather than content, and it holds identity alone. It used to
// carry a context percentage and an allowance percentage beside the three
// facts here, on the grounds that the facts block below can be off the bottom
// of a pane an eighty-line message has pushed down. Both numbers are gone from
// the record, and nothing in the chrome replaced them.
func Label(record session.Record, at time.Time) string {
	return strings.Join([]string{
		Text.Render(fit(record.DisplayName(), widestName)),
		StateStyle(record.Rank).Render(record.State()),
		Subtle.Render(session.Ago(record.Elapsed(at))),
	}, Rule.Render(" · "))
}

// Preview is everything the row has no room for, in the order somebody reads
// it: WHAT IT SAID first, because that is the thing being decided on; then what
// it is spending, as reference; then what happened before.
//
// The message leads. It used to be third, under a heading that repeated the
// highlighted row and a facts line that repeated the row's path and branch — so
// the first two lines of the pane were always things the reader had just read,
// and the sentence they had opened the picker for was below them. The identity
// those lines carried is now the pane's label, which says it once, in chrome.
//
// It is handed its history rather than fetching it, so that what it renders is
// a function of what it was given.
func Preview(
	record session.Record, history session.History, at time.Time, width int,
) string {
	var parts []string

	if message := strings.TrimSpace(record.Message); message != "" {
		parts = append(parts, Markdowned(message, width))
	}
	if facts := Facts(record); len(facts) > 0 {
		parts = append(parts, "", rule(width), FactBlock(facts, width))
	}
	if lines := Earlier(record, history, at, width); len(lines) > 0 {
		parts = append(parts, "", rule(width),
			Subtle.Render("earlier")+"\n"+strings.Join(lines, "\n"))
	}
	if len(parts) == 0 {
		parts = append(parts, Rule.Render("nothing said yet"))
	}
	return strings.Join(parts, "\n") + "\n"
}

// rule is the line between one part of the pane and the next. base03, which is
// what a rule is drawn in: the base02 this used to be is a background slot and
// renders at 1.16:1 against the canvas, which is not a dim line but no line.
// One column short of the pane, because the last one belongs to the scrollbar.
func rule(width int) string {
	return Rule.Render(strings.Repeat("─", max(width-1, 1)))
}

// cut trims a rendered line to the pane, ANSI and all. The last column is the
// scrollbar's.
func cut(line string, width int) string {
	return lipgloss.NewStyle().MaxWidth(max(width-1, 1)).Render(line)
}

// Fact is one row of the metadata block: a name in the margin and a value
// beside it.
type Fact struct{ Key, Value string }

// factColumn is the width of the margin the names sit in, and it is shared
// with the timeline below so that the two blocks line up as one column.
const factColumn = 9

// FactBlock lays the facts out as a two-column block rather than as a line of
// values separated by middots, so that a name in the margin says what the
// value beside it is without a label being spent on it.
func FactBlock(facts []Fact, width int) string {
	lines := make([]string, 0, len(facts))
	for _, fact := range facts {
		lines = append(lines, cut(
			Subtle.Width(factColumn).Render(fact.Key)+fact.Value, width))
	}
	return strings.Join(lines, "\n")
}

// Facts is the metadata block: what this session is working on, with what, and
// what it has spent doing it.
//
// Every part disappears when there is nothing to say, and that is not
// tidiness: a zero token count almost always means an agent-integration that
// does not report usage, and printing "0 tokens" would state as a fact
// something nobody measured (§A7.4.3).
func Facts(record session.Record) []Fact {
	var facts []Fact

	if Home(record.Cwd) != "" {
		facts = append(facts, Fact{"where", Where(record)})
	}

	// One line for the things that are merely true of the process, because
	// none of them is read at a glance and each would waste a row alone.
	var about []string
	if record.Model != "" {
		about = append(about, Text.Render(record.Model))
	}
	if total := record.Usage.Total(); total > 0 {
		about = append(about, Subtle.Render(Tokens(total)+" tokens"))
	}
	if record.Process.PID != 0 {
		about = append(about, Rule.Render("pid "+strconv.Itoa(record.Process.PID)))
	}
	if len(about) > 0 {
		facts = append(facts, Fact{"running", strings.Join(about, Rule.Render("  ·  "))})
	}
	return facts
}

// Markdowned renders what the agent said the way it wrote it.
//
// An agent's message is markdown — headings, lists, fenced code, tables — and
// a picker that prints it raw shows somebody a wall of asterisks and backticks
// at the moment they are deciding which of four agents to answer. glamour is
// the renderer; the style beside this file is this machine's palette, and it
// states every decision rather than inheriting glamour's dark one, which is
// how a rendered heading once still had "## " in front of it.
//
// A message that cannot be rendered is printed as it arrived: the point is
// reading what the agent said, and a renderer that fails should not eat it.
func Markdowned(message string, width int) string {
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStylesFromJSONBytes(stylesheet()),
		glamour.WithWordWrap(width),
		glamour.WithEmoji(),
	)
	if err != nil {
		return message + "\n"
	}
	rendered, err := renderer.Render(message)
	if err != nil {
		return message + "\n"
	}
	// glamour pads a document with a blank line at each end; a pane this size
	// has better uses for two rows.
	return strings.Trim(rendered, "\n")
}

// Earlier is the session's own history, newest first: what it said before the
// thing it is saying now, and when its state last moved.
//
// Both halves are bounded by count where they are written (§A7.7), so this
// prints what it is given and never decides how much to keep.
func Earlier(
	record session.Record, history session.History, at time.Time, width int,
) []string {
	type moment struct {
		when time.Time
		text string
	}
	var moments []moment

	for _, change := range history.Changes {
		from := string(change.From)
		if from == "" {
			from = "new"
		}
		moments = append(moments, moment{
			change.At,
			StateStyle(change.From.Rank()).Render(from) + Rule.Render(" → ") +
				StateStyle(change.To.Rank()).Render(string(change.To)),
		})
	}
	for _, said := range history.Messages {
		// The message already at the top of the pane is not news down here.
		if strings.TrimSpace(said.Message) == strings.TrimSpace(record.Message) {
			continue
		}
		moments = append(moments, moment{
			said.At,
			Subtle.Render(strings.Join(strings.Fields(said.Message), " ")),
		})
	}

	// Newest first, and stable with it, so that a state change and the message
	// written beside it keep the order they were recorded in.
	for i := 1; i < len(moments); i++ {
		for j := i; j > 0 && moments[j].when.After(moments[j-1].when); j-- {
			moments[j], moments[j-1] = moments[j-1], moments[j]
		}
	}

	lines := make([]string, 0, len(moments))
	for _, one := range moments {
		ago := "now"
		if !one.when.IsZero() && at.After(one.when) {
			ago = session.Ago(at.Sub(one.when))
		}
		// One line each, cut rather than wrapped. This is a timeline, not a
		// transcript: an agent's last-but-one message can be four hundred
		// words, and wrapping it buries the state changes it sits between.
		lines = append(lines,
			cut(Rule.Width(factColumn-2).Align(lipgloss.Right).Render(ago)+"  "+one.text, width))
	}
	return lines
}
