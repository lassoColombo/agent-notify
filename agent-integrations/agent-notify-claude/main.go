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
		// The process the agent runs under, for the ancestry walk (§A8.4).
		"claude",
		map[string]func([]string) int{"install": install, "uninstall": uninstall},
		func(payload Payload) (session.Report, bool) {
			// The title is written once, near the start, and never changes,
			// so the head of the transcript is read once a turn and not once
			// a tool call: on the prompt, by which time a title written after
			// the first one has landed (D-107). The tail is read on every
			// hook, because the responses on it are new on every hook.
			title := ""
			if payload.HookEventName == "UserPromptSubmit" {
				title = WhatClaudeTitlesThisSession(payload.TranscriptPath)
			}
			return Translate(payload.HookEventName, payload,
				WhatClaudeKnowsAboutThisSession(payload.SessionID),
				WhatTheTranscriptSays(payload.TranscriptPath), title)
		}))
}
