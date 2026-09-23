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

var nine = time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)

// painted is the whole bar — the structure and then the values — for a test
// that has nothing to announce and does not care which half a thing came from.
// It is also the order the display sends them in when it has just declared
// everything, which is what makes "added before it is set" testable at all.
func painted(bar Bar, sessions []agentnotify.Record) []string {
	values, _ := Render(bar, sessions, agentnotify.Arrival{}, false)
	return append(Scaffold(bar), values...)
}

func bar() Bar {
	return Bar{
		Prefix: "agent", Position: "left", Rows: 3,
		Preview:    Preview{Lines: 4, Depth: 10, Width: 30, Text: "0xffffffff"},
		Sketchybar: "/opt/homebrew/bin/sketchybar", Core: "/usr/local/bin/agent-notify",
		Glyphs:  agentnotify.NewPalette(map[agentnotify.Kernel]string{agentnotify.BlockedOnYou: "!", agentnotify.Working: "*"}, nil),
		Colours: agentnotify.NewPalette(DefaultColours, nil),
		Now:     nine.Add(4 * time.Minute),
	}
}

func session(id string, kernel agentnotify.Kernel, since time.Time) agentnotify.Record {
	return agentnotify.Record{
		Key:        agentnotify.Key{Host: "mac", Agent: "claude", SessionID: id},
		Name:       id,
		Kernel:     kernel,
		Rank:       kernel.Rank(),
		StateSince: since,
	}
}

// joined is the command line as one string, which is what the assertions below
// are written against: the argv is exactly what would have been run.
func joined(arguments []string) string { return strings.Join(arguments, " ") }

func TestACounterPerStateWithItsCount(t *testing.T) {
	line := joined(painted(bar(), []agentnotify.Record{
		session("one", agentnotify.Working, nine),
		session("two", agentnotify.Working, nine),
		session("three", agentnotify.BlockedOnYou, nine),
	}))

	for _, want := range []string{
		"--add item agent.working left",
		"--add item agent.blocked-on-you left",
		"icon=* ",
		"label=2 ",
		"label=1 ",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("the render does not contain %q:\n%s", want, line)
		}
	}
}

// TestAStateWithNothingInItIsNotDrawnButKeepsItsPlace: a bar whose items move
// about as the day goes on is a bar nobody can click.
func TestAStateWithNothingInItIsNotDrawnButKeepsItsPlace(t *testing.T) {
	arguments := painted(bar(), []agentnotify.Record{session("one", agentnotify.Working, nine)})
	line := joined(arguments)

	if !strings.Contains(line, "--add item agent.broke left") {
		t.Errorf("an empty state lost its place on the bar:\n%s", line)
	}
	if !strings.Contains(line, "--set agent.broke icon=") || !strings.Contains(line, "drawing=off") {
		t.Errorf("an empty state is still being drawn:\n%s", line)
	}

	// And the order is the rank order, always, whatever the data says.
	positions := map[string]int{}
	for i, argument := range arguments {
		if strings.HasPrefix(argument, "agent.") && !strings.Contains(argument, ".row.") {
			if _, seen := positions[argument]; !seen {
				positions[argument] = i
			}
		}
	}
	for _, pair := range [][2]string{
		{"agent.blocked-on-you", "agent.broke"},
		{"agent.broke", "agent.finished-a-turn"},
		{"agent.finished-a-turn", "agent.working"},
		{"agent.working", "agent.idle"},
	} {
		if positions[pair[0]] > positions[pair[1]] {
			t.Errorf("%s comes after %s", pair[0], pair[1])
		}
	}
}

func TestEndedSessionsAreNotCounted(t *testing.T) {
	line := joined(painted(bar(), []agentnotify.Record{
		session("gone", agentnotify.Ended, nine),
		session("here", agentnotify.Working, nine),
	}))
	if strings.Contains(line, "agent.ended") {
		t.Errorf("the bar has an item for ended sessions:\n%s", line)
	}
	if !strings.Contains(line, "label=1 ") {
		t.Errorf("the ended one was counted:\n%s", line)
	}
}

