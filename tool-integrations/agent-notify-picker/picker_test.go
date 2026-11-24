package main

import (
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	fzf "github.com/junegunn/fzf/src"

	"github.com/lassoColombo/agent-notify/session"
)

// What is worth testing here is what this program decides, which is: what a row
// says, what the preview says, and what fzf is asked for. The screen itself is
// fzf's, and a test of it would be a test of somebody else's library.

var noon = time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

func aSession() session.Record {
	return session.Record{
		Key:        session.Key{Host: "here", Agent: "claude", SessionID: "one"},
		Kernel:     session.BlockedOnYou,
		Rank:       session.RankBlockedOnYou,
		Detail:     "permission-prompt",
		Name:       "agent-notify",
		Cwd:        "/Users/somebody/projects/agent-notify",
		Branch:     "worktree-view-changed",
		Model:      "claude-opus-5",
		StateSince: noon.Add(-12 * time.Minute),
		Message:    "May I run `rm -rf ./build`?",
		Usage:      session.Usage{Input: 120_000, Output: 7_000},
		Process:    session.Process{PID: 44182},
	}
}

func plain(text string) string {
	var out strings.Builder
	for i := 0; i < len(text); {
		if text[i] == 0x1b {
			for i < len(text) && text[i] != 'm' {
				i++
			}
			i++
			continue
		}
		out.WriteByte(text[i])
		i++
	}
	return out.String()
}

// TestFzfAcceptsWhatItIsTold is the cheapest test here and the one that pays:
// a mistyped colour key or a preview-window spec fzf does not accept would
// otherwise be found by somebody pressing the key it is bound to.
func TestFzfAcceptsWhatItIsTold(t *testing.T) {
	_, preview := Split(133)
	if _, err := fzf.ParseOptions(false,
		Arguments("/usr/bin/true", preview, Ghost(false), "")); err != nil {
		t.Fatalf("fzf refused its own arguments: %v", err)
	}
	// And the fallback spec, which is the one a pipe gets.
	_, preview = Split(0)
	if _, err := fzf.ParseOptions(false,
		Arguments("/usr/bin/true", preview, Ghost(true), "")); err != nil {
		t.Fatalf("fzf refused the fallback layout: %v", err)
	}
	// And the window that has something to say about the config file, which is
	// the one somebody sees exactly once — on the run after they edited it.
	_, preview = Split(133)
	if _, err := fzf.ParseOptions(false, Arguments("/usr/bin/true",
		preview, Ghost(false), Complaint([]string{"base0D is not a colour"})),
	); err != nil {
		t.Fatalf("fzf refused a complaint in the header: %v", err)
	}
}

// TestAPersonsFzfDefaultsAreNotThePickers: `--tac` reverses the urgency
// order, and `--multi` or `--print-query` hands back two lines where the
// picker reads one and never returns.
func TestAPersonsFzfDefaultsAreNotThePickers(t *testing.T) {
	t.Setenv("FZF_DEFAULT_OPTS", "--tac --multi --print-query")
	_, preview := Split(133)
	options, err := fzf.ParseOptions(false, Arguments("/usr/bin/true", preview, Ghost(false), ""))
	if err != nil {
		t.Fatalf("fzf refused its own arguments: %v", err)
	}
	if options.Tac || options.Multi > 0 || options.PrintQuery {
		t.Errorf("the picker took a person's defaults: tac %v, multi %d, print-query %v",
			options.Tac, options.Multi, options.PrintQuery)
	}
}

// TestThePreviewIsRunWherePathsHaveSpaces: the preview is this program, run
// by sh, and a path with a space or a quote in it is one word to sh.
func TestThePreviewIsRunWherePathsHaveSpaces(t *testing.T) {
	program := "/Users/First Last/it's/agent-notify-picker"
	arguments := Arguments(program, "right,50%", Ghost(false), "")
	at := slices.Index(arguments, "--preview")
	if at < 0 || !slices.Contains(arguments, "--with-shell=/bin/sh -c") {
		t.Fatalf("no preview run by sh in %v", arguments)
	}
	command := strings.Replace(arguments[at+1], "{1}", "'k'", 1)
	out, err := exec.Command("/bin/sh", "-c", "set -- "+command+`; printf '%s\n' "$1"`).Output()
	if err != nil || strings.TrimSpace(string(out)) != program {
		t.Errorf("sh read the program as %q (%v), want %q", out, err, program)
	}
}

