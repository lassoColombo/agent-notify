package main

import (
	"os"

	"github.com/lassoColombo/agent-notify/hook"
	"github.com/lassoColombo/agent-notify/session"
)

func main() {
	var payload Payload
	os.Exit(hook.Main(os.Args[1:],
		"agent-notify-claude reads one hook payload on stdin.\n"+
			"  agent-notify-claude install [--print] [--settings PATH]\n",
		install, &payload, func() (session.Report, bool) {
			return Translate(payload.HookEventName, payload,
				WhatClaudeKnowsAboutThisSession(payload.SessionID),
				WhatTheTranscriptSays(payload.TranscriptPath),
				WhatClaudeTitlesThisSession(payload.TranscriptPath))
		}))
}
