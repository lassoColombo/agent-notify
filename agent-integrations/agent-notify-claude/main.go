package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/lassoColombo/agent-notify/hook"
)

// main never exits 2, whatever happens.
//
// Claude reads exit code 2 from a hook as "block this", feeds stderr back into
// the session, and reads stdout into the conversation. A notifier that stops
// your agent working is far worse than one that does not notify, so this
// program is silent on both streams and exits 0 from every path that runs as a
// hook. Diagnostics go to agent-notify's log (R2).
func main() {
	// install is not a hook. It is run by a person, so it may speak and it may
	// fail.
	if len(os.Args) > 1 && os.Args[1] == "install" {
		os.Exit(install(os.Args[2:]))
	}
	if len(os.Args) > 1 {
		fmt.Fprintln(os.Stderr, "agent-notify-claude reads one hook payload on stdin.")
		fmt.Fprintln(os.Stderr, "  agent-notify-claude install [--print] [--settings PATH]")
		os.Exit(1)
	}

	var payload Payload
	// A payload that does not parse is not an error: Claude sends different
	// shapes per hook and adds fields between releases.
	_ = json.NewDecoder(os.Stdin).Decode(&payload)

	report, worth := Translate(payload.HookEventName, payload,
		WhatClaudeKnowsAboutThisSession(payload.SessionID),
		WhatTheTranscriptSays(payload.TranscriptPath),
		WhatClaudeTitlesThisSession(payload.TranscriptPath))
	if !worth {
		return
	}
	hook.Record(report)
}