// TestTheListIsTheCockpitsWidth: the list is the fixed part and the message
// takes the rest, so a wider screen is a wider message rather than more air
// between a name and its wait.
func TestTheListIsTheCockpitsWidth(t *testing.T) {
	if list, preview := Split(133); list != listWidth || preview != "right,97" {
		t.Errorf("133 columns: list %d, preview %q", list, preview)
	}
	if list, preview := Split(200); list != listWidth || preview != "right,164" {
		t.Errorf("200 columns: list %d, preview %q", list, preview)
	}
	// A terminal too narrow for both halves splits it, rather than leaving a
	// message a word wide.
	if list, preview := Split(60); list != 30 || preview != "right,30" {
		t.Errorf("60 columns: list %d, preview %q", list, preview)
	}
	// A terminal that would not say gets the list's width and a fraction.
	if list, preview := Split(0); list != listWidth || preview != "right,70%" {
		t.Errorf("no terminal width: list %d, preview %q", list, preview)
	}
}

// TestARowIsTheGlyphTheNameAndTheWait: the cockpit's row, and nothing the
// cockpit's row does not carry (D-133).
func TestARowIsTheGlyphTheNameAndTheWait(t *testing.T) {
	row := Row(aSession(), noon, 28)
	drawn := plain(row)
	for _, want := range []string{session.DefaultGlyphs[session.BlockedOnYou], "agent-notify", "12m"} {
		if !strings.Contains(drawn, want) {
			t.Errorf("row is missing %q: %q", want, drawn)
		}
	}
	for _, gone := range []string{"blocked", "permission-prompt", "projects", "worktree-view-changed"} {
		if strings.Contains(drawn, gone) {
			t.Errorf("row still says %q: %q", gone, drawn)
		}
	}
	// Exactly the width it was given, so that every wait ends on one column.
	if width := lipgloss.Width(row); width != 28 {
		t.Errorf("row is %d columns, want 28: %q", width, drawn)
	}
	if !strings.HasSuffix(drawn, " 12m") {
		t.Errorf("the wait is not at the row's right edge: %q", drawn)
	}
}

// TestOnlyTheGlyphIsColouredByState: a row painted end to end in the state's
// hue makes identity a moving target — a session's name would change colour
// when its state did, so no name could be learned by its colour.
func TestOnlyTheGlyphIsColouredByState(t *testing.T) {
	row := Row(aSession(), noon, 28)

	name := strings.Index(row, "agent-notify")
	if name < 0 {
		t.Fatalf("no name in the row: %q", row)
	}
	paints := row[:name]
	if !strings.Contains(paints, "235;111;146") {
		t.Errorf("the glyph is not in the state's colour: %q", paints)
	}
	at := strings.LastIndex(paints, "\x1b[")
	if at < 0 || !strings.Contains(paints[at:], "224;222;244") {
		t.Errorf("the name is not drawn in base05: %q", paints)
	}
}

// TestTheWaitSaysWhenToLook: dim, except bold for an agent waiting on you and
// gold for a turn that has run fifteen minutes — the cockpit's rule, so the two
// lists say the same thing about the same agent.
func TestTheWaitSaysWhenToLook(t *testing.T) {
	waitOf := func(record session.Record) string {
		row := Row(record, noon, 28)
		return row[strings.LastIndex(row, session.Ago(record.Elapsed(noon)))-24:]
	}
	if wait := waitOf(aSession()); !strings.Contains(wait, "\x1b[1;") && !strings.Contains(wait, "\x1b[1m") {
		t.Errorf("an agent waiting on you does not have a bold wait: %q", wait)
	}

	working := aSession()
	working.Kernel, working.Rank = session.Working, session.RankWorking
	working.StateSince = noon.Add(-5 * time.Minute)
	if wait := waitOf(working); !strings.Contains(wait, "140;136;166") {
		t.Errorf("a turn of five minutes is not dim: %q", wait)
	}
	working.StateSince = noon.Add(-session.StuckAfter)
	if wait := waitOf(working); !strings.Contains(wait, "246;193;119") {
		t.Errorf("a turn of fifteen minutes is not gold: %q", wait)
	}
}

// TestThePreviewIsWhatItSaid: the message and nothing else. Where it lives,
// its model, its spend and its history were here and were cut (D-133).
func TestThePreviewIsWhatItSaid(t *testing.T) {
	got := plain(Preview(aSession(), 70))
	if !strings.Contains(got, "rm -rf ./build") {
		t.Errorf("the preview does not say what the agent said:\n%s", got)
	}
	for _, gone := range []string{"projects", "claude-opus-5", "tokens", "pid", "earlier", "blocked"} {
		if strings.Contains(got, gone) {
			t.Errorf("the preview still says %q:\n%s", gone, got)
		}
	}

	quiet := aSession()
	quiet.Message = "  "
	if got := plain(Preview(quiet, 70)); !strings.Contains(got, "nothing said yet") {
		t.Errorf("a session that has said nothing gets: %q", got)
	}
}

