// Package onpath answers which integrations are installed, by looking at
// PATH and nothing else (D-102). It is the set the configuration starts from
// and the set `install` completes to, and having one answer is what stops
// the completion offering something the configuration would not run.
package onpath

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// Integrations is every agent-notify-<name> a person could run, by the name you
// would type after `agent-notify install`.
//
// The naming convention is the only thing core knows about an integration, and
// deliberately so (R10) — this cannot tell a display from an adapter, and does
// not need to.
func Integrations() []string {
	const prefix = "agent-notify-"
	seen := map[string]bool{}
	var found []string
	for _, directory := range filepath.SplitList(os.Getenv("PATH")) {
		entries, err := os.ReadDir(directory)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			name, isOne := strings.CutPrefix(entry.Name(), prefix)
			if !isOne || seen[name] {
				continue
			}
			if _, err := exec.LookPath(entry.Name()); err != nil {
				continue
			}
			seen[name] = true
			found = append(found, name)
		}
	}
	slices.Sort(found)
	return found
}
