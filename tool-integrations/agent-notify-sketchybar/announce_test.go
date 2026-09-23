package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agentnotify "github.com/lassoColombo/agent-notify"
)

func announcer() Bar {
	b := bar()
	b.Announce = 8 * time.Second
	return b
}

// closes looks for the close as ARGUMENTS rather than as text, because the same
// words appear inside a row's click script — which closes the chip it was
// clicked in, and has nothing to do with an announcement ending.
func closes(arguments []string, item string) bool {
	for i := 0; i+2 < len(arguments); i++ {
		if arguments[i] == "--set" && arguments[i+1] == item && arguments[i+2] == "popup.drawing=off" {
			return true
		}
	}
	return false
}

func at(id string, kernel agentnotify.Kernel, since time.Time, message string) agentnotify.Record {
	record := session(id, kernel, since)
	record.Message = message
	return record
}

// TestAStateChangeLightsTheBarUp is the whole feature in one assertion: the
// counter is lit, its chip is open, the row inside it is lit, and the agent's
// words are under it.
func TestAStateChangeLightsTheBarUp(t *testing.T) {
	b := announcer()
	just := at("lenny", agentnotify.BlockedOnYou, b.Now.Add(-time.Second), "may I run the tests?")

	arguments, now := Render(b, []agentnotify.Record{just}, agentnotify.Arrival{}, false)
	if !now.Announced() {
		t.Fatal("nothing was announced")
	}
	argv := strings.Join(arguments, " ")

	counter := b.item(agentnotify.BlockedOnYou)
	for _, want := range []string{
		counter + " popup.drawing=on",                 // the chip opens
		"--set " + counter + " background.drawing=on", // the counter is lit
		"--set " + counter + ".row.0 background.drawing=on",
		"label=may I run the tests?",
	} {
		if !strings.Contains(argv, want) {
			t.Errorf("the announcement does not %q:\n%s", want, argv)
		}
	}
}

// TestWorkingIsNotWorthAnnouncing. An agent getting on with it is the state the
// bar is in most of the day, and a bar lit up all day says nothing.
func TestWorkingIsNotWorthAnnouncing(t *testing.T) {
	b := announcer()
	busy := at("one", agentnotify.Working, b.Now, "still going")
	if _, now := Render(b, []agentnotify.Record{busy}, agentnotify.Arrival{}, false); now.Announced() {
		t.Errorf("working announced itself: %+v", now)
	}
	idle := at("two", agentnotify.Idle, b.Now, "")
	if _, now := Render(b, []agentnotify.Record{idle}, agentnotify.Arrival{}, false); now.Announced() {
		t.Errorf("idle announced itself: %+v", now)
	}
}

// TestAnAnnouncementIsOverWhenTheRecordSaysSo, not when the display noticed.
//
// The deadline is state-since plus the window, so an unrelated agent's event —
// which repaints the whole bar — cannot extend it. With `now + window` a busy
// machine would keep one announcement lit for as long as anything else was
// happening.
func TestAnAnnouncementIsOverWhenTheRecordSaysSo(t *testing.T) {
	b := announcer()
	old := at("lenny", agentnotify.FinishedATurn, b.Now.Add(-30*time.Second), "done")

	_, now := Render(b, []agentnotify.Record{old}, agentnotify.Arrival{}, false)
	if now.Announced() {
		t.Fatalf("a state from half a minute ago announced itself: %+v", now)
	}

	fresh := at("lenny", agentnotify.FinishedATurn, b.Now.Add(-time.Second), "done")
	_, first := Render(b, []agentnotify.Record{fresh}, agentnotify.Arrival{}, false)
	later := b
	later.Now = b.Now.Add(4 * time.Second)
	_, second := Render(later, []agentnotify.Record{fresh}, first, false)
	if !first.Announced() || !second.Announced() {
		t.Fatal("it should still be lit four seconds in")
	}
	if !first.Until.Equal(second.Until) {
		t.Errorf("a repaint moved the deadline: %s then %s", first.Until, second.Until)
	}
}

// TestWhenItIsOverTheChipCloses.
func TestWhenItIsOverTheChipCloses(t *testing.T) {
	b := announcer()
	record := at("lenny", agentnotify.Broke, b.Now.Add(-time.Second), "it fell over")
	_, was := Render(b, []agentnotify.Record{record}, agentnotify.Arrival{}, false)

	over := b
	over.Now = b.Now.Add(20 * time.Second)
	arguments, now := Render(over, []agentnotify.Record{record}, was, false)
	if now.Announced() {
		t.Fatal("it is still announcing")
	}
	argv := strings.Join(arguments, " ")
	if !closes(arguments, b.item(agentnotify.Broke)) {
		t.Errorf("the chip was left open:\n%s", argv)
	}
	if !strings.Contains(argv, "--set "+b.item(agentnotify.Broke)+" background.drawing=off") {
		t.Errorf("the counter was left lit:\n%s", argv)
	}
}

