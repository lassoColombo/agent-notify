//go:build unix

package sessionstore

import (
	"os"
	"syscall"
)

// flush is fsync, and deliberately not (*os.File).Sync.
//
// [verified 2026-09-17, macOS 26, APFS] On darwin Go's File.Sync issues
// F_FULLFSYNC, which waits for the drive to empty its own write cache: 5.8ms
// per write against 160µs for fsync, measured on a 1KB record written by
// tmp-file and rename. An event can write two files, so File.Sync would have
// put ~12ms on every hook — on the one path that must never make the agent
// wait (R1).
//
// fsync is what §A7.3 asks for and what this data is worth. It gets the record
// out of this process and into the kernel, which is what survives a crashed
// hook. What it does not survive is losing power, and that costs nothing here:
// a reboot changes the boot id, which ends every session in the store anyway
// (§A8), so a record that did not reach the platter was describing a process
// that no longer exists.
func flush(file *os.File) error {
	return syscall.Fsync(int(file.Fd()))
}
