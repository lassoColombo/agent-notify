package main

import (
	"fmt"
	"strings"

	"github.com/lassoColombo/agent-notify/subscribe"
)

// Settings is `[integration.picker.settings]`, and nothing else in the file is
// ours. A key nobody declared is refused by name rather than ignored
// (subscribe.Settings), because a misspelled colour that changes nothing and
// says nothing is the config bug people give up on.
type Settings struct {
	Colours Colours `toml:"colors"`
	Keymap  Keymap  `toml:"keys"`
}

// Keymap is `[integration.picker.settings.keys]`, and it is the colours table
// the other way round.
//
// A hue is the user's and where it is spent is this program's; a KEY is the
// user's and what there is to ask for is this program's. So the table is keyed
// on the action — eight of them, declared, so that a misspelled `preivew-down`
// is refused by name rather than binding nothing — and never on the key, which
// would be an open namespace with no way to catch a typo and an invitation to
// bind fzf actions this picker does not keep. `toggle-sort` is the sharpest of
// those: core computes one urgency order for every display (§A5.5), and a list
// that re-sorts moves the row somebody is about to press enter on.
//
// A value is one key or a list of them, and it REPLACES the default rather
// than adding to it — that is how a key is given back. Underneath, fzf's own
// defaults still stand for anything this program does not bind: take ctrl-d off
// `preview-page-down` and it is fzf's "quit on an empty query" again, which is
// exactly what the README says binding it cost.
type Keymap struct {
	Down            Keys `toml:"down"`
	Up              Keys `toml:"up"`
	PreviewDown     Keys `toml:"preview-down"`
	PreviewUp       Keys `toml:"preview-up"`
	PreviewPageDown Keys `toml:"preview-page-down"`
	PreviewPageUp   Keys `toml:"preview-page-up"`
	Choose          Keys `toml:"choose"`
	Leave           Keys `toml:"leave"`
}

// Keys is what asks for one action: `"ctrl-j"`, or `["down", "ctrl-n"]` where
// two things ask for the same one. `""` and `[]` are the third spelling and
// they mean the same as each other — nothing asks for it here, so the key goes
// back to whatever fzf does with it.
//
// What a key is CALLED is not checked anywhere in this program. See
// TheKeysWeShippedWith for why fzf is asked instead.
type Keys []string

// UnmarshalText is the one-key spelling. A list needs no help — it is a slice
// of strings and decodes as one — so between them both spellings work, and the
// empty string lands here as a table that was written and says nothing.
func (k *Keys) UnmarshalText(text []byte) error {
	if len(text) == 0 {
		*k = Keys{}
		return nil
	}
	*k = Keys{string(text)}
	return nil
}

// DefaultKeys is what asks for each action when nobody has said otherwise, and
// it is today's behaviour written down rather than inherited.
//
// down and up were fzf's own defaults minus whatever this program had taken —
// "ctrl-j and ctrl-k also move the list, except here, where they scroll the
// preview" is a thing nobody can read off a source file. The four that scroll
// the message are the four nearest the home row, because the list is three rows
// and the message under it can be a hundred.
var DefaultKeys = Keymap{
	Down:            Keys{"down", "ctrl-n"},
	Up:              Keys{"up", "ctrl-p"},
	PreviewDown:     Keys{"ctrl-j"},
	PreviewUp:       Keys{"ctrl-k"},
	PreviewPageDown: Keys{"ctrl-d"},
	PreviewPageUp:   Keys{"ctrl-u"},
	Choose:          Keys{"enter"},
	Leave:           Keys{"esc", "ctrl-c"},
}

// binding is one row of that table: this program's word for the action, fzf's
// word for it, and the keys in force.
type binding struct {
	action string
	fzf    string
	keys   *Keys
}

// bindings pairs every declared action with what fzf calls it, in one place, so
// that the table, the defaults and what fzf is told cannot drift apart.
//
// The two vocabularies differ on purpose in one place: `preview-page-down` is
// fzf's `preview-half-page-down`, because half a page per press is what it does
// and what the name has to carry is the contrast with a line.
func (m *Keymap) bindings() []binding {
	return []binding{
		{"down", "down", &m.Down},
		{"up", "up", &m.Up},
		{"preview-down", "preview-down", &m.PreviewDown},
		{"preview-up", "preview-up", &m.PreviewUp},
		{"preview-page-down", "preview-half-page-down", &m.PreviewPageDown},
		{"preview-page-up", "preview-half-page-up", &m.PreviewPageUp},
		{"choose", "accept", &m.Choose},
		{"leave", "abort", &m.Leave},
	}
}