// TestAChipSomebodyIsReadingIsNotTakenAway. A pointer that has arrived owns the
// chip from that moment; what closes it then is the pointer leaving the bar.
func TestAChipSomebodyIsReadingIsNotTakenAway(t *testing.T) {
	b := announcer()
	record := at("lenny", agentnotify.Broke, b.Now.Add(-time.Second), "it fell over")
	_, was := Render(b, []agentnotify.Record{record}, agentnotify.Arrival{}, false)

	over := b
	over.Now = b.Now.Add(20 * time.Second)
	arguments, _ := Render(over, []agentnotify.Record{record}, was, true)
	argv := strings.Join(arguments, " ")
	if closes(arguments, b.item(agentnotify.Broke)) {
		t.Errorf("a chip with a pointer in it was closed:\n%s", argv)
	}
	if !strings.Contains(argv, "--set "+b.item(agentnotify.Broke)+" background.drawing=off") {
		t.Errorf("the counter should still stop being lit:\n%s", argv)
	}
}

// TestWhatJustHappenedWins, which is not the same as what is worst. Somebody
// looking up at a lit bar is asking what changed, and urgency has already had
// its say in the order of the counters.
func TestWhatJustHappenedWins(t *testing.T) {
	b := announcer()
	worse := at("broken", agentnotify.Broke, b.Now.Add(-5*time.Second), "fell over")
	newer := at("asking", agentnotify.BlockedOnYou, b.Now.Add(-time.Second), "may I?")

	_, now := Render(b, []agentnotify.Record{worse, newer}, agentnotify.Arrival{}, false)
	if !now.Announced() || now.Record.Key != newer.Key {
		t.Errorf("announced %+v, want the one that just happened", now)
	}
}

// TestAnnouncingCanBeTurnedOff, for somebody who wants counters that only count.
func TestAnnouncingCanBeTurnedOff(t *testing.T) {
	b := bar() // no Announce
	record := at("lenny", agentnotify.BlockedOnYou, b.Now, "may I?")
	arguments, now := Render(b, []agentnotify.Record{record}, agentnotify.Arrival{}, false)
	if now.Announced() {
		t.Fatalf("announced with the window at zero: %+v", now)
	}
	if strings.Contains(strings.Join(arguments, " "), "popup.drawing=on") {
		t.Error("a chip opened itself with announcing off")
	}
}

// ── the preview ──────────────────────────────────────────────────────────────

func TestThePreviewWrapsAndKeepsTheAgentsOwnLines(t *testing.T) {
	preview := Preview{Lines: 4, Depth: 20, Width: 20, Text: "0xffffffff"}
	record := at("one", agentnotify.Working,
		time.Now(), "the quick brown fox jumped over it\n\nand then stopped")

	lines := preview.Of(record)
	if lines[0] != "one" {
		t.Errorf("the first line is %q, want the session's name", lines[0])
	}
	for _, line := range lines {
		if len([]rune(line)) > preview.Width {
			t.Errorf("a line is %d wide, want at most %d: %q", len([]rune(line)), preview.Width, line)
		}
	}
	if !strings.Contains(strings.Join(lines, "\n"), "\n\nand then stopped") {
		t.Errorf("the agent's own blank line did not survive:\n%q", lines)
	}
}

// TestWhatIsWrittenAndWhatIsDrawnAreDifferentQuestions. Conflating them is what
// makes a scroll reveal blank rows.
func TestWhatIsWrittenAndWhatIsDrawnAreDifferentQuestions(t *testing.T) {
	b := bar()
	b.Preview = Preview{Lines: 3, Depth: 12, Width: 10, Text: "0xffffffff"}
	record := at("one", agentnotify.Working, time.Now(), strings.Repeat("word ", 30))

	slots, held, rest := b.previewOf(agentnotify.Working, record)
	written, drawn := 0, 0
	for _, slot := range slots {
		if slot.filled {
			written++
		}
		if slot.filled && slot.on {
			drawn++
		}
	}
	if written <= drawn {
		t.Errorf("%d written and %d drawn: everything written was drawn, so a scroll "+
			"would reveal nothing", written, drawn)
	}
	if drawn != b.Preview.Lines {
		t.Errorf("%d lines drawn, want %d", drawn, b.Preview.Lines)
	}
	if rest != held-b.Preview.window() {
		t.Errorf("the footer says %d below, with %d held and %d on screen", rest, held, b.Preview.window())
	}
}

func TestAMessageLongerThanTheDepthSaysSo(t *testing.T) {
	preview := Preview{Lines: 3, Depth: 5, Width: 10, Text: "0xffffffff"}
	record := at("one", agentnotify.Working, time.Now(), strings.Repeat("word ", 50))

	lines := preview.Of(record)
	if len(lines) != preview.Depth {
		t.Fatalf("%d lines, want it cut to %d", len(lines), preview.Depth)
	}
	if !strings.HasSuffix(lines[len(lines)-1], "…") {
		t.Errorf("the last line is %q — a message that just stops reads as an agent "+
			"that stopped", lines[len(lines)-1])
	}
}

func TestQuotable(t *testing.T) {
	for given, want := range map[string]string{
		"it's fine":   "it’s fine",
		"two\nlines":  "two lines",
		"a\tb":        "a b",
		"plain words": "plain words",
	} {
		if got := quotable(given); got != want {
			t.Errorf("quotable(%q) = %q, want %q", given, got, want)
		}
	}
}

