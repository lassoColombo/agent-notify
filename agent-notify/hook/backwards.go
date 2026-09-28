package hook

import (
	"bytes"
	"os"
	"slices"
)

// ReadBackwards hands each line of a file to each, newest first, reading block
// bytes at a time from the end, until each returns false or limit bytes have
// been read. A line split across two reads is handed over whole.
//
// For an adapter reading an agent's own append-only files on the hook path:
// the newest lines are the ones worth having, and a transcript an hour in is
// fifty megabytes.
func ReadBackwards(path string, block, limit int64, each func(line []byte) bool) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	facts, err := file.Stat()
	if err != nil {
		return err
	}

	var carried []byte
	var readSoFar int64
	for end := facts.Size(); end > 0 && readSoFar < limit; {
		start := max(end-block, 0)
		chunk := make([]byte, end-start)
		if _, err := file.ReadAt(chunk, start); err != nil {
			return err
		}
		readSoFar += int64(len(chunk))

		lines := bytes.Split(slices.Concat(chunk, carried), []byte("\n"))
		if start > 0 {
			carried, lines = lines[0], lines[1:]
		}
		for i := len(lines) - 1; i >= 0; i-- {
			if !each(lines[i]) {
				return nil
			}
		}
		end = start
	}
	return nil
}
