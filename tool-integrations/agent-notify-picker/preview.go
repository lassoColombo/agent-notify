package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"charm.land/glamour/v2"

	"github.com/lassoColombo/agent-notify/session"
)

// previewOf is this program run again by fzf, once per row somebody looks at,
// with the key of that row.
//
// Reading the store per preview rather than holding it is the right trade: a
// few milliseconds against a pane that is correct when it is looked at. A key
// that no longer names anything is a session that ended between the keypress
// and the read, which is ordinary and not an error.
func previewOf(arguments []string) int {
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
			fmt.Print(Preview(record, previewWidth()))
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

// Preview is what the agent said, and nothing else (D-133).
//
// Who it is, its state and its wait are the row beside it. Where it lives,
// its model, what it has spent and its history were here once, under the
// message, and were cut: the picker is for choosing an agent and going to it,
// and none of them is what that choice is made on.
func Preview(record session.Record, width int) string {
	if message := strings.TrimSpace(record.Message); message != "" {
		// One column short of the pane, because the last one is the scrollbar's.
		return Markdowned(message, max(width-1, 1)) + "\n"
	}
	return Rule.Render("nothing said yet") + "\n"
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
