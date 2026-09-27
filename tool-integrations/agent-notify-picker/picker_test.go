package main

import (
	"strings"
	"testing"
	"time"

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
	if _, err := fzf.ParseOptions(true,
		Arguments("/usr/bin/true", PreviewWindow(3, 40), Ghost(false), "")); err != nil {
		t.Fatalf("fzf refused its own arguments: %v", err)
	}
	// And the fallback spec, which is the one a pipe or a tiny pane gets.
	if _, err := fzf.ParseOptions(true,
		Arguments("/usr/bin/true", PreviewWindow(3, 0), Ghost(true), "")); err != nil {
		t.Fatalf("fzf refused the fallback layout: %v", err)
	}
	// And the window that has something to say about the config file, which is
	// the one somebody sees exactly once — on the run after they edited it.
	if _, err := fzf.ParseOptions(true, Arguments("/usr/bin/true",
		PreviewWindow(3, 40), Ghost(false), Complaint([]string{"base0D is not a colour"})),
	); err != nil {
		t.Fatalf("fzf refused a complaint in the header: %v", err)
	}
}

// TestTheSplitIsSizedByHowManySessionsThereAre: a fraction is the wrong
// instrument — three rows in 28% of a tall pane is air, twelve rows in the same
// 28% is a list that scrolls while the preview has rows to spare.
func TestTheSplitIsSizedByHowManySessionsThereAre(t *testing.T) {
	if got := PreviewWindow(3, 40); got != "down,34" {
		t.Errorf("3 sessions in 40 rows: %q, want the list to take only 3", got)
	}
	if got := PreviewWindow(30, 40); got != "down,23" {
		t.Errorf("30 sessions in 40 rows: %q, want the list capped at two fifths", got)
	}
	// A terminal that would not say, and a pane too short to divide, both fall
	// back to the fraction rather than to something unreadable.
	if got := PreviewWindow(3, 0); got != "down,68%" {
		t.Errorf("no terminal height: %q", got)
	}
	if got := PreviewWindow(3, 10); got != "down,68%" {
		t.Errorf("a pane with no room to split: %q", got)
	}
}

// TestTheRowCarriesWhatIsTypedAtIt: the row is what the matcher matches, so
// anything somebody hunts by has to be in it — the branch and the directory
// included, which nothing else on the line would otherwise say.
func TestTheRowCarriesWhatIsTypedAtIt(t *testing.T) {
	was := homeDirectory
	homeDirectory = func() (string, error) { return "/Users/somebody", nil }
	defer func() { homeDirectory = was }()

	row := plain(Row(aSession(), noon, Measure([]session.Record{aSession()})))
	for _, want := range []string{
		"agent-notify", "permission-prompt", "12m",
		"~/projects/agent-notify", "worktree-view-changed",
	} {
		if !strings.Contains(row, want) {
			t.Errorf("row is missing %q:\n%s", want, row)
		}
	}
	if strings.Contains(row, "/Users/somebody") {
		t.Errorf("the row printed a home directory in full: %s", row)
	}
	// Core's canonical string is the LABEL's job. The row says the same thing
	// in the width a column can afford: "blocked/permission-prompt".
	if !strings.Contains(row, "blocked/permission-prompt") {
		t.Errorf("the row does not say what state this is: %s", row)
	}
	if strings.Contains(row, "blocked-on-you") {
		t.Errorf("the row spells a kernel core writes as a sentence: %s", row)
	}
}

