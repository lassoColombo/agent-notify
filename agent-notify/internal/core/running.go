package core

import (
	"github.com/lassoColombo/agent-notify/internal/process"
	"github.com/lassoColombo/agent-notify/session"
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
func (c *Core) WhatIsRunning(includeEnded bool) []session.Record {
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
		// A session being filed is written into ended/ before it leaves
		// sessions/, so a read that falls between the two lists sees it twice.
		// The higher sequence is the newer, which is how Read settles the same
		// two files.
		var both []session.Record
		at := map[session.Key]int{}
		for _, record := range append(records, ended...) {
			if i, seen := at[record.Key]; seen {
				if record.Sequence > both[i].Sequence {
					both[i] = record
				}
				continue
			}
			at[record.Key] = len(both)
			both = append(both, record)
		}
		records = both
	}

	boot, err := process.BootIdentity()
	if err != nil {
		c.Logger.Warn("cannot read this boot's identity", "problem", err.Error())
	}
	machine := process.ProcessesOnThisMachine{}
	for i := range records {
		if records[i].Kernel == session.Ended {
			continue
		}
		if process.LivenessOf(records[i], boot, machine) == process.Gone {
			records[i].Kernel = session.Ended
			records[i].Detail = process.ProcessGone
			records[i].Rank = session.Ended.Rank()
		}
	}
	session.ByUrgency(records)
	return records
}
