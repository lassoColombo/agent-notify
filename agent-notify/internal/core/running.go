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
	records, _ := c.WhatIsRunningAndWhatEnded(includeEnded)
	return records
}

// WhatIsRunningAndWhatEnded is WhatIsRunning with the verdicts beside the
// records, for the one caller that files them. The session-watcher reads the
// store once per wake, hands these records to every display it runs, and ends
// what the verdicts name (D-92). It is the same read and the same decision —
// process.Ended, superseded sessions included — so a display the watcher runs
// and one reading cold through the SDK see one world.
func (c *Core) WhatIsRunningAndWhatEnded(includeEnded bool) ([]session.Record, []process.Ending) {
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
		// Without it the reboot rule cannot apply and every session is judged
		// by probing instead, which is slower and still correct.
		c.Logger.Warn("cannot read this boot's identity", "problem", err.Error())
	}
	ended := process.Ended(records, boot, process.Self(), process.ProcessesOnThisMachine{})
	for _, end := range ended {
		for i := range records {
			if records[i].Key != end.Key {
				continue
			}
			records[i].Kernel = session.Ended
			records[i].Detail = end.Detail
			records[i].Rank = session.Ended.Rank()
		}
	}
	session.ByUrgency(records)
	return records, ended
}
