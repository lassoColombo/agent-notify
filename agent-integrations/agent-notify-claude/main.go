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
			// Which session this one continues is looked for when it starts,
			// and again on a prompt in case the old transcript said so only
			// after the start: once a turn, never once a tool call (D-127).
			continues := ""
			if payload.HookEventName == "SessionStart" || payload.HookEventName == "UserPromptSubmit" {
				continues = WhatThisSessionContinues(payload.TranscriptPath, payload.SessionID)
			}
			// The tail is read on every hook, because the responses on it,
			// and the title among them, are new on every hook.
			return Translate(payload,
				WhatClaudeKnowsAboutThisSession(payload.SessionID),
				WhatTheTranscriptSays(payload.TranscriptPath), continues)
		}))
}