// TestAChipListsItsSessionsWithTheirAge.
func TestAChipListsItsSessionsWithTheirAge(t *testing.T) {
	line := joined(painted(bar(), []agentnotify.Record{
		session("monomodules", agentnotify.Working, nine),
	}))

	if !strings.Contains(line, "--add item agent.working.row.0 popup.agent.working") {
		t.Errorf("the chip has no row:\n%s", line)
	}
	if !strings.Contains(line, "monomodules") || !strings.Contains(line, "4m") {
		t.Errorf("the row does not say what it is and how long it has been:\n%s", line)
	}
}

// TestARowThatDoesNotFitIsCountedRatherThanDropped: a count that is quietly
// wrong is worse than a count.
func TestARowThatDoesNotFitIsCountedRatherThanDropped(t *testing.T) {
	var many []agentnotify.Record
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		many = append(many, session(id, agentnotify.Working, nine))
	}
	line := joined(painted(bar(), many))

	if !strings.Contains(line, "and 3 more") {
		t.Errorf("five sessions in three rows did not say what was left over:\n%s", line)
	}
	if !strings.Contains(line, "label=5 ") {
		t.Errorf("the counter does not still say five:\n%s", line)
	}
}

// TestClickingFocuses is the whole point of a chip.
func TestClickingFocuses(t *testing.T) {
	record := session("monomodules", agentnotify.BlockedOnYou, nine)
	line := joined(painted(bar(), []agentnotify.Record{record}))

	want := "'/usr/local/bin/agent-notify' focus-session --quiet '" + record.Key.String() + "'"
	if !strings.Contains(line, want) {
		t.Errorf("no click runs %s:\n%s", want, line)
	}
	// And the counter itself goes to the most urgent one in it, which saves
	// the hover nine times in ten.
	if !strings.Contains(line, "--set agent.blocked-on-you click_script=") {
		t.Errorf("the counter is not clickable:\n%s", line)
	}
}

// TestHoverOpensOneChipAndClosesTheOthers: two chips open at once is a
// rendering bug a person sees as flicker.
func TestHoverOpensOneChipAndClosesTheOthers(t *testing.T) {
	arguments := painted(bar(), []agentnotify.Record{session("one", agentnotify.Working, nine)})

	var script string
	for i, argument := range arguments {
		if argument == "agent.working" && i+1 < len(arguments) {
			for _, candidate := range arguments[i:] {
				if strings.HasPrefix(candidate, "script=") {
					script = candidate
					break
				}
			}
		}
		if script != "" {
			break
		}
	}
	if script == "" {
		t.Fatalf("the counter has no hover script:\n%s", joined(arguments))
	}
	if !strings.Contains(script, "mouse.entered") {
		t.Errorf("the hover script does not check the sender: %s", script)
	}
	if !strings.Contains(script, "'agent.working' popup.drawing=on") {
		t.Errorf("hovering does not open its own chip: %s", script)
	}
	if !strings.Contains(script, "'agent.idle' popup.drawing=off") {
		t.Errorf("hovering does not close the others: %s", script)
	}
}

// TestSomethingHearsTheMouseLeave: there is no "the mouse left the bar" event
// on an item, only mouse.exited.global, so one hidden item hears it.
func TestSomethingHearsTheMouseLeave(t *testing.T) {
	line := joined(painted(bar(), nil))
	if !strings.Contains(line, "--subscribe agent.away mouse.exited.global") {
		t.Errorf("nothing closes the chips when the mouse leaves:\n%s", line)
	}
	if !strings.Contains(line, "--set agent.away drawing=off") {
		t.Errorf("the closer is visible:\n%s", line)
	}
}

// TestEveryItemIsAddedBeforeItIsSet is what makes a render idempotent and
// self-healing: sketchybar warns and carries on for both, so a display that
// says everything every time survives somebody reloading their bar.
func TestEveryItemIsAddedBeforeItIsSet(t *testing.T) {
	arguments := painted(bar(), []agentnotify.Record{session("one", agentnotify.Working, nine)})

	added := map[string]bool{}
	for i, argument := range arguments {
		switch argument {
		case "--add":
			added[arguments[i+2]] = true
		case "--set", "--subscribe":
			if !added[arguments[i+1]] {
				t.Errorf("%s %s before it was added", argument, arguments[i+1])
			}
		}
	}
}