// Colours is `[integration.picker.settings.colors]`: the twelve hues this
// program paints with, by base24 slot.
//
// WHAT a slot is, is the user's — it is the one thing about a terminal's
// palette that only they can know, which is the test §A14 sets. WHERE a slot is
// spent is theme.go's and is not in this file at all. So a person who moves
// base09 moves `broke` in the list and a string inside a fenced code block,
// together and in one line; the README's "one hue one meaning" section is the
// list of what each one carries.
//
// The slots are named rather than hue-worded because the hue words lie on this
// machine: the "blue" slot is iris, which is purple, and base0A is annotated
// ANSI yellow while being rose. A slot name is also what scheme.yaml is written
// in, so this table is a paste from the file a person already keeps.
//
// There are twelve because twelve is what gets painted. A slot nothing spends
// is not offered, since a key that changes nothing is the same silence the
// strictness above exists to break.
type Colours struct {
	Base02 string `toml:"base02"`
	Base03 string `toml:"base03"`
	Base04 string `toml:"base04"`
	Base05 string `toml:"base05"`
	Base08 string `toml:"base08"`
	Base09 string `toml:"base09"`
	Base0A string `toml:"base0A"`
	Base0B string `toml:"base0B"`
	Base0C string `toml:"base0C"`
	Base0D string `toml:"base0D"`
	Base13 string `toml:"base13"`
	Base14 string `toml:"base14"`
}

// slot is one line of that table: what it is called, what the user wrote, and
// the variable it sets.
type slot struct {
	key   string
	given string
	into  *string
}

// slots pairs every declared key with the variable it paints, in one place, so
// that the table, the validation and the assignment cannot drift apart.
func (c Colours) slots() []slot {
	return []slot{
		{"base02", c.Base02, &base02},
		{"base03", c.Base03, &base03},
		{"base04", c.Base04, &base04},
		{"base05", c.Base05, &base05},
		{"base08", c.Base08, &base08},
		{"base09", c.Base09, &base09},
		{"base0A", c.Base0A, &base0A},
		{"base0B", c.Base0B, &base0B},
		{"base0C", c.Base0C, &base0C},
		{"base0D", c.Base0D, &base0D},
		{"base13", c.Base13, &base13},
		{"base14", c.Base14, &base14},
	}
}

// keymap is the keys in force, and rebound is whether any of them came out of
// the config file — which is what decides, later, whether an fzf refusal is
// worth a second try with this program's own.
var (
	keymap  = DefaultKeys
	rebound bool
)

// Read reads this integration's settings and applies them: the hues to the
// styles, the keys to what fzf is told. It never fails.
//
// What it answers with instead is a list of complaints, which the window then
// carries in its header. That is §A14's rule — a malformed configuration must
// never break the thing — applied to the one program here that is opened by a
// keypress: `close_on_exit true` means a picker that refuses to start is a
// floating pane which appears and vanishes, and that is indistinguishable from
// a crash. A wrong colour is a thing you can see and fix; a pane that flashes
// is a thing you file a bug about.
//
// It is called once, in main, before the three jobs below it are dispatched.
// All three are separate processes — the list, a preview, a label — and a
// resolve that happened inside only one of them would draw the rows in your
// palette and the pane under them in mine. One read of the file for both
// tables, rather than one each.
func Read(given subscribe.Integration) []string {
	var settings Settings
	if err := given.Settings(&settings); err != nil {
		// A table that could not be decoded at all leaves every default
		// standing: a picker in the wrong colours, not a picker that will
		// not open.
		return []string{oneLine(err.Error())}
	}
	return append(repaint(settings.Colours), rebind(settings.Keymap)...)
}

