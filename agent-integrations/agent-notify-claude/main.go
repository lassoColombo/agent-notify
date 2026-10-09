package main

import (
	"os"

	"github.com/lassoColombo/agent-notify/hook"
	"github.com/lassoColombo/agent-notify/session"
)

func main() {
	os.Exit(hook.Main(os.Args[1:],
		"agent-notify-claude reads one hook payload on stdin.\n"+
			"  agent-notify-claude install [--print] [--settings PATH]\n"+
			"  agent-notify-claude uninstall [--settings PATH]\n",
		map[string]func([]string) int{"install": install, "uninstall": uninstall},
		func(payload Payload) (session.Report, bool) {
			return Translate(payload.HookEventName, payload,
				WhatClaudeKnowsAboutThisSession(payload.SessionID),
				WhatTheTranscriptSays(payload.TranscriptPath),
				WhatClaudeTitlesThisSession(payload.TranscriptPath))
		}))
}
