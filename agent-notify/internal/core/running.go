package core

import (
	agentnotify "github.com/lassoColombo/agent-notify"
	"github.com/lassoColombo/agent-notify/internal/process"
)

// WhatIsRunning is the cold read path: what there is, most urgent first, with
// the liveness decision applied as it reads and nothing written (§A7.6, D-30).
//
// A session whose process was killed must not be shown as working merely
// because nothing has got round to filing it — but filing it is a transition,
// and transitions belong to the session-watcher (§A8.6). So this corrects the
// record it hands over and leaves the one on disk alone: a command a statusline
// polls three times a second has no business writing to the store.
//
// It is here rather than in the command that first needed it because three
// things need the same answer — `list`, the browser, and any display reading
// cold through the SDK — and three copies of "is this process still there"
// would eventually disagree about a session somebody is looking at (R24).
//
// `includeEnded` asks for the sessions that are over and still resumable, which
// is the set a picker exists to offer (§A7.5) and the set a bar does not want.
func (c *Core) WhatIsRunning(includeEnded bool) []agentnotify.Record {
	records, err := c.Store.List()
	if err != nil {
		// One unreadable record costs its own row and nothing else.
		c.Logger.Warn("reading sessions", "problem", err.Error())
	}
	if includeEnded {
		ended, err := c.Store.ListEnded()
		if err != nil {
			c.Logger.Warn("reading ended sessions", "problem", err.Error())
		}
		records = append(records, ended...)
	}

	boot, err := process.BootIdentity()
	if err != nil {
		c.Logger.Warn("cannot read this boot's identity", "problem", err.Error())
	}
	machine := process.ProcessesOnThisMachine{}
	for i := range records {
		if records[i].Kernel == agentnotify.Ended {
			continue
		}
		if process.LivenessOf(records[i], boot, machine) == process.Gone {
			records[i].Kernel = agentnotify.Ended
			records[i].Detail = process.ProcessGone
			records[i].Rank = agentnotify.Ended.Rank()
		}
	}
	agentnotify.ByUrgency(records)
	return records
}
