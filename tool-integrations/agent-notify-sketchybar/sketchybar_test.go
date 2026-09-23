package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	agentnotify "github.com/lassoColombo/agent-notify"
)

// These run against the real sketchybar on this machine, under their own item
// prefix and cleaned up afterwards. Everything this program knows about
// sketchybar is a fact about a program, and a fake would only confirm what this
// file already believes.

func realSketchybar(t *testing.T) Sketchybar {
	t.Helper()
	// A test runs in a shell, which is the one context where looking sketchybar
	// up is right — and exactly why the program no longer does it (D-67).
	binary, err := exec.LookPath("sketchybar")
	if err != nil {
		t.Skipf("no sketchybar here: %v", err)
	}
	bar := Sketchybar{Binary: binary, Timeout: 5 * time.Second}
	if _, err := bar.Do([]string{"--query", "bar"}); err != nil {
		t.Skipf("sketchybar is not running: %v", err)
	}
	return bar
}

// items is what the bar is holding, as a set.
//
// It retries, because a query issued immediately after a large batch comes back
// EMPTY with exit 0 — measured on v2.24.0, and gone again 200ms later. Nothing
// in this display queries, so it costs production nothing; it is a trap only
// for a test that looks at its own work too quickly.
func items(t *testing.T, bar Sketchybar) map[string]bool {
	t.Helper()
	var queried struct {
		Items []string `json:"items"`
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		out, err := exec.Command(bar.Binary, "--query", "bar").Output()
		if err == nil && json.Unmarshal(out, &queried) == nil && len(queried.Items) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the bar never answered a query")
		}
		time.Sleep(100 * time.Millisecond)
	}
	held := map[string]bool{}
	for _, one := range queried.Items {
		held[one] = true
	}
	return held
}

func testBar(t *testing.T, bar Sketchybar) Bar {
	t.Helper()
	under := bar
	t.Cleanup(func() { _, _ = under.Do(Remove(testPrefix(t))) })
	return testPrefix(t)
}

func testPrefix(t *testing.T) Bar {
	return Bar{
		Prefix: "antest", Position: "right", Rows: 3,
		Sketchybar: "/opt/homebrew/bin/sketchybar", Core: "/usr/bin/true",
		Glyphs:  agentnotify.NewPalette(DefaultGlyphs, nil),
		Colours: agentnotify.NewPalette(DefaultColours, nil),
		Now:     time.Now(),
	}
}

// TestARealBarTakesTheRenderAndGivesItBack is the round trip: everything this
// renders exists afterwards, says what it was told to say, and comes off again.
func TestARealBarTakesTheRenderAndGivesItBack(t *testing.T) {
	sketchybar := realSketchybar(t)
	bar := testBar(t, sketchybar)

	complaints, err := sketchybar.Do(painted(bar, []agentnotify.Record{
		session("alpha", agentnotify.BlockedOnYou, time.Now().Add(-4*time.Minute-30*time.Second)),
		session("beta", agentnotify.Working, time.Now()),
	}))
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if len(complaints) != 0 {
		t.Errorf("sketchybar complained: %v", complaints)
	}

	held := items(t, sketchybar)
	for _, want := range []string{"antest.away", "antest.blocked-on-you", "antest.working",
		"antest.blocked-on-you.row.0"} {
		if !held[want] {
			t.Errorf("%s is not on the bar", want)
		}
	}

	queried := query(t, sketchybar, "antest.blocked-on-you")
	if queried.Label.Value != "1" {
		t.Errorf("the counter says %q, want 1", queried.Label.Value)
	}
	if queried.Geometry.Drawing != "on" {
		t.Errorf("the counter is not drawn")
	}
	if !strings.Contains(queried.Scripting.Script, "mouse.entered") {
		t.Errorf("the counter has no hover script: %q", queried.Scripting.Script)
	}

	row := query(t, sketchybar, "antest.blocked-on-you.row.0")
	if !strings.Contains(row.Label.Value, "alpha") || !strings.Contains(row.Label.Value, "4m") {
		t.Errorf("the row says %q, want the name and the age", row.Label.Value)
	}
	if !strings.Contains(row.Scripting.ClickScript, "focus-session") {
		t.Errorf("the row does not focus on click: %q", row.Scripting.ClickScript)
	}

	// An empty state keeps its place and is not drawn.
	if empty := query(t, sketchybar, "antest.broke"); empty.Geometry.Drawing != "off" {
		t.Errorf("a state with nothing in it is being drawn")
	}
}

