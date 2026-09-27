// Package onpath answers which integrations are actually installed, by looking
// at PATH and nothing else.
//
// Two commands ask: `install` completes to them, and `doctor` uses the same
// answer to notice a program you installed and never mentioned in your config.
// Having one answer is what stops the completion offering something `install`
// would then refuse.
package onpath

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"
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

// CompleteThem offers the programs that are actually there, which is the same
// question `install` asks a moment later.
func CompleteThem(
	command *cobra.Command, arguments []string, whatHasBeenTypedSoFar string,
) ([]string, cobra.ShellCompDirective) {
	if len(arguments) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return Integrations(), cobra.ShellCompDirectiveNoFileComp
}
