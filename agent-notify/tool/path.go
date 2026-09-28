package tool

import (
	"fmt"
	"os"
	"path/filepath"
)

// AbsolutePath is a tool's path out of the configuration, checked. There is no
// search and no PATH fallback: an integration is run by the hook or by launchd,
// whose PATH is not your shell's, so the lookup happens at install, in your
// shell (D-67). key names the setting, for the message.
func AbsolutePath(key, given string) (string, error) {
	switch {
	case given == "":
		return "", fmt.Errorf("%s is not set — `agent-notify install` finds it in your own "+
			"shell and prints the line to add", key)
	case !filepath.IsAbs(given):
		return "", fmt.Errorf("%s = %q must be an absolute path: this program's PATH is not yours",
			key, given)
	}
	if _, err := os.Stat(given); err != nil {
		return "", fmt.Errorf("%s = %q: %w", key, given, err)
	}
	return given, nil
}