// TestNothingReachesAShellUnquoted is the security assertion, and it is about
// the scripts only.
//
// The top-level arguments never touch a shell — they are argv to an exec — so a
// label full of semicolons is inert there. The `script=` and `click_script=`
// values are different: sketchybar runs those through a shell when somebody
// hovers or clicks. Everything interpolated into one is either ours or a
// percent-encoded key, and all of it is quoted.
func TestNothingReachesAShellUnquoted(t *testing.T) {
	nasty := session("'; rm -rf ~; echo '", agentnotify.Working, nine)
	arguments := painted(bar(), []agentnotify.Record{nasty})

	var scripts int
	for _, argument := range arguments {
		value, is := strings.CutPrefix(argument, "click_script=")
		if !is {
			value, is = strings.CutPrefix(argument, "script=")
		}
		if !is || value == "" {
			continue
		}
		scripts++
		// An agent's words DO reach the hover script — that is what a preview
		// is — and the rule is that they arrive with no quote of their own, so
		// that everything they contain stays inside the one this file opened.
		if strings.Count(value, "'")%2 != 0 {
			t.Errorf("a script has an odd number of quotes, so something escaped one:\n%s", value)
		}
		if strings.Contains(value, "label='") && strings.Contains(value, "rm -rf ~; echo") {
			t.Errorf("an agent's quotes survived into a script:\n%s", value)
		}
		// The key does reach one, and it arrives percent-encoded by the record
		// itself and quoted by this file.
		if strings.Contains(value, "focus-session") {
			if !strings.Contains(value, "'"+nasty.Key.String()+"'") {
				t.Errorf("the key is not quoted in:\n%s", value)
			}
			if strings.Contains(nasty.Key.String(), ";") {
				t.Errorf("the key reached a script with a semicolon still in it: %s", nasty.Key)
			}
		}
	}
	if scripts == 0 {
		t.Fatal("no scripts were rendered, so this proved nothing")
	}
}

// TestALabelNeedsNoQuoting, stated so that nobody adds quoting to it later and
// puts literal apostrophes on somebody's bar.
func TestALabelNeedsNoQuoting(t *testing.T) {
	record := session("a name with spaces", agentnotify.Working, nine)
	for _, argument := range painted(bar(), []agentnotify.Record{record}) {
		if label, is := strings.CutPrefix(argument, "label="); is {
			if strings.HasPrefix(label, "'") {
				t.Errorf("a label was quoted: %q — it is one argv element to an exec, "+
					"and the quotes would be part of the text", argument)
			}
		}
	}
}

func TestRemoveTakesEverythingOff(t *testing.T) {
	line := joined(Remove(bar()))
	for _, want := range []string{"agent.away", "agent.working", "agent.working.row.2", "agent.idle"} {
		if !strings.Contains(line, "--remove "+want) {
			t.Errorf("%s is left on the bar:\n%s", want, line)
		}
	}
}

// TestTheCountersCanBePutSomewhere is the reason `before` exists at all: a
// sketchybar side is ordered by the order things were added to it, and this
// display is not there when your sketchybarrc runs — the session-watcher starts
// it afterwards. Without a neighbour to name, everything it owns lands at the
// far end of its side, which for somebody whose bar already has a layout is the
// wrong end.
func TestTheCountersCanBePutSomewhere(t *testing.T) {
	placed := bar()
	placed.Before = "sep_agents"

	argv := strings.Join(painted(placed, nil), " ")
	first := placed.item(Painted()[0])
	second := placed.item(Painted()[1])
	if !strings.Contains(argv, "--move "+first+" before sep_agents") {
		t.Errorf("the first counter was not put before the neighbour:\n%s", argv)
	}
	// The rest follow it, so the group keeps its rank order however often this
	// runs.
	if !strings.Contains(argv, "--move "+second+" after "+first) {
		t.Errorf("the counters did not stay in order:\n%s", argv)
	}
}

