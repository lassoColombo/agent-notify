package main

import (
	"os"

	"github.com/lassoColombo/agent-notify/hook"
	"github.com/lassoColombo/agent-notify/session"
)

func main() {
	var payload Payload
	os.Exit(hook.Main(os.Args[1:],
		"agent-notify-codex reads one hook payload on stdin.\n"+
			"  agent-notify-codex install [--print] [--config PATH]\n",
		install, &payload, func() (session.Report, bool) {
			return Translate(payload,
				WhatCodexCallsThisThread(payload.SessionID),
				WhatCodexHasSpent(payload.TranscriptPath))
		}))
}