// ── the scroll, run for real ─────────────────────────────────────────────────

// TestTheScrollScriptMovesTheWindow runs the generated shell against a stub
// sketchybar, because a script that is only ever read is a script nobody has
// tested.
//
// Twelve lines held, three on screen, the window at the top, and one notch down.
func TestTheScrollScriptMovesTheWindow(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "argv")
	stub := filepath.Join(dir, "sketchybar")
	// It answers a --query the way sketchybar answers one, and records anything
	// else it is asked to do.
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = --query ]; then printf '{\\n\\t\"icon\": {\\n\\t\\t\"value\" : \"pv:agent.working:12:0\"\\n\\t}\\n}\\n'; exit 0; fi\n" +
		"printf '%s ' \"$@\" >> " + log + "\n"
	if err := os.WriteFile(stub, []byte(script), 0o700); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	b := bar()
	b.Sketchybar = stub
	b.Preview = Preview{Lines: 4, Depth: 20, Width: 30, Text: "0xffffffff"}

	run := exec.Command("/bin/sh", "-c", b.scrolling())
	run.Env = append(os.Environ(), "SENDER="+b.event(), "SCROLL_DELTA=-40")
	if out, err := run.CombinedOutput(); err != nil {
		t.Fatalf("the scroll script would not run: %v\n%s", err, out)
	}

	argv, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("the script did nothing: %v", err)
	}
	sent := string(argv)
	// The window was lines 1..3 and is now 3..5, so two go off and two come on
	// — and nothing else is touched, which is the whole point of a pool.
	for _, want := range []string{
		"--set agent.working.pv.1 drawing=off",
		"--set agent.working.pv.2 drawing=off",
		"--set agent.working.pv.4 drawing=on",
		"--set agent.working.pv.5 drawing=on",
		"--set agent.scroll icon=pv:agent.working:12:2",
		"7 more lines",
	} {
		if !strings.Contains(sent, want) {
			t.Errorf("the scroll did not %q:\n%s", want, sent)
		}
	}
	if strings.Contains(sent, "pv.3 drawing") || strings.Contains(sent, "pv.9 drawing") {
		t.Errorf("the scroll touched a line it did not need to:\n%s", sent)
	}
}

// TestTheScrollStopsAtTheEnds rather than running off either of them.
func TestTheScrollStopsAtTheEnds(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "argv")
	stub := filepath.Join(dir, "sketchybar")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = --query ]; then printf '{\"icon\": {\"value\" : \"pv:agent.working:5:0\"}}\\n'; exit 0; fi\n" +
		"printf '%s ' \"$@\" >> " + log + "\n"
	if err := os.WriteFile(stub, []byte(script), 0o700); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	b := bar()
	b.Sketchybar = stub
	b.Preview = Preview{Lines: 4, Depth: 20, Width: 30, Text: "0xffffffff"}

	// Already at the top, asked to go up: nothing to do, and nothing sent.
	run := exec.Command("/bin/sh", "-c", b.scrolling())
	run.Env = append(os.Environ(), "SENDER="+b.event(), "SCROLL_DELTA=200")
	if out, err := run.CombinedOutput(); err != nil {
		t.Fatalf("the scroll script would not run: %v\n%s", err, out)
	}
	if _, err := os.Stat(log); err == nil {
		sent, _ := os.ReadFile(log)
		t.Errorf("a scroll off the top still sent something:\n%s", sent)
	}

	// A shove down stops at the last line rather than scrolling into nothing.
	run = exec.Command("/bin/sh", "-c", b.scrolling())
	run.Env = append(os.Environ(), "SENDER="+b.event(), "SCROLL_DELTA=-400")
	if out, err := run.CombinedOutput(); err != nil {
		t.Fatalf("the scroll script would not run: %v\n%s", err, out)
	}
	sent, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("the scroll did nothing at all: %v", err)
	}
	if !strings.Contains(string(sent), "icon=pv:agent.working:5:2") {
		t.Errorf("the window did not stop at the last line:\n%s", sent)
	}
}

// TestAWheelIsForwardedFromWhereverItLands, because a mouse event reaches only
// the item under the pointer and that is a different one every time.
func TestAWheelIsForwardedFromWhereverItLands(t *testing.T) {
	b := announcer()
	argv := strings.Join(painted(b, []agentnotify.Record{
		at("one", agentnotify.Working, b.Now, "words")}), " ")

	if !strings.Contains(argv, "--subscribe agent.working.row.0 mouse.entered mouse.scrolled") {
		t.Errorf("a row does not hear the wheel:\n%s", argv)
	}
	if !strings.Contains(argv, "--subscribe agent.working.pv.0 mouse.scrolled") {
		t.Errorf("a preview line does not hear the wheel:\n%s", argv)
	}
	if !strings.Contains(argv, "--trigger "+b.event()) {
		t.Errorf("nothing forwards the wheel to the one item that knows where the window is:\n%s", argv)
	}
}
