package session

// View is what a display is handed: the world, and what moved in it since this
// display last looked. It is what `render` reads on stdin and what
// `agent-notify tail --json` prints, so it is a published shape.
type View struct {
	// Sessions is everything, most urgent first (R24).
	Sessions []Record `json:"sessions"`

	// Changed is what moved since the last view, most urgent first. A renderer
	// ignores it; a notifier reads it and nothing else. It is empty on the
	// first view: nothing has moved when there was no previous one.
	Changed []Change `json:"changed,omitempty"`
}

// Change is one session that moved, and what it moved from.
type Change struct {
	Record Record `json:"record"`

	// PreviousKernel is the kernel this display last saw it in; empty for a
	// session it has never seen, which is an arrival rather than a
	// transition. Equal to Record.Kernel means something other than the state
	// moved.
	PreviousKernel Kernel `json:"previous_kernel,omitempty"`
}

// LastShown is the world as one display was last handed it. It is kept by
// whoever has the memory that outlives the change: core for a display it
// runs, the display itself for one that owns its process.
type LastShown struct {
	sessions map[string]Record
	shown    bool
}

// Replace swaps the whole world in and answers with the view to hand over, or
// false when this display has nothing to be told: it has seen a view, nothing
// moved on the fields it asked to be woken for, and nothing LEFT. The three
// are decided here, once, because the second is not the third: a departure is
// not a change to any record, and a caller comparing records would take a
// session vanishing for nothing happening (R24). The first view carries no
// changes, since nothing has moved when there was no previous one (R22).
func (l *LastShown) Replace(fresh []Record, wakeOn []string) (View, bool) {
	if l.sessions == nil {
		l.sessions = make(map[string]Record, len(fresh))
	}
	was := make(map[string]Kernel, len(fresh))
	var moved []Record
	arrived := make(map[string]bool, len(fresh))

	for _, record := range fresh {
		key := record.Key.String()
		arrived[key] = true
		if previous, have := l.sessions[key]; !have || Differs(previous, record, wakeOn) {
			was[key] = previous.Kernel
			moved = append(moved, record)
		}
		l.sessions[key] = record
	}
	departed := false
	for key := range l.sessions {
		if !arrived[key] {
			delete(l.sessions, key)
			departed = true
		}
	}

	first := !l.shown
	l.shown = true
	if !first && len(moved) == 0 && !departed {
		return View{}, false
	}

	all := make([]Record, 0, len(l.sessions))
	for _, record := range l.sessions {
		all = append(all, record)
	}
	ByUrgency(all)
	if first {
		return View{Sessions: all}, true
	}

	ByUrgency(moved)
	changed := make([]Change, 0, len(moved))
	for _, record := range moved {
		changed = append(changed, Change{Record: record, PreviousKernel: was[record.Key.String()]})
	}
	return View{Sessions: all, Changed: changed}, true
}
