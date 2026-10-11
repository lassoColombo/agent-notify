package tool

import (
	"fmt"
	"os"
	"os/exec"
)

// Find is where a tool is: what the configuration says, checked, or else the
// name looked up on PATH (D-101). An integration runs as a descendant of the
// agent, which was started from a shell, so PATH is ordinarily the user's;
// the key is for the one started some other way. key names the setting, for
// the message.
func Find(key, name, configured string) (string, error) {
	if configured != "" {
		if _, err := os.Stat(configured); err != nil {
			return "", fmt.Errorf("%s = %q: %w", key, configured, err)
		}
		return configured, nil
	}
	found, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%s is not on this process's PATH and %s does not say where it is",
			name, key)
	}
	return found, nil
}