// TestALongNameDoesNotTearTheRowInTwo: lipgloss WRAPS rather than truncates, so
// a name wider than its column came back as two lines and every row under it
// was drawn a line lower than the cursor thought it was. A real store found
// this in under a second.
func TestALongNameDoesNotTearTheRowInTwo(t *testing.T) {
	long := aSession()
	long.Name = "dmilog3-rollout-dashboard-proposals"
	withANewline := aSession()
	withANewline.Name = "two\nlines"

	for _, record := range []session.Record{long, withANewline} {
		if row := Row(record, noon, 28); strings.Contains(row, "\n") {
			t.Errorf("a row wrapped onto a second line: %q", row)
		}
	}
	if drawn := plain(Row(long, noon, 28)); !strings.Contains(drawn, "…") || !strings.HasSuffix(drawn, "12m") {
		t.Errorf("a name that was cut short does not say so, or took the wait with it: %q", drawn)
	}
}

// TestEveryStateIsADifferentWeight: the two commonest rows in a real list were
// pine (3.38:1) and muted (3.42:1) — the same weight, told apart only by hue,
// at a contrast where hue barely resolves.
func TestEveryStateIsADifferentWeight(t *testing.T) {
	seen := map[string]session.Kernel{}
	for _, kernel := range session.Kernels() {
		colour := StateColour(kernel.Rank())
		if other, taken := seen[colour]; taken {
			t.Errorf("%s and %s are both %s", kernel, other, colour)
		}
		seen[colour] = kernel
	}
}

// TestTheMessageIsRenderedAsMarkdown is the point of the preview: an agent
// writes markdown, and a picker that prints it raw shows somebody a wall of
// asterisks at the moment they are deciding who to answer.
func TestTheMessageIsRenderedAsMarkdown(t *testing.T) {
	rendered := Markdowned("# Heading\n\nSome **bold** text and `code`.\n", 60)

	if strings.Contains(rendered, "**bold**") || strings.Contains(rendered, "# Heading") {
		t.Errorf("the markdown came through as source:\n%s", rendered)
	}
	if !strings.Contains(plain(rendered), "bold") || !strings.Contains(plain(rendered), "Heading") {
		t.Errorf("the words themselves went missing:\n%s", plain(rendered))
	}
	if !strings.Contains(rendered, "\x1b[") {
		t.Errorf("nothing was styled, so the style file is not being read:\n%s", rendered)
	}
}

// TestTheStateDecidesTheColour: the question this is opened to answer is which
// session wants you, and the answer is a hue before it is a word (R24).
func TestTheStateDecidesTheColour(t *testing.T) {
	blocked := StateColour(session.RankBlockedOnYou)
	working := StateColour(session.RankWorking)
	ended := StateColour(session.RankEnded)

	if blocked != base08 {
		t.Errorf("blocked-on-you is %s, want love", blocked)
	}
	if working == blocked || ended == working {
		t.Errorf("three states share a colour: %s %s %s", blocked, working, ended)
	}
	// Working is BRIGHT pine, not pine: pine is 3.38:1 on this canvas and
	// muted is 3.42:1, so working and ended were the same weight.
	if working != base14 {
		t.Errorf("working is %s, want bright pine", working)
	}
	// An unknown rank from a newer core still gets something rather than
	// nothing, which is what the rank is on the wire for (§A5.5).
	if StateColour(999) == "" {
		t.Errorf("a rank this build has never seen got no colour at all")
	}
}

// TestAnUnlexableCodeBlockIsNotPaintedAsAFault.
//
// glamour GUESSES a language for a fence that names none, and chroma marks
// everything it then cannot lex as an error token — which the style shipped
// with a red background. A message containing a fenced screen mock-up came out
// as solid red bars across the pane, which is the loudest thing this program
// has ever drawn and meant nothing at all.
func TestAnUnlexableCodeBlockIsNotPaintedAsAFault(t *testing.T) {
	const red = "48;2;240;91;91" // #F05B5B, chroma's error background
	mock := "Here is the layout:\n\n```\n ▌  ⚠ blocked   agent-notify   12m\n ───────────────────\n```\n"

	rendered := Markdowned(mock, 70)
	if strings.Contains(rendered, red) {
		t.Errorf("a code block chroma could not lex was painted as an error:\n%q", rendered)
	}
	if !strings.Contains(plain(rendered), "agent-notify") {
		t.Errorf("the block's contents went missing:\n%s", plain(rendered))
	}
}