// TestRenderingTwiceIsFine: the second render adds items that already exist and
// sketchybar says so, which is informational and not a problem. A display that
// treated it as one would cry wolf on every change it ever made.
func TestRenderingTwiceIsFine(t *testing.T) {
	sketchybar := realSketchybar(t)
	bar := testBar(t, sketchybar)
	sessions := []agentnotify.Record{session("alpha", agentnotify.Working, time.Now())}

	if _, err := sketchybar.Do(painted(bar, sessions)); err != nil {
		t.Fatalf("first render: %v", err)
	}
	complaints, err := sketchybar.Do(painted(bar, sessions))
	if err != nil {
		t.Fatalf("second render: %v", err)
	}
	if len(complaints) != 0 {
		t.Errorf("the second render was reported as a problem: %v", complaints)
	}
}

// TestSettingSomethingThatIsNotThereIsAProblem is the other half of the same
// rule: sketchybar marks that one `[!]`, and this display does report it.
func TestSettingSomethingThatIsNotThereIsAProblem(t *testing.T) {
	sketchybar := realSketchybar(t)
	// Alone in a batch this also exits 1, which is the measured rule: the code
	// reports the last message only. The complaint is what is trusted.
	//
	// Tried more than once, because the bar is one daemon for the whole machine
	// and this test shares it with whatever else is painting — including, on the
	// machine this was written on, the real display. Under concurrent traffic a
	// message's complaint occasionally does not come back at all: measured, and
	// the reason the display treats a complaint as a hint to repair rather than
	// as the only chance it will get.
	var complaints []string
	for attempt := range 5 {
		if attempt > 0 {
			time.Sleep(150 * time.Millisecond)
		}
		var err error
		complaints, err = sketchybar.Do([]string{"--set", "antest_nothing_at_all", "label=x"})
		if err != nil {
			t.Fatalf("Do: %v", err)
		}
		if len(complaints) > 0 {
			break
		}
	}
	if len(complaints) != 1 || !strings.Contains(complaints[0], "not found") {
		t.Errorf("complaints = %v, want the one sketchybar made", complaints)
	}
}

// TestRemoveTakesEverythingOffARealBar.
func TestRemoveTakesEverythingOffARealBar(t *testing.T) {
	sketchybar := realSketchybar(t)
	bar := testBar(t, sketchybar)

	if _, err := sketchybar.Do(painted(bar, []agentnotify.Record{
		session("alpha", agentnotify.Working, time.Now()),
	})); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if _, err := sketchybar.Do(Remove(bar)); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	for name := range items(t, sketchybar) {
		if strings.HasPrefix(name, "antest") {
			t.Errorf("%s is still on the bar", name)
		}
	}
}

type queried struct {
	Label struct {
		Value string `json:"value"`
	} `json:"label"`
	Geometry struct {
		Drawing string `json:"drawing"`
	} `json:"geometry"`
	Scripting struct {
		Script      string `json:"script"`
		ClickScript string `json:"click_script"`
	} `json:"scripting"`
}

func query(t *testing.T, bar Sketchybar, name string) queried {
	t.Helper()
	var answer queried
	deadline := time.Now().Add(5 * time.Second)
	for {
		out, err := exec.Command(bar.Binary, "--query", name).Output()
		if err == nil && json.Unmarshal(out, &answer) == nil && answer.Geometry.Drawing != "" {
			return answer
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s never answered a query", name)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// TestAWholesaleRefusalIsNotSilence is the failure M14 actually hit, and the
// worst kind: the display ran, sketchybar exited non-zero, said why in its own
// words rather than against any message, and nothing anywhere noticed.
//
// `sketchybar` needs USER set to find its per-user service. A supervised child
// is given a deliberately short environment, USER was not on the list, and the
// bar simply never drew anything (D-43).
func TestAWholesaleRefusalIsNotSilence(t *testing.T) {
	// A test runs in a shell, which is the one context where looking sketchybar
	// up is right — and exactly why the program no longer does it (D-67).
	binary, err := exec.LookPath("sketchybar")
	if err != nil {
		t.Skipf("no sketchybar here: %v", err)
	}
	bar := Sketchybar{Binary: binary, Timeout: 5 * time.Second}

	// Reproduce it exactly: no USER in the environment of the child.
	for _, name := range []string{"USER", "LOGNAME"} {
		t.Setenv(name, "")
		os.Unsetenv(name)
	}
	_, err = bar.Do([]string{"--query", "bar"})
	if err == nil {
		t.Skip("this sketchybar does not need USER, so there is nothing to prove here")
	}
	if !strings.Contains(err.Error(), "refused") {
		t.Errorf("the error does not say sketchybar refused wholesale: %v", err)
	}
}
