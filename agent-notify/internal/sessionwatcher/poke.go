package sessionwatcher

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"time"

	"github.com/lassoColombo/agent-notify/internal/paths"
	"github.com/lassoColombo/agent-notify/session"
)

// Poke is the whole of what record-agent-event says to the session-watcher:
// this session changed, and this is how far along it is.
//
// It carries nothing else on purpose. The 2048-byte datagram cap is not an
// obstacle to work around; it is the rule "events are hints" enforced by the
// kernel. The content is in the store, and a poke that never arrives costs
// nothing but a sweep's worth of delay (plan.md §A13.1, R4).
type Poke struct {
	Key      string `json:"key"`
	Sequence uint64 `json:"sequence"`
	// Event is what caused the change, carried so that a delta can say so
	// (§A13.1). It is an optimisation like the rest of the poke: lose it and
	// the sweep publishes the same change without it.
	Event session.Event `json:"event,omitempty"`
}

// ErrNobodyListening means there is no session-watcher, which is the signal to
// start one.
var ErrNobodyListening = errors.New("no session-watcher is listening")

// Send delivers a poke, or says nobody was there.
//
// It never blocks for long, which matters more than it looks: a unix datagram
// socket does not silently discard like UDP — it blocks the sender or returns
// EAGAIN — so a session-watcher that stopped reading could otherwise reach all
// the way back down the hook path and stall a user's agent (§A9.3, R1).
func Send(layout paths.Layout, poke Poke) error {
	encoded, err := json.Marshal(poke)
	if err != nil {
		return err
	}

	address, err := net.ResolveUnixAddr("unixgram", layout.SessionChangesSocket())
	if err != nil {
		return err
	}
	connection, err := net.DialUnix("unixgram", nil, address)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED) {
			return ErrNobodyListening
		}
		return err
	}
	defer connection.Close()

	if err := connection.SetWriteDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		return err
	}
	if _, err := connection.Write(encoded); err != nil {
		if errors.Is(err, syscall.ECONNREFUSED) {
			return ErrNobodyListening
		}
		return err
	}
	return nil
}

// Listen binds the datagram socket the pokes arrive on.
//
// The stale socket file of a killed session-watcher is removed first. That is
// safe only because the caller holds the singleton lock — which is why this
// takes one rather than being callable on its own (§A9.2).
func Listen(
	layout paths.Layout, held *TheOnlyRunningWatcher,
) (*net.UnixConn, error) {
	if held == nil {
		return nil, fmt.Errorf("the socket is bound by whoever holds the lock, and nobody does")
	}
	if err := layout.CheckSockets(); err != nil {
		return nil, err
	}
	path := layout.SessionChangesSocket()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("cannot clear the stale socket %s: %w", path, err)
	}

	address, err := net.ResolveUnixAddr("unixgram", path)
	if err != nil {
		return nil, err
	}
	connection, err := net.ListenUnixgram("unixgram", address)
	if err != nil {
		return nil, fmt.Errorf("cannot bind %s: %w", path, err)
	}
	if err := os.Chmod(path, paths.FileMode); err != nil {
		connection.Close()
		return nil, err
	}
	return connection, nil
}

// Receive reads one poke, or reports that the deadline passed.
//
// A datagram that does not parse is not an error worth stopping for: the store
// is the truth, and the worst a corrupt poke can cost is one reconcile that
// would have happened at the next tick anyway.
func Receive(connection *net.UnixConn, within time.Duration) (Poke, bool) {
	buffer := make([]byte, 2048)
	if err := connection.SetReadDeadline(time.Now().Add(within)); err != nil {
		return Poke{}, false
	}
	read, _, err := connection.ReadFromUnix(buffer)
	if err != nil {
		return Poke{}, false
	}
	var poke Poke
	if err := json.Unmarshal(buffer[:read], &poke); err != nil {
		return Poke{}, false
	}
	return poke, true
}

// PokeFor is the poke a record deserves.
func PokeFor(record session.Record, event session.Event) Poke {
	return Poke{Key: record.Key.String(), Sequence: record.Sequence, Event: event}
}
