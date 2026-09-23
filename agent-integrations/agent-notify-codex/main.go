package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/lassoColombo/agent-notify/hook"
)

// main never exits non-zero from a hook, and never writes to stdout.
//
// Both matter more here than for any other agent so far. Codex reads exit code
// 2 as "block this operation", and PermissionRequest reads a handler's STDOUT
// as a verdict on the tool it is asking about — saying nothing is how a handler
// declines to decide and lets the normal flow continue. A notifier that denies
// your agent a tool call, or silently approves one, is far worse than a
// notifier that does not notify (R2).
func main() {
	// install is not a hook. It is run by a person, so it may speak and it may
	// fail.
	if len(os.Args) > 1 && os.Args[1] == "install" {
		os.Exit(install(os.Args[2:]))
	}
	if len(os.Args) > 1 {
		fmt.Fprintln(os.Stderr, "agent-notify-codex reads one hook payload on stdin.")
		fmt.Fprintln(os.Stderr, "  agent-notify-codex install [--print] [--config PATH]")
		os.Exit(1)
	}

	var payload Payload
	// A payload that does not parse is not an error: codex sends a different
	// shape per hook and adds fields between releases.
	_ = json.NewDecoder(os.Stdin).Decode(&payload)

	report, worth := Translate(payload,
		WhatCodexCallsThisThread(payload.SessionID),
		WhatCodexHasSpent(payload.TranscriptPath))
	if !worth {
		return
	}
	hook.Record(report)
}
