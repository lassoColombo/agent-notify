// Package onpath answers which integrations are installed, by looking at
// PATH and nothing else (D-102). It is the set the configuration starts from
// and the set `install` completes to, and having one answer is what stops
// the completion offering something the configuration would not run.
package onpath

import (
	"os"
	"path/filepath"
	"strings"
)

// Integrations is every agent-notify-<name> a person could run, by the name you
// would type after `agent-notify install`, and where each one is: the first
// executable file of that name along PATH, which is the one exec resolves.
//
// The naming convention is the only thing core knows about an integration, and
// deliberately so (R10) — this cannot tell a display from an adapter, and does
// not need to.
func Integrations() map[string]string {
	const prefix = "agent-notify-"
	found := map[string]string{}
	visited := map[string]bool{}
	for _, directory := range filepath.SplitList(os.Getenv("PATH")) {
		if visited[directory] {
			// A PATH that names a directory twice is the ordinary case, and
			// the directory is read once (D-116).
			continue
		}
		visited[directory] = true
		entries, err := os.ReadDir(directory)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			name, isOne := strings.CutPrefix(entry.Name(), prefix)
			if !isOne || name == "" || found[name] != "" {
				continue
			}
			// One stat, of the entry itself, rather than LookPath walking the
			// whole of PATH again for a name this directory just listed.
			path := filepath.Join(directory, entry.Name())
			info, err := os.Stat(path)
			if err != nil || !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
				continue
			}
			found[name] = path
		}
	}
	return found
}
