// Package find turns whatever a person typed into the one session they meant,
// and offers the ones that are running when they have not typed it yet.
//
// Three commands take a session as their first argument — `focus-session`,
// `focused` and `annotate` — and the two halves here are what they share. They
// are one package because they are one question asked twice: the completion
// offers exactly the names [Session] will accept, so nothing can be offered
// that would then be refused.
package find

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lassoColombo/agent-notify/internal/core"
	"github.com/lassoColombo/agent-notify/session"
)

// Session turns whatever a person typed into one record.
//
// Three spellings, because three different callers exist: a picker passes the
// key it was given, a person types the name they see on their bar, and a script
// pastes a session id out of a log. Ambiguity is an error that lists the
// candidates rather than a guess, because the whole point of focus is going
// somewhere on purpose.
func Session(openedCore *core.Core, wanted string) (session.Record, error) {
	sessions, err := openedCore.Store.List()
	if err != nil {
		return session.Record{}, err
	}
	ended, err := openedCore.Store.ListEnded()
	if err == nil {
		sessions = append(sessions, ended...)
	}

	var matched []session.Record
	for _, record := range sessions {
		switch {
		case record.Key.String() == wanted,
			record.Key.SessionID == wanted,
			strings.EqualFold(record.DisplayName(), wanted),
			len(wanted) >= 4 && strings.HasPrefix(record.Key.SessionID, wanted):
			matched = append(matched, record)
		}
	}

	switch len(matched) {
	case 1:
		return matched[0], nil
	case 0:
		return session.Record{}, fmt.Errorf("no session matches %q — `agent-notify list --all` says what there is", wanted)
	}

	session.ByUrgency(matched)
	var names strings.Builder
	for _, record := range matched {
		fmt.Fprintf(&names, "\n  %s  %s  %s", record.Key.String(), record.DisplayName(), record.State())
	}
	return session.Record{}, fmt.Errorf("%q matches %d sessions:%s", wanted, len(matched), names.String())
}

// TheSessionsThatAreRunning offers them most urgent first, with what each one
// is doing as the description beside it.
//
// It is the reason the command line is cobra at all. Everything else the
// library brings is comfort; this is the program answering its own question at
// the moment somebody is asking it.
func TheSessionsThatAreRunning(
	command *cobra.Command, arguments []string, whatHasBeenTypedSoFar string,
) ([]string, cobra.ShellCompDirective) {
	if len(arguments) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	openedCore, err := core.OpenEverythingACommandNeeds("complete")
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	defer openedCore.Close()

	records, err := openedCore.Store.List()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	session.ByUrgency(records)

	offered := make([]string, 0, len(records))
	for _, record := range records {
		// `name<TAB>description` is cobra's spelling for a completion with a
		// gloss beside it, which zsh and fish both show.
		offered = append(offered, record.DisplayName()+"\t"+string(record.Kernel))
	}
	return offered, cobra.ShellCompDirectiveNoFileComp
}