// repaint lays the user's hues over this program's defaults.
func repaint(colours Colours) []string {
	var complaints []string
	for _, slot := range colours.slots() {
		switch {
		case slot.given == "":
			// An absent key is not an opinion, and the default stands.
		case !isHue(slot.given):
			complaints = append(complaints, fmt.Sprintf(
				"[integration.%s.settings.colors] %s = %q is not a colour, "+
					"so %s is still %s", Name, slot.key, slot.given, slot.key, *slot.into))
		default:
			*slot.into = strings.ToLower(slot.given)
		}
	}
	restyle()
	return complaints
}

// rebind lays the user's keys over this program's, and refuses the table whole
// rather than in part.
//
// A keymap is one object. Half of somebody's — the half that did not collide —
// is a window where some keys do what they asked and some do what this program
// asked, which is worse to sit in front of than either, and it is the same
// argument the stylesheet makes about a window half in your palette.
func rebind(given Keymap) []string {
	inForce := DefaultKeys
	rebound = false
	wanted, into := given.bindings(), inForce.bindings()
	for i := range wanted {
		// Written at all is the ask, which is why this is `!= nil` rather than
		// a length: an empty list is somebody taking a key off an action and
		// leaving it to fzf, and an absent key is somebody with no opinion.
		if *wanted[i].keys != nil {
			*into[i].keys = *wanted[i].keys
			rebound = true
		}
	}

	// A key asked to do two things does one of them, silently, and which one is
	// fzf's business rather than anybody's intention. Said out loud instead —
	// and it catches the ordinary version of this, which is a key moved onto an
	// action while the action that had it was left where it was.
	taken := map[string]string{}
	for _, one := range inForce.bindings() {
		for _, key := range *one.keys {
			if other, already := taken[key]; already {
				keymap, rebound = DefaultKeys, false
				return []string{fmt.Sprintf(
					"[integration.%s.settings.keys] %s is asked to be both %s and %s, "+
						"so the keys this program shipped with are the ones in force",
					Name, key, other, one.action)}
			}
			taken[key] = one.action
		}
	}

	keymap = inForce
	return nil
}

// fzfBinds is what fzf is told about keys: `key:action`, every key of every
// action, in this program's fixed order.
//
// The machinery binds are not in here and must not be. `focus` redraws the
// preview's label and is not a key at all; keeping it in its own --bind is what
// makes it impossible for somebody's table to take it away.
func fzfBinds() string {
	binds := make([]string, 0, len(keymap.bindings()))
	for _, one := range keymap.bindings() {
		for _, key := range *one.keys {
			binds = append(binds, key+":"+one.fzf)
		}
	}
	return strings.Join(binds, ",")
}

// TheKeysWeShippedWith puts this program's own keys back and says why, in the
// words fzf refused the others in.
//
// A key NAME is fzf's vocabulary — ctrl-j, alt-up, f13, the lot — and a second
// copy of it here would be a copy that drifts from the library this program is
// built against. So the binds go over as they were written and fzf's answer is
// the validation: what it will not take is dropped for what this program
// shipped with, and the window opens saying so. Nobody presses a key and gets
// nothing back.
func TheKeysWeShippedWith(refusal error) string {
	keymap, rebound = DefaultKeys, false
	return fmt.Sprintf("[integration.%s.settings.keys] %s — keeping the keys "+
		"this program shipped with", Name, oneLine(refusal.Error()))
}

// isHue is `#rrggbb`, and nothing else.
//
// fzf and lipgloss each accept more than that — a colour number, a name, an
// ANSI index — and the single spelling is the point rather than a limitation:
// every one of these values is also handed to glamour, which takes fewer of
// them, and a table where one line says "#eb6f92" and the next says "red" is a
// table nobody can read as a palette.
func isHue(value string) bool {
	if len(value) != 7 || value[0] != '#' {
		return false
	}
	for _, digit := range value[1:] {
		if !strings.ContainsRune("0123456789abcdefABCDEF", digit) {
			return false
		}
	}
	return true
}

// Complaint is what the window says about a configuration it could not use in
// full: the first thing wrong with it, and how much else there was.
//
// One line rather than all of them, because the header is drawn above the list
// and a file with a dozen typos in it would otherwise push the sessions off the
// screen — which is the picker refusing to open by another route.
func Complaint(complaints []string) string {
	switch len(complaints) {
	case 0:
		return ""
	case 1:
		return complaints[0]
	}
	return fmt.Sprintf("%s (and %d more)", complaints[0], len(complaints)-1)
}
