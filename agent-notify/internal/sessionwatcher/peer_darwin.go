//go:build darwin

package sessionwatcher

import (
	"net"

	"golang.org/x/sys/unix"
)

// peerPID asks the kernel which process is on the other end of a unix socket.
//
// It exists so that the supervisor's health check is exact rather than
// name-based: "the child I started has connected" is a different claim from
// "something calling itself zellij-display is connected", and a self-started
// subscriber sharing a name would otherwise make a hung child look healthy.
//
// Asking the kernel rather than taking the peer's word for it is R25: identity
// that can be asserted is identity that will be asserted wrongly, and by
// accident long before by malice. A pid in the handshake would have been one
// field and one lie away from wrong.
func peerPID(connection net.Conn) (int, bool) {
	unixConnection, ok := connection.(*net.UnixConn)
	if !ok {
		return 0, false
	}
	raw, err := unixConnection.SyscallConn()
	if err != nil {
		return 0, false
	}

	var pid int
	var lookup error
	if err := raw.Control(func(fd uintptr) {
		pid, lookup = unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID)
	}); err != nil || lookup != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}
