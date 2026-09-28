// Codex names its own threads, and this is how a hook reads that name back.
//
// It is in this program rather than in core because every word of it is
// codex's: the file, its shape, and the one field in it worth repeating (R10).
// Core is handed a string and never learns where it came from.
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/lassoColombo/agent-notify/hook"
)

// The hook payload does not carry the name, so it is read out of band from the
// index codex keeps beside its rollouts:
//
//	~/.codex/session_index.jsonl
//	{"id":"01a0bb7f-…","thread_name":"Count to 12","updated_at":"2026-09-19T21:09:44.002592Z"}
//
// The name itself lives in `threads.name` in ~/.codex/state_5.sqlite, and this
// file is an exact projection of it — [verified 2026-09-19, 0.154.0] same ids,
// same strings, every one. The projection is what is read rather than the
// database, for three reasons that all point the same way: the database's
// filename carries its schema generation (`state_5`, beside `logs_2` and
// `memories_1`) and so is guaranteed to move; `name` is one of the columns
// migrations bolted on late, in a schema still visibly in motion; and reading
// it means a sqlite driver in every adapter binary and a WAL-mode open of
// somebody else's database while they are writing to it. A two-field text file
// with a stable name is the better door to the same string.
//
// [verified 2026-09-19, 0.154.0] by driving a real codex TUI and watching both:
// the thread's row appears the instant a prompt is sent, its title a second
// later, and **its name about four seconds in** — mid-turn, so every hook after
// it carries the name. A line is appended here at that same moment.
//
// One thing codex does not name: a headless `codex exec` thread. Its row is
// written with a null name and it never reaches this file, so those sessions
// show the working directory. That is right rather than a gap — a one-shot with
// no conversation has nothing to be named after.
const (
	// codexHomeVariable relocates the whole of ~/.codex, the same override
	// install.go honours for the config file.
	codexHomeVariable = "CODEX_HOME"
	// indexFile is the index, relative to that home.
	indexFile = "session_index.jsonl"
	// oneReadWorthOfIndex is how much is read at a time, working backwards from
	// the end. A line is about a hundred bytes, so the first read covers the
	// last six hundred threads to be named and almost always ends the search.
	oneReadWorthOfIndex = 64 << 10
	// asFarBackAsItIsWorthGoing stops the search on a file that has grown past
	// anything a person could have produced — about forty thousand threads.
	// Without it a corrupt or enormous index would be read in full on the path
	// the agent is waiting on.
	asFarBackAsItIsWorthGoing = 4 << 20
)

// codexThread is the part of one index line this program reads.
type codexThread struct {
	ID   string `json:"id"`
	Name string `json:"thread_name"`
}

// WhatCodexCallsThisThread is the name to report, or nothing at all.
//
// Nothing at all is an ordinary answer and never an error: a codex too old to
// keep the index, a thread it has not named yet, a `codex exec` it will never
// name. Core treats an empty name as "leave the stored one alone" and falls
// back to the working directory, which is what it did before this existed.
//
// The search runs backwards from the end of the file, a read at a time, and
// that direction is the whole design. Entries are in the order threads were
// *named* — four seconds into a first turn, and never rewritten afterwards —
// so the newest names are at the end, and the thread a hook is firing for is
// usually within the last read. Usually, not always: a session left open while
// six hundred others are started is pushed off the end of that read by threads
// named since, and it is exactly the long-lived session whose name is most
// worth having. So a read that does not find it moves to the one before it.
// The first read is a shortcut, not the extent of the search.
func WhatCodexCallsThisThread(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ""
	}
	// The id as it appears in the file, so that a line can be dismissed without
	// being parsed.
	wanted := []byte(`"` + sessionID + `"`)
	var name string
	_ = hook.ReadBackwards(whereCodexKeepsItsIndex(), oneReadWorthOfIndex, asFarBackAsItIsWorthGoing,
		func(line []byte) bool {
			if !bytes.Contains(line, wanted) {
				return true
			}
			found, itIsThisThread := nameInLine(line, sessionID)
			if itIsThisThread {
				name = found
			}
			return !itIsThisThread
		})
	return name
}

// nameInLine reads one line of the index, and says whether it is about the
// thread the hook is describing.
//
// Codex is appending to this file while this runs, and the line may be a
// fragment or garbage; neither is worth more than moving on to the next one.
func nameInLine(line []byte, sessionID string) (name string, itIsThisThread bool) {
	var thread codexThread
	if err := json.Unmarshal(line, &thread); err != nil {
		return "", false
	}
	if thread.ID != sessionID {
		return "", false
	}
	return strings.TrimSpace(thread.Name), true
}

// whereCodexKeepsItsIndex is ~/.codex/session_index.jsonl, or wherever the user
// moved the whole of codex's home to.
func whereCodexKeepsItsIndex() string {
	if home := strings.TrimSpace(os.Getenv(codexHomeVariable)); home != "" {
		return filepath.Join(home, indexFile)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex", indexFile)
}
