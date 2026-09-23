package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	agentnotify "github.com/lassoColombo/agent-notify"
)

var nine = time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)

func bar() Bar {
	return Bar{
		Rows:    3,
		Resting: "0xff8c88a6",
		Glyphs: agentnotify.NewPalette(map[agentnotify.Kernel]string{
			agentnotify.BlockedOnYou:  "!",
			agentnotify.FinishedATurn: "+",
			agentnotify.Working:       "*",
		}, nil),
		Colours:  agentnotify.NewPalette(DefaultColours, nil),
		Now:      nine.Add(4 * time.Minute),
		Announce: 8 * time.Second,
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

// title is the item's text as one string, which is what a person reads.
func title(shown WhatTheBarShows) string {
	var text strings.Builder
	for _, piece := range shown.Title {
		text.WriteString(piece.Text)
	}
	return text.String()
}

func rows(shown WhatTheBarShows) []MenuRow {
	var only []MenuRow
	for _, row := range shown.Menu {
		if row.Kind == Row {
			only = append(only, row)
		}
	}
	return only
}

func find(t *testing.T, paint WhatTheBarShows, name string) MenuRow {
	t.Helper()
	for _, item := range paint.Menu {
		if item.Text == name {
			return item
		}
	}
	t.Fatalf("no row called %q in %v", name, paint.Menu)
	return MenuRow{}
}

// marks is every alien on the item, which is what nearly every test about the
// bar is about.
func marks(t *testing.T, paint WhatTheBarShows) []TitlePiece {
	t.Helper()
	var only []TitlePiece
	for _, span := range paint.Title {
		if span.Symbol == "" {
			continue
		}
		if span.Symbol != Invader {
			t.Fatalf("the item is wearing %q, want the invader", span.Symbol)
		}
		only = append(only, span)
	}
	if len(only) == 0 {
		t.Fatal("the item has no mark on it at all")
	}
	return only
}

// TestOneAlienPerStateThatHasAnybodyInIt, most urgent first, each in its own
// colour — and a state with nothing in it is simply not there.
func TestOneAlienPerStateThatHasAnybodyInIt(t *testing.T) {
	paint, _ := Render(bar(), []agentnotify.Record{
		session("one", agentnotify.Working, nine),
		session("two", agentnotify.Working, nine),
		session("three", agentnotify.BlockedOnYou, nine),
	})
	on := marks(t, paint)
	if len(on) != 2 {
		t.Fatalf("the item has %d marks on it, want one per live state", len(on))
	}
	if on[0].Colour != DefaultColours[agentnotify.BlockedOnYou] {
		t.Errorf("the first mark is %q, want blocked-on-you in front", on[0].Colour)
	}
	if on[1].Colour != DefaultColours[agentnotify.Working] {
		t.Errorf("the second mark is %q, want working", on[1].Colour)
	}
	// Two agents in a state is still one alien. How many is in the tooltip.
	if paint.Tooltip != "agent-notify — 1 blocked on you, 2 working" {
		t.Errorf("the tooltip says %q", paint.Tooltip)
	}
}

// TestOnlyTheStatesWaitingOnYouFlicker. Working is the state the bar is in most
// of the day and it is not a request: a mark that flickered through it would be
// a light nobody could work next to.
func TestOnlyTheStatesWaitingOnYouFlicker(t *testing.T) {
	flickers := map[agentnotify.Kernel]bool{
		agentnotify.BlockedOnYou:  true,
		agentnotify.Broke:         true,
		agentnotify.FinishedATurn: true,
		agentnotify.Working:       false,
		agentnotify.Idle:          false,
	}
	for kernel, want := range flickers {
		paint, _ := Render(bar(), []agentnotify.Record{session("one", kernel, nine)})
		if got := marks(t, paint)[0].Flicker; got != want {
			t.Errorf("%s flickers = %v, want %v", kernel, got, want)
		}
	}
}

// TestOneStateAsksAndTheOthersSitStill is the reason flickering is a property
// of a piece and not of the item.
func TestOneStateAsksAndTheOthersSitStill(t *testing.T) {
	paint, _ := Render(bar(), []agentnotify.Record{
		session("ask", agentnotify.BlockedOnYou, nine),
		session("busy", agentnotify.Working, nine),
		session("quiet", agentnotify.Idle, nine),
	})
	on := marks(t, paint)
	if len(on) != 3 {
		t.Fatalf("the item has %d marks, want three", len(on))
	}
	if !on[0].Flicker {
		t.Errorf("the agent waiting for you is not asking")
	}
	if on[1].Flicker || on[2].Flicker {
		t.Errorf("a mark for agents that want nothing is flickering: %v", on[1:])
	}
}

// TestNothingRunningIsStillOneAlien. An empty title is a few points of blank
// menu bar, which reads as the display having died.
func TestNothingRunningIsStillOneAlien(t *testing.T) {
	paint, _ := Render(bar(), nil)
	on := marks(t, paint)
	if len(on) != 1 || on[0].Colour != bar().Resting {
		t.Errorf("with nothing running the item is %v, want one resting mark", on)
	}
	if on[0].Flicker {
		t.Errorf("an empty machine is asking for somebody")
	}
	if len(paint.Menu) != 1 || paint.Menu[0].Kind != Note ||
		!strings.Contains(paint.Menu[0].Text, "no agents") {
		t.Errorf("the menu is %v, want one note", paint.Menu)
	}
	if paint.Tooltip != "agent-notify — nothing running" {
		t.Errorf("the tooltip says %q", paint.Tooltip)
	}
}

// TestTheMarkNeverChangesShape is the property the counting designs could not
// keep: whatever is happening, every mark on the item is the same sprite, so
// the bar is read by colour and by movement rather than by being decoded.
func TestTheMarkNeverChangesShape(t *testing.T) {
	for _, world := range [][]agentnotify.Record{
		nil,
		{session("ask", agentnotify.BlockedOnYou, nine)},
		{session("oops", agentnotify.Broke, nine)},
		{session("ready", agentnotify.FinishedATurn, nine)},
		{session("busy", agentnotify.Working, nine)},
		{session("quiet", agentnotify.Idle, nine)},
		{session("gone", agentnotify.Ended, nine)},
	} {
		paint, _ := Render(bar(), world)
		marks(t, paint) // fails unless every one of them is the invader
	}
}

// TestTheMarksAreSeparated. Two sprites with nothing between them read as one
// wider sprite.
func TestTheMarksAreSeparated(t *testing.T) {
	paint, _ := Render(bar(), []agentnotify.Record{
		session("ask", agentnotify.BlockedOnYou, nine),
		session("busy", agentnotify.Working, nine),
	})
	on := marks(t, paint)
	if on[0].Text != "" {
		t.Errorf("the first mark carries %q, and nothing comes before it", on[0].Text)
	}
	if on[1].Text != gap {
		t.Errorf("the second mark carries %q, want the gap in front of it", on[1].Text)
	}
}

// TestAnEndedSessionIsNotOnTheBar (D-26).
func TestAnEndedSessionIsNotOnTheBar(t *testing.T) {
	paint, _ := Render(bar(), []agentnotify.Record{
		session("gone", agentnotify.Ended, nine),
		session("here", agentnotify.Working, nine),
	})
	if on := marks(t, paint); len(on) != 1 {
		t.Errorf("the item has %d marks, want only the working one", len(on))
	}
	if len(rows(paint)) != 1 {
		t.Errorf("the menu lists %d sessions, want 1", len(rows(paint)))
	}
	if strings.Contains(paint.Tooltip, "ended") {
		t.Errorf("the tooltip counts an ended session: %q", paint.Tooltip)
	}
}

// TestTheMenuIsStatesThenSessions.
func TestTheMenuIsStatesThenSessions(t *testing.T) {
	paint, _ := Render(bar(), []agentnotify.Record{
		session("busy", agentnotify.Working, nine),
		session("ask", agentnotify.BlockedOnYou, nine),
	})
	var shape []string
	for _, item := range paint.Menu {
		shape = append(shape, string(item.Kind)+":"+item.Text)
	}
	want := []string{"section:blocked on you", "row:ask", "separator:", "section:working", "row:busy"}
	if strings.Join(shape, "|") != strings.Join(want, "|") {
		t.Errorf("the menu is %v,\n            want %v", shape, want)
	}
}

// TestARowSaysEverythingARowHasToSay.
func TestARowSaysEverythingARowHasToSay(t *testing.T) {
	record := session("alpha", agentnotify.BlockedOnYou, nine)
	record.Message = "Shall I delete the branch?"
	record.Detail = "permission-prompt"

	paint, _ := Render(bar(), []agentnotify.Record{record})
	row := find(t, paint, "alpha")

	if row.Key != record.Key.String() {
		t.Errorf("the row carries key %q, want %q", row.Key, record.Key.String())
	}
	if row.Age != "4m" {
		t.Errorf("the row says the age is %q, want 4m", row.Age)
	}
	if row.Glyph != "!" {
		t.Errorf("the row's glyph is %q", row.Glyph)
	}
	if row.Colour != DefaultColours[agentnotify.BlockedOnYou] {
		t.Errorf("the row's colour is %q", row.Colour)
	}
	// And what the agent SAID is deliberately not here: it goes in the
	// notification, which arrives when it is said.
	if strings.Contains(fmt.Sprint(row), "delete the branch") {
		t.Errorf("the row is carrying the message again: %+v", row)
	}
}

// TestSessionsAreOrderedByUrgencyInsideAState, which is core's order and not
// one written here (R24).
func TestSessionsAreOrderedByUrgencyInsideAState(t *testing.T) {
	paint, _ := Render(bar(), []agentnotify.Record{
		session("recent", agentnotify.BlockedOnYou, nine.Add(3*time.Minute)),
		session("waiting", agentnotify.BlockedOnYou, nine),
	})
	listed := rows(paint)
	if len(listed) != 2 || listed[0].Text != "waiting" {
		t.Errorf("the rows are %v, want the one that has been waiting longest first", listed)
	}
}

// TestALongStateSaysHowManyItIsNotShowing. Silence about the rest would read as
// "these are all of them".
func TestALongStateSaysHowManyItIsNotShowing(t *testing.T) {
	var many []agentnotify.Record
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		many = append(many, session(id, agentnotify.Working, nine))
	}
	paint, _ := Render(bar(), many)

	if got := len(rows(paint)); got != 3 {
		t.Errorf("the menu lists %d sessions, want the configured 3", got)
	}
	last := paint.Menu[len(paint.Menu)-1]
	if last.Kind != Note || last.Text != "and 2 more" {
		t.Errorf("the menu ends with %v, want a note saying two are missing", last)
	}
}

// TestAnAnnouncementIsAddedAndTakesNothingAway. The mark stays exactly where it
// is; the name of whatever just changed goes after it for a few seconds.
func TestAnAnnouncementIsAddedAndTakesNothingAway(t *testing.T) {
	board := bar()
	board.Now = nine.Add(2 * time.Second)
	paint, announcement := Render(board, []agentnotify.Record{
		session("busy", agentnotify.Working, nine.Add(-time.Hour)),
		session("alpha", agentnotify.BlockedOnYou, nine),
	})

	if !announcement.Announced() {
		t.Fatal("nothing was announced")
	}
	if got := marks(t, paint)[0].Colour; got != DefaultColours[agentnotify.BlockedOnYou] {
		t.Errorf("the first mark is %q", got)
	}
	if got := title(paint); got != gap+gap+"alpha" {
		t.Errorf("the item says %q, want the marks and then the name", got)
	}
	if !find(t, paint, "alpha").Lit {
		t.Errorf("the announced row is not lit")
	}
	if find(t, paint, "busy").Lit {
		t.Errorf("a row nobody announced is lit")
	}
}

// TestAnAnnouncementEndsOnTheRECORD'sClock (D-47). Two paints of the same
// records, three seconds apart, and the second is not announcing — because the
// deadline is state-since plus the window and no paint can move it.
func TestAnAnnouncementEndsOnTheRecordsClock(t *testing.T) {
	records := []agentnotify.Record{session("alpha", agentnotify.BlockedOnYou, nine)}

	early := bar()
	early.Announce = 5 * time.Second
	early.Now = nine.Add(time.Second)
	if _, announcement := Render(early, records); !announcement.Announced() {
		t.Fatal("nothing was announced a second in")
	}

	late := early
	late.Now = nine.Add(6 * time.Second)
	paint, announcement := Render(late, records)
	if announcement.Announced() {
		t.Errorf("still announcing six seconds into a five second window")
	}
	if got := title(paint); got != "" {
		t.Errorf("the item still says %q, want the mark on its own again", got)
	}
	if len(paint.Title) != 1 {
		t.Errorf("the item is in %d pieces", len(paint.Title))
	}
}

// TestWorkingDoesNotAnnounce: an agent getting on with it is the state the bar
// is in most of the day (D-47).
func TestWorkingDoesNotAnnounce(t *testing.T) {
	board := bar()
	board.Now = nine.Add(time.Second)
	if _, announcement := Render(board, []agentnotify.Record{
		session("busy", agentnotify.Working, nine),
		session("quiet", agentnotify.Idle, nine),
	}); announcement.Announced() {
		t.Errorf("announced %v", announcement)
	}
}

// TestTheMostRECENTIsAnnouncedNotTheWorst. A person looking up at an item that
// has just changed is asking what happened, not what is worst.
func TestTheMostRecentIsAnnouncedNotTheWorst(t *testing.T) {
	board := bar()
	board.Now = nine.Add(5 * time.Second)
	_, announcement := Render(board, []agentnotify.Record{
		session("worse", agentnotify.BlockedOnYou, nine),
		session("newer", agentnotify.FinishedATurn, nine.Add(2*time.Second)),
	})
	if !announcement.Announced() || announcement.Record.DisplayName() != "newer" {
		t.Errorf("announced %v, want the one that just happened", announcement)
	}
}

// TestAnnounceZeroKeepsTheItemPassive.
func TestAnnounceZeroKeepsTheItemPassive(t *testing.T) {
	board := bar()
	board.Announce = 0
	board.Now = nine.Add(time.Second)
	paint, announcement := Render(board, []agentnotify.Record{
		session("alpha", agentnotify.BlockedOnYou, nine),
	})
	if announcement.Announced() {
		t.Errorf("announced %v with the window turned off", announcement)
	}
	if len(paint.Title) != 1 {
		t.Errorf("the item is in %d pieces, want the mark on its own", len(paint.Title))
	}
	if !marks(t, paint)[0].Flicker {
		t.Errorf("the window is off, but an agent is still waiting and the mark is not asking")
	}
}

// TestWhatCrossesTheBoundaryIsJSON is the boundary itself: whatever Render
// returns has to survive being marshalled, because that is the only form the
// drawing side ever sees it in.
func TestWhatCrossesTheBoundaryIsJSON(t *testing.T) {
	record := session("alpha", agentnotify.BlockedOnYou, nine)
	record.Message = "it's \"quoted\", has a \\ in it, and\na newline"
	paint, _ := Render(bar(), []agentnotify.Record{record})

	encoded, err := json.Marshal(paint)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var back WhatTheBarShows
	if err := json.Unmarshal(encoded, &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if title(back) != title(paint) {
		t.Errorf("the title did not survive: %q", title(back))
	}
	if find(t, back, "alpha").Symbol != find(t, paint, "alpha").Symbol {
		t.Errorf("the row's symbol did not survive")
	}
}

// TestEveryStateIsADifferentColour, because every one of them can be on the bar
// at the same time as all the others, wearing the same sprite. Colour is the
// only thing that tells one alien from the next, so two states sharing a value
// is two states nobody can tell apart.
//
// This replaces the rule that came before it — only the states that want you
// are coloured — which was right for an item that showed one mark and wrong for
// one that shows several. What is rationed now is movement, not colour.
func TestEveryStateIsADifferentColour(t *testing.T) {
	seen := map[string]agentnotify.Kernel{}
	for _, kernel := range Painted() {
		colour := DefaultColours[kernel]
		if colour == "" {
			t.Errorf("%s has no colour", kernel)
			continue
		}
		if already, taken := seen[colour]; taken {
			t.Errorf("%s and %s are both %q", already, kernel, colour)
		}
		seen[colour] = kernel
	}
}

// TestNothingIsDrawnInAHarshColour. Rosé Pine is muted by design and a menu bar
// is translucent: a saturated hue sitting on one all day is a light nobody can
// stop noticing.
func TestNothingIsDrawnInAHarshColour(t *testing.T) {
	for kernel, colour := range DefaultColours {
		if saturation(t, colour) > 0.65 {
			t.Errorf("%s is drawn in %q, which is louder than this palette gets", kernel, colour)
		}
	}
}

// TestIdleIsTheTextColourDimmed, and not a colour of its own. The palette's own
// dim tones are built for an opaque background and come out darker than the
// system's secondary label on a translucent menu bar, which is where this
// started: an idle mark nobody could see.
func TestIdleIsTheTextColourDimmed(t *testing.T) {
	const text = "e0def4" // Rosé Pine's own foreground
	idle := DefaultColours[agentnotify.Idle]
	if idle[4:] != text {
		t.Errorf("idle is %q, want the text colour %s dimmed", idle, text)
	}
	if dim := alpha(t, idle); dim >= 1 || dim <= 0.7 {
		t.Errorf("idle is drawn at %.2f: quieter than the rest, and still legible on a bar "+
			"the desktop shows through", dim)
	}
}

// saturation is how much hue a `0xAARRGGBB` has, on the HSV definition: 0 for
// anything grey, whatever its brightness.
func saturation(t *testing.T, colour string) float64 {
	t.Helper()
	packed := packed(t, colour)
	red := float64((packed >> 16) & 0xff)
	green := float64((packed >> 8) & 0xff)
	blue := float64(packed & 0xff)
	high := max(red, max(green, blue))
	low := min(red, min(green, blue))
	if high == 0 {
		return 0
	}
	return (high - low) / high
}

func alpha(t *testing.T, colour string) float64 {
	t.Helper()
	return float64((packed(t, colour)>>24)&0xff) / 255
}

func packed(t *testing.T, colour string) uint64 {
	t.Helper()
	if len(colour) != 10 || !strings.HasPrefix(colour, "0x") {
		t.Fatalf("%q is not a 0xAARRGGBB colour", colour)
	}
	value, err := strconv.ParseUint(colour[2:], 16, 64)
	if err != nil {
		t.Fatalf("%q: %v", colour, err)
	}
	return value
}

// TestEverySymbolThisShipsWithExistsOnThisMac. A name from a newer SF Symbols
// release draws nothing at all — not a fallback, not a box, nothing — so the
// defaults have to be checked against the machine rather than assumed.
func TestEverySymbolThisShipsWithExistsOnThisMac(t *testing.T) {
	for kernel, name := range DefaultSymbols {
		if !SymbolKnown(name) {
			t.Errorf("%s is drawn with %q, which this macOS does not have", kernel, name)
		}
	}
}

// TestEveryPaintedStateHasASymbol, because a row with no icon in a menu where
// every other row has one reads as a row that is broken.
func TestEveryPaintedStateHasASymbol(t *testing.T) {
	for _, kernel := range Painted() {
		if DefaultSymbols[kernel] == "" {
			t.Errorf("%s has no symbol", kernel)
		}
	}
}

// TestTheDocumentCarriesSymbolsAndTheirFallback. The glyph stays in the
// document beside the symbol: a symbol that does not resolve must leave
// something behind.
func TestTheDocumentCarriesSymbolsAndTheirFallback(t *testing.T) {
	board := bar()
	board.Symbols = agentnotify.NewPalette(DefaultSymbols, nil)
	paint, _ := Render(board, []agentnotify.Record{
		session("alpha", agentnotify.Working, nine),
	})

	if got := paint.Title[0].Symbol; got != Invader {
		t.Errorf("the item's symbol is %q", got)
	}
	row := find(t, paint, "alpha")
	if row.Symbol != DefaultSymbols[agentnotify.Working] {
		t.Errorf("the row's symbol is %q", row.Symbol)
	}
	if row.Glyph == "" {
		t.Errorf("the row kept no text fallback for a symbol that does not resolve")
	}
}
