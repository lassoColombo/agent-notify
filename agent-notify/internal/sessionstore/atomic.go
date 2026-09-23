package sessionstore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lassoColombo/agent-notify/internal/paths"
)

// writeJSON replaces path with value, atomically, or leaves it alone.
//
// A reader never sees a half-written record because it never sees the file
// being written: the bytes go to a temporary file in the same directory, are
// flushed to disk, and only then does one rename put them in place. The
// directory itself is flushed afterwards, because a rename that is not durable
// is a rename that a power cut can undo while leaving the data behind
// (plan.md §A7.3).
func writeJSON(path string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("cannot encode %s: %w", filepath.Base(path), err)
	}
	encoded = append(encoded, '\n')

	dir := filepath.Dir(path)
	temporary, err := os.CreateTemp(dir, ".write-*")
	if err != nil {
		return fmt.Errorf("cannot create a temporary file in %s: %w", dir, err)
	}
	// From here on every failure has to take the temporary file with it, or a
	// crashed write leaves litter that nothing will ever clean up.
	defer os.Remove(temporary.Name())

	if err := temporary.Chmod(paths.FileMode); err != nil {
		temporary.Close()
		return fmt.Errorf("cannot restrict %s: %w", temporary.Name(), err)
	}
	if _, err := temporary.Write(encoded); err != nil {
		temporary.Close()
		return fmt.Errorf("cannot write %s: %w", temporary.Name(), err)
	}
	if err := flush(temporary); err != nil {
		temporary.Close()
		return fmt.Errorf("cannot flush %s: %w", temporary.Name(), err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("cannot close %s: %w", temporary.Name(), err)
	}
	if err := os.Rename(temporary.Name(), path); err != nil {
		return fmt.Errorf("cannot put %s in place: %w", path, err)
	}
	return syncDir(dir)
}

// readJSON loads a file, reporting separately that it was not there — which is
// the ordinary case for a session nobody has recorded yet, not a failure.
func readJSON(path string, into any) (bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("cannot read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, into); err != nil {
		return false, fmt.Errorf("%s is not readable as JSON: %w", path, err)
	}
	return true, nil
}

// syncDir makes a rename durable. A directory is opened read-only and synced;
// on the platforms this runs on that is the documented way to do it.
func syncDir(dir string) error {
	handle, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("cannot open %s to flush it: %w", dir, err)
	}
	defer handle.Close()
	if err := flush(handle); err != nil {
		return fmt.Errorf("cannot flush %s: %w", dir, err)
	}
	return nil
}
