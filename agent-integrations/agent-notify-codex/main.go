package main

import (
	"os"

	"github.com/lassoColombo/agent-notify/hook"
	"github.com/lassoColombo/agent-notify/session"
)

func main() {
	os.Exit(hook.Main(os.Args[1:],
		"agent-notify-codex reads one hook payload on stdin.\n"+
			"  agent-notify-codex install [--print] [--config PATH]\n"+
			"  agent-notify-codex uninstall [--config PATH]\n",
		map[string]func([]string) int{"install": install, "uninstall": uninstall},
		func(payload Payload) (session.Report, bool) {
			return Translate(payload,
				WhatCodexCallsThisThread(payload.SessionID),
				WhatCodexHasSpent(payload.TranscriptPath))
		}))
}