func TestWithNoNeighbourNothingIsMoved(t *testing.T) {
	if argv := strings.Join(painted(bar(), nil), " "); strings.Contains(argv, "--move") {
		t.Errorf("something was moved without being asked:\n%s", argv)
	}
}

// TestTheChipIsOpaque is the bug a screenshot found: a popup with no background
// of its own is transparent, so the rows were drawn perfectly over a terminal
// full of text and could not be read at all.
func TestTheChipIsOpaque(t *testing.T) {
	opaque := bar()
	opaque.Popup = Popup{Background: "0xf2191724", Border: "0xff524f67", Radius: 10}

	argv := strings.Join(painted(opaque, []agentnotify.Record{
		session("one", agentnotify.Working, nine)}), " ")
	for _, want := range []string{
		"popup.background.drawing=on",
		"popup.background.color=0xf2191724",
		"popup.background.corner_radius=10",
		"popup.background.border_width=1",
		"popup.background.border_color=0xff524f67",
	} {
		if !strings.Contains(argv, want) {
			t.Errorf("the chip does not set %s:\n%s", want, argv)
		}
	}
}

// TestAChipWithNoBorderHasNoBorder rather than a black one, which is what an
// empty colour would otherwise paint.
func TestAChipWithNoBorderHasNoBorder(t *testing.T) {
	plain := bar()
	plain.Popup = Popup{Background: "0xff000000"}

	argv := strings.Join(painted(plain, nil), " ")
	if !strings.Contains(argv, "popup.background.border_width=0") {
		t.Errorf("a chip with no border colour still drew one:\n%s", argv)
	}
	if strings.Contains(argv, "border_color") {
		t.Errorf("a border colour was set from nothing:\n%s", argv)
	}
}

// TestAChipCanBeLeftAlone: somebody whose bar already styles its popups
// globally can turn this off entirely by naming no background.
func TestAChipCanBeLeftAlone(t *testing.T) {
	if argv := strings.Join(painted(bar(), nil), " "); strings.Contains(argv, "popup.background") {
		t.Errorf("a chip painted a background nobody asked for:\n%s", argv)
	}
}

// TestAHoverScriptCannotBeMadeToDoAnything is the same assertion, proved rather
// than reasoned about: the script is built, run by a real /bin/sh, and what it
// would have executed is checked afterwards.
//
// It is here because the preview is the one place in this system where text an
// agent wrote is put inside a shell command. §A15 says a shell would turn a
// pane name into a command; this is the boundary that says otherwise.
func TestAHoverScriptCannotBeMadeToDoAnything(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	log := filepath.Join(dir, "argv")
	stub := filepath.Join(dir, "sketchybar")
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\"; done >> " + log + "\n"
	if err := os.WriteFile(stub, []byte(script), 0o700); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	nasty := session("innocent", agentnotify.Working, nine)
	// Short on purpose: the preview wraps and then stops at its depth, and a
	// payload that fell off the end would prove nothing. It runs in the
	// directory the marker is in, so the whole attack fits on two lines.
	nasty.Message = "'; touch ran; echo ' `touch ran` $(touch ran) $HOME ~"

	shell := bar()
	shell.Sketchybar = stub
	run := exec.Command("/bin/sh", "-c", shell.hover(agentnotify.Working, nasty))
	run.Dir = dir
	run.Env = append(os.Environ(), "SENDER=mouse.entered")
	if out, err := run.CombinedOutput(); err != nil {
		t.Fatalf("the hover script would not run: %v\n%s", err, out)
	}

	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the agent's message EXECUTED: everything in this file is wrong")
	}
	argv, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("the stub was never called: %v", err)
	}
	if !strings.Contains(string(argv), "$(touch") || !strings.Contains(string(argv), "$HOME") {
		t.Errorf("the message did not arrive as the text it is:\n%s", argv)
	}
}
