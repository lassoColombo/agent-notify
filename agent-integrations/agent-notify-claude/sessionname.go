// Claude names its own sessions and remembers where each one started, and this
// is how a hook reads both back.
//
// It is in this program rather than in core because every word of it is
// Claude's: the file and its fields (R10). Core is handed a name and a
// directory and never learns where they came from.
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// The hook payload carries the name on two hooks of the nine, and only when
// somebody gave the session one. [verified 2026-09-19, 2.1.236] Every hook
// input Claude built was spread from the same six fields — session_id,
// transcript_path, cwd, prompt_id, permission_mode and the agent pair — and the
// name was not among them.
//
// [verified 2026-10-09, 2.1.285] Since 2026-09-20 `UserPromptSubmit` and
// `SessionStart` carry `session_title`: 910 of 47,742 captured payloads have
// one, on those two hooks and no other, and it is only ever a title somebody
// gave — a `/rename`, `--name`, or a hook's `sessionTitle`. Translate reads it
// first (D-88). That it arrives on a prompt and not on the tool call after it
// costs nothing, because an empty name leaves the stored one alone; and it is
// the only place a title given by a SessionStart hook ever appears, because that
// one never reaches the file below. The file is still read on every hook: it
// holds the names the payload does not carry, and the directory.
//
// `session_name` remains the statusLine command's alone, which is a slot the
// user owns and this program will not take.
//
// Its `cwd` is present but it is not the session's. [verified 2026-09-20] It is
// the directory the tool ran in, so it follows the agent's `cd` around and
// changes several times a turn — and a display falling back to the last
// component of it renames the session on every one of them.
//
// Both are read out of band instead, from the file Claude keeps for each live
// session:
//
//	~/.claude/sessions/<pid>.json
//	{"pid":38514,"sessionId":"4a152fd4-…","cwd":"/…/agent-notify",
//	 "name":"agent-notify-96","nameSource":"derived","status":"busy",…}
//
// Claude rewrites it as the session runs, so a `/rename` halfway through is
// picked up by the next hook without anything having to be told about it — and
// its `cwd` stays the one the session was started in.
const (
	// configDirectoryVariable relocates the whole of ~/.claude.
	configDirectoryVariable = "CLAUDE_CONFIG_DIR"
	// pidVariable names the file directly. [verified 2026-09-19, 2.1.236] by
	// printing the environment from inside a real hook: CLAUDE_PID=38514, the
	// pid of the claude this hook was a child of, and the name of its file.
	// Without it the directory is scanned instead, which costs a readdir and a
	// few hundred bytes per live session.
	pidVariable = "CLAUDE_PID"
)

// What `nameSource` can say. [verified 2026-10-09, 2.1.285] The binary accepts
// six values — `user`, `hook`, `auto`, `derived`, `collision` and `peer` — and
// a session started with `CLAUDE_CODE_SESSION_NAME` gets a file with a name and
// no `nameSource` at all. `derived` is the placeholder. `collision` is what is
// left when two live sessions want one name, behind a flag that is off on this
// machine, so it has never been seen here and is treated as a placeholder.
// `peer` was not traced.
const (
	// nameAHumanChose is a `/rename` or `--name`, typed or accepted. It is the
	// best name anything has for a session, because somebody decided it.
	nameAHumanChose = "user"
	// nameAHookChose is the `sessionTitle` a UserPromptSubmit hook returned.
	// [verified 2026-10-09, 2.1.285] One returned by a SessionStart hook is
	// written to the transcript and carried by the next payload, and never
	// reaches this file: its `nameSource` stays `derived`.
	nameAHookChose = "hook"
	// nameClaudeGenerated is Claude's own label for the session, written by a
	// side query that is told to produce at most five lowercase words and to
	// "skip generic verbs like fix/add/update". [verified 2026-09-22, 2.1.267]
	// It is written on the bridge path — remote and cloud sessions — and not on
	// an ordinary interactive one, so it is read here rather than relied on.
	// The 2.1.285 binary also writes it when a plan is approved; that has not
	// been seen.
	nameClaudeGenerated = "auto"
)

