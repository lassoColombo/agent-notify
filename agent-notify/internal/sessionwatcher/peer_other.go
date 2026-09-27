//go:build !darwin

package sessionwatcher

import "net"

// peerPID has no implementation here yet. Saying so is the point: the
// supervisor falls back to matching by name, which is what it would have done
// anyway, and nothing else in the system changes (M17 fills this in).
func peerPID(net.Conn) (int, bool) { return 0, false }
