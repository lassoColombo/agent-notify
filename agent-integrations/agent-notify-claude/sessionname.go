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

// The hook payload carries neither, on the hooks that matter. [verified
// 2026-09-19, 2.1.236] Every hook input Claude built was spread from the same
// six fields — session_id, transcript_path, cwd, prompt_id, permission_mode and
// the agent pair — and the name was not among them.
//
// [verified 2026-09-22, 2.1.267] That has since half changed, and the half
// matters. Claude began sending `session_title` on 2026-09-20; 225 of 23,984
// captured payloads carry one, and every value is a `/rename` somebody typed —
// so it is the top rung of the three and never the other two. It rides on
// `UserPromptSubmit` and `SessionStart` alone, which is two of the nine hooks
// this program subscribes to, so a name taken from it would be a name that
// arrives on a prompt and not on the tool call after it. The file below answers
// on all nine and is the only source for `nameSource`, so the file is still
// what is read; the payload agrees with it wherever both speak.
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

// The four things `nameSource` can say, of which two are a name and two are a
// placeholder. [verified 2026-09-22, 2.1.267] The schema in the binary
// enumerates `["user","auto","collision"]` and a second code path writes
// `"derived"`; this machine's live sessions carry `user` and `derived`.
const (
	// nameAHumanChose is a `/rename`, typed or accepted. It is the best name
	// anything has for a session, because somebody decided it.
	nameAHumanChose = "user"
	// nameClaudeGenerated is Claude's own label for the session, written by a
	// side query that is told to produce at most five lowercase words and to
	// "skip generic verbs like fix/add/update". [verified 2026-09-22, 2.1.267]
	// It is written on the bridge path — remote and cloud sessions — and not on
	// an ordinary interactive one, so it is read here rather than relied on.
	nameClaudeGenerated = "auto"
)

// ClaudeSession is the part of that file this program reads. Claude writes a
// dozen more fields and will write more; they are none of our business.
//
// The two placeholder sources, `derived` and `collision`, are both
// `<cwd-basename>-<counter>` — `agent-notify-16` — and the name is still
// reported when that is all there is. The counter looked like noise beside
// core's own fallback, which is that basename without it, and it is the only
// thing that tells three sessions in one repository apart: throwing it away
// made all three read `agent-notify`. What `nameSource` buys is the order — a
// placeholder now sorts below Claude's own title and below the first prompt
// (sessiontitle.go), and above nothing else.
type ClaudeSession struct {
	SessionID  string `json:"sessionId"`
	Name       string `json:"name"`
	NameSource string `json:"nameSource"`
	Cwd        string `json:"cwd"`
}

// NameAHumanChose is the name if somebody decided it, and nothing otherwise.
func (session ClaudeSession) NameAHumanChose() string {
	if session.NameSource == nameAHumanChose {
		return session.Name
	}
	return ""
}

// NameClaudeGenerated is the name if Claude wrote it as a label, and nothing
// otherwise — in particular nothing for the `<cwd-basename>-<counter>` it
// stamps on a session that has no name at all.
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