// TestOnlyTheStateIsColouredByState: a row painted end to end in the state's
// hue makes identity a moving target — a session's name would change colour
// when its state did, so no name could be learned by its colour.
func TestOnlyTheStateIsColouredByState(t *testing.T) {
	row := Row(aSession(), noon, Measure([]session.Record{aSession()}))

	name := strings.Index(row, "agent-notify")
	if name < 0 {
		t.Fatalf("no name in the row: %q", row)
	}
	// The escape immediately before the name is the one that paints it.
	paints := row[:name]
	if at := strings.LastIndex(paints, "\x1b["); at < 0 ||
		!strings.Contains(paints[at:], "224;222;244") {
		t.Errorf("the name is not drawn in base05: %q", paints)
	}
	if strings.Contains(paints[strings.LastIndex(paints, "\x1b["):], "235;111;146") {
		t.Errorf("the name is painted in the state's colour: %q", row)
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

// TestTheFactsAreTheOnesWorthScanning: what it is working on, with what, and
// what it has spent — the things a row has no room for.
func TestTheFactsAreTheOnesWorthScanning(t *testing.T) {
	facts := plain(FactBlock(Facts(aSession()), 100))
	for _, want := range []string{
		"projects/agent-notify", // where
		"worktree-view-changed", // which branch
		"claude-opus-5",         // which model
		"127k tokens",           // what it has spent
		"pid 44182",
	} {
		if !strings.Contains(facts, want) {
			t.Errorf("facts are missing %q:\n%s", want, facts)
		}
	}
}

// TestNothingIsInventedWhenNothingWasReported: a session with no usage, no
// model and no branch is the ordinary case for most agents, and every one of
// those lines must simply not appear (§A7.4.3).
func TestNothingIsInventedWhenNothingWasReported(t *testing.T) {
	quiet := session.Record{
		Key:    session.Key{Host: "here", Agent: "codex", SessionID: "two"},
		Kernel: session.Idle,
		Name:   "quiet",
	}
	facts := plain(FactBlock(Facts(quiet), 100))
	for _, unwanted := range []string{"tokens", "pid"} {
		if strings.Contains(facts, unwanted) {
			t.Errorf("facts invented %q out of an empty record: %q", unwanted, facts)
		}
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

// TestPreviewSaysWhatTheAgentSaidAndWhatItSaidBefore.
func TestPreviewSaysWhatTheAgentSaidAndWhatItSaidBefore(t *testing.T) {
	history := session.History{
		Changes: []session.StateChange{{
			At: noon.Add(-12 * time.Minute), From: session.Working, To: session.BlockedOnYou,
		}},
		Messages: []session.SaidSomething{
			{At: noon.Add(-31 * time.Minute), Message: "tests are green"},
			{At: noon.Add(-12 * time.Minute), Message: "May I run `rm -rf ./build`?"},
		},
	}

	got := plain(Preview(aSession(), history, noon, 70))
	for _, want := range []string{
		"rm -rf ./build", "earlier", "working → blocked-on-you", "tests are green",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("preview is missing %q:\n%s", want, got)
		}
	}
	// WHAT IT SAID IS THE FIRST THING IN THE PANE. It used to be third, under a
	// heading that repeated the highlighted row and a facts line that repeated
	// that row's path and branch.
	if said, facts := strings.Index(got, "rm -rf ./build"), strings.Index(got, "where"); said > facts {
		t.Errorf("the metadata is above the message:\n%s", got)
	}
	// And who it is is said once, in the pane's label, rather than twice.
	if strings.Contains(got, "blocked-on-you/permission-prompt") {
		t.Errorf("the pane repeats the label's identity line:\n%s", got)
	}
	// The message at the top is not repeated under "earlier".
	if strings.Count(got, "rm -rf ./build") != 1 {
		t.Errorf("the current message is repeated in the history:\n%s", got)
	}
}

// TestTheLabelSaysWhoInCoresOwnWords: the row no longer spells the kernel out,
// so the canonical (kernel, detail) string has to be somewhere — it is here,
// once, for the row the cursor is on.
func TestTheLabelSaysWhoInCoresOwnWords(t *testing.T) {
	label := plain(Label(aSession(), noon))
	for _, want := range []string{"agent-notify", "blocked-on-you/permission-prompt", "12m"} {
		if !strings.Contains(label, want) {
			t.Errorf("label is missing %q: %q", want, label)
		}
	}
}

// TestTheLeafOfThePathIsTheLitPart: a working directory is five components
// long and only the last tells two sessions apart, so in one colour it is the
// loudest thing on the row and competing with the name.
func TestTheLeafOfThePathIsTheLitPart(t *testing.T) {
	was := homeDirectory
	homeDirectory = func() (string, error) { return "/Users/somebody", nil }
	defer func() { homeDirectory = was }()

	drawn := Directory("/Users/somebody/projects/rocket")
	leaf := strings.LastIndex(drawn, "rocket")
	if leaf < 0 {
		t.Fatalf("the leaf is missing: %q", drawn)
	}
	if !strings.Contains(drawn[:leaf], base03) && !strings.Contains(drawn[:leaf], "110;106;134") {
		t.Errorf("the parent directories are not dimmed: %q", drawn)
	}
	if !strings.Contains(drawn[leaf-12:leaf], "196;167;231") {
		t.Errorf("the leaf is not iris: %q", drawn)
	}
}

func TestTokens(t *testing.T) {
	for _, one := range []struct {
		total uint64
		want  string
	}{{640, "640"}, {127_000, "127k"}, {2_500_000, "2.5M"}} {
		if got := Tokens(one.total); got != one.want {
			t.Errorf("Tokens(%d) = %q, want %q", one.total, got, one.want)
		}
	}
}

func TestHomeIsWrittenTheWayPeopleWriteIt(t *testing.T) {
	was := homeDirectory
	homeDirectory = func() (string, error) { return "/Users/somebody", nil }
	defer func() { homeDirectory = was }()

	if got := Home("/Users/somebody/projects/rocket"); got != "~/projects/rocket" {
		t.Errorf("Home = %q", got)
	}
	if got := Home("/opt/elsewhere"); got != "/opt/elsewhere" {
		t.Errorf("Home = %q — a path outside home is left alone", got)
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

// TestALongNameDoesNotTearTheRowInTwo: lipgloss WRAPS rather than truncates, so
// a name wider than its column came back as two lines and every row under it
// was drawn a line lower than the cursor thought it was. A real store found
// this in under a second.
func TestALongNameDoesNotTearTheRowInTwo(t *testing.T) {
	was := homeDirectory
	homeDirectory = func() (string, error) { return "/Users/somebody", nil }
	defer func() { homeDirectory = was }()

	long := aSession()
	long.Name = "dmilog3-rollout-dashboard-proposals"
	long.Detail = "a detail nobody would ever sensibly write"
	withANewline := aSession()
	withANewline.Name = "two\nlines"

	sessions := []session.Record{long, withANewline}
	columns := Measure(sessions)
	for _, record := range sessions {
		row := Row(record, noon, columns)
		if strings.Contains(row, "\n") {
			t.Errorf("a row wrapped onto a second line: %q", row)
		}
	}
	if !strings.Contains(plain(Row(long, noon, columns)), "…") {
		t.Errorf("a name that was cut short does not say so: %q", plain(Row(long, noon, columns)))
	}
}

// TestALongPathLosesItsHeadNotItsTail: cutting the tail takes the repository's
// name off, which is the one component that identifies the session.
func TestALongPathLosesItsHeadNotItsTail(t *testing.T) {
	long := "~/projects/personal/nushell/modules/nu-http-client-generator"
	got := elide(long, 44)

	if !strings.HasSuffix(got, "nu-http-client-generator") {
		t.Errorf("the leaf was cut off: %q", got)
	}
	if !strings.HasPrefix(got, "…/") {
		t.Errorf("nothing says the path was shortened: %q", got)
	}
	if width := len([]rune(got)); width > 44 {
		t.Errorf("%q is %d columns, want 44 at most", got, width)
	}
	// Whole components, so it can still be read aloud: every part between the
	// slashes is one the original path actually had.
	for _, component := range strings.Split(strings.TrimPrefix(got, "…/"), "/") {
		if !strings.Contains(long, "/"+component+"/") && !strings.HasSuffix(long, "/"+component) {
			t.Errorf("%q is not a component of %q", component, long)
		}
	}
	// An ordinary path is left exactly alone.
	if short := "~/projects/work/cybergon/lenny"; elide(short, 44) != short {
		t.Errorf("a path that fits was changed: %q", elide(short, 44))
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