// ClaudeSession is the part of that file this program reads. Claude writes a
// dozen more fields and will write more; they are none of our business.
//
// The placeholder is the last component of the directory and one random byte
// in hex — `agent-notify-16`. [verified 2026-10-09, 2.1.285] It is
// `randomBytes(1)` in the binary and not a counter: two sessions started in the
// same second were `-4a` and `-2a`. It is read and never reported (D-75): core's
// own fallback is the directory plus two characters of the session id, which
// tells sessions in one repository apart just as well and is the same for every
// agent.
type ClaudeSession struct {
	SessionID  string `json:"sessionId"`
	Name       string `json:"name"`
	NameSource string `json:"nameSource"`
	Cwd        string `json:"cwd"`
}

// NameSomebodyChose is the name if a person or a hook decided it, and nothing
// otherwise.
//
// A name with no `nameSource` is one of them. [verified 2026-10-09, 2.1.285] It
// is how Claude files a session started with `CLAUDE_CODE_SESSION_NAME`, which
// the binary itself labels `user` and the file leaves out. Claude strips that
// variable from a hook's environment, so this file is the only place the name
// exists.
func (session ClaudeSession) NameSomebodyChose() string {
	switch session.NameSource {
	case nameAHumanChose, nameAHookChose, "":
		return session.Name
	}
	return ""
}

// NameClaudeGenerated is the name if Claude wrote it as a label, and nothing
// otherwise — in particular nothing for the placeholder it stamps on a session
// that has no name at all.
func (session ClaudeSession) NameClaudeGenerated() string {
	if session.NameSource == nameClaudeGenerated {
		return session.Name
	}
	return ""
}

// WhatClaudeKnowsAboutThisSession is the name and directory to report, or
// nothing at all.
//
// Nothing at all is never an error: Claude may be too old to keep the file, and
// a hook that guessed would be worse than a hook that said nothing. Core treats
// an empty field as "leave the stored one alone", and the caller still has the
// payload's own cwd to fall back to.
func WhatClaudeKnowsAboutThisSession(sessionID string) ClaudeSession {
	sessionID = strings.TrimSpace(sessionID)
	directory := whereClaudeKeepsItsLiveSessions()
	if sessionID == "" || directory == "" {
		return ClaudeSession{}
	}

	// The fast path, which is the path: one open of one small file.
	if pid := strings.TrimSpace(os.Getenv(pidVariable)); pid != "" {
		if session, itIsThisSession := sessionInside(filepath.Join(directory, pid+".json"), sessionID); itIsThisSession {
			return session
		}
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		return ClaudeSession{}
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		if session, itIsThisSession := sessionInside(filepath.Join(directory, entry.Name()), sessionID); itIsThisSession {
			return session
		}
	}
	return ClaudeSession{}
}

// sessionInside reads one of Claude's session files, and says whether it is
// about the session the hook is describing.
//
// Checking the session id is the whole safety of this: the file is named after
// a pid, pids are reused, and `/clear` replaces the session inside a process
// without the file changing its name. A file that does not agree about which
// session it describes is somebody else's, or is stale, and either way it has
// nothing to say about this one.
func sessionInside(path, sessionID string) (ClaudeSession, bool) {
	content, err := os.ReadFile(path)
	if err != nil {
		return ClaudeSession{}, false
	}
	var session ClaudeSession
	if err := json.Unmarshal(content, &session); err != nil {
		return ClaudeSession{}, false
	}
	if session.SessionID != sessionID {
		return ClaudeSession{}, false
	}
	session.Name = strings.TrimSpace(session.Name)
	session.NameSource = strings.TrimSpace(session.NameSource)
	session.Cwd = strings.TrimSpace(session.Cwd)
	return session, true
}

// whereClaudeKeepsItsLiveSessions is ~/.claude/sessions, or wherever the user
// moved the whole configuration directory to.
func whereClaudeKeepsItsLiveSessions() string {
	if configured := strings.TrimSpace(os.Getenv(configDirectoryVariable)); configured != "" {
		return filepath.Join(configured, "sessions")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "sessions")
}
