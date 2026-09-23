package watcher

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync"
	"time"

	agentnotify "github.com/lassoColombo/agent-notify"
	"github.com/lassoColombo/agent-notify/internal/paths"
)

// Subscribers is the fan-out half of the session-watcher: every connected
// tool-integration, and the bounded queue in front of each one.
//
// Nothing here may affect anything else. A display may be slow, may crash, may
// be killed, may be a shell script — none of that may reach the agent, the
// store, the session-watcher or another display (§A12.3, R13). So every write
// happens on that subscriber's own goroutine, behind its own queue, and the
// worst a stuck one can do is overflow its own queue and be sent a fresh
// snapshot.
type Subscribers struct {
	listener net.Listener
	logger   *slog.Logger
	bound    int
	// snapshot is how a subscriber gets the current state, whether because it
	// just connected, asked, or fell behind.
	snapshot func(wantEnded bool) []agentnotify.Record

	mu        sync.Mutex
	connected []*subscriber
	since     time.Time
}

// Serve binds the stream socket subscribers connect to.
//
// Like the datagram socket, a stale file from a killed session-watcher is
// removed first, and that is safe only because the caller holds the singleton
// lock (§A9.2).
func Serve(
	layout paths.Layout, held *TheOnlyRunningWatcher, logger *slog.Logger, bound int,
	snapshot func(wantEnded bool) []agentnotify.Record,
) (*Subscribers, error) {
	if held == nil {
		return nil, fmt.Errorf("the socket is bound by whoever holds the lock, and nobody does")
	}
	path := layout.SubscribersSocket()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("cannot clear the stale socket %s: %w", path, err)
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("cannot bind %s: %w", path, err)
	}
	if err := os.Chmod(path, paths.FileMode); err != nil {
		listener.Close()
		return nil, err
	}
	if bound < 1 {
		bound = 1
	}
	return &Subscribers{
		listener: listener, logger: logger, bound: bound,
		snapshot: snapshot, since: time.Now().UTC(),
	}, nil
}

// Accept runs until the listener is closed.
func (s *Subscribers) Accept() {
	for {
		connection, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.welcome(connection)
	}
}

// welcome performs the handshake and, if it succeeds, hands the connection to a
// subscriber of its own.
func (s *Subscribers) welcome(connection net.Conn) {
	reader := bufio.NewReader(connection)
	_ = connection.SetReadDeadline(time.Now().Add(10 * time.Second))
	line, err := reader.ReadBytes('\n')
	if err != nil {
		connection.Close()
		return
	}
	_ = connection.SetReadDeadline(time.Time{})

	var hello agentnotify.Hello
	if err := json.Unmarshal(line, &hello); err != nil || hello.Kind != agentnotify.KindHello {
		s.refuse(connection, "the first message must be a hello")
		return
	}
	if hello.Name == "" {
		s.refuse(connection, "a subscriber must say what it is called")
		return
	}
	welcome, err := json.Marshal(agentnotify.Welcome{
		Kind: agentnotify.KindWelcome, Version: agentnotify.Version,
		Since: s.since.Format(time.RFC3339Nano),
	})
	if err != nil {
		connection.Close()
		return
	}
	if _, err := connection.Write(append(welcome, '\n')); err != nil {
		connection.Close()
		return
	}

	// Who is really on the other end, asked of the kernel rather than of the
	// peer (R25). The supervisor uses it to tell "the child I started has
	// connected" from "something calling itself that is connected".
	pid, _ := peerPID(connection)

	joined := &subscriber{
		hello: hello, connection: connection, reader: reader,
		pid: pid, since: time.Now(),
		logger: s.logger.With("subscriber", hello.Name),
		bound:  s.bound, pending: map[string]outbound{},
		signal: make(chan struct{}, 1), snapshot: s.snapshot,
	}
	s.mu.Lock()
	s.connected = append(s.connected, joined)
	s.mu.Unlock()

	s.logger.Info("a subscriber connected",
		"name", hello.Name, "pid", pid, "roles", hello.Roles, "wake_on", hello.WakeOn)

	joined.ask("connect")
	go joined.write()
	joined.read()

	s.drop(joined)
}

func (s *Subscribers) refuse(connection net.Conn, reason string) {
	defer connection.Close()
	s.logger.Warn("refused a subscriber", "reason", reason)
	encoded, err := json.Marshal(agentnotify.Refused{
		Kind: agentnotify.KindRefused, Reason: reason, Version: agentnotify.Version,
	})
	if err != nil {
		return
	}
	_ = connection.SetWriteDeadline(time.Now().Add(time.Second))
	_, _ = connection.Write(append(encoded, '\n'))
}

func (s *Subscribers) drop(leaving *subscriber) {
	s.mu.Lock()
	for i, one := range s.connected {
		if one == leaving {
			s.connected = append(s.connected[:i], s.connected[i+1:]...)
			break
		}
	}
	s.mu.Unlock()
	leaving.close()
	s.logger.Info("a subscriber disconnected", "name", leaving.hello.Name)
}

// Publish offers one change to every subscriber that cares about it.
//
// It never blocks: a subscriber that cannot keep up overflows its own queue and
// is sent a snapshot instead. That is why record-agent-event can never be
// stalled by a slow display, which is the failure this whole shape exists to
// prevent (§A9.3, R1).
func (s *Subscribers) Publish(previous, next agentnotify.Record, event agentnotify.Event) {
	s.mu.Lock()
	listening := append([]*subscriber(nil), s.connected...)
	s.mu.Unlock()

	for _, one := range listening {
		if !agentnotify.Differs(previous, next, one.hello.WakeOn) {
			continue
		}
		one.offer(next.Key.String(), outbound{delta: &agentnotify.Delta{
			Kind: agentnotify.KindDelta, Session: next,
			PreviousKernel: previous.Kernel, Event: event,
		}})
	}
}

// Forget tells every subscriber that a session has been pruned entirely, so
// that nobody's map keeps it forever.
func (s *Subscribers) Forget(key agentnotify.Key) {
	s.mu.Lock()
	listening := append([]*subscriber(nil), s.connected...)
	s.mu.Unlock()

	for _, one := range listening {
		one.offer(key.String(), outbound{gone: &agentnotify.Gone{
			Kind: agentnotify.KindGone, Key: key,
		}})
	}
}

// Connected is who is listening, for doctor.
func (s *Subscribers) Connected() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make([]string, 0, len(s.connected))
	for _, one := range s.connected {
		names = append(names, one.hello.Name)
	}
	return names
}

// A Listener is one connection, as the supervisor needs to see it.
type Listener struct {
	Name  string
	PID   int
	Since time.Time
	Roles []string
}

// Listening is who is connected, with enough to tell one from another. The
// supervisor matches on PID where the platform can supply one and falls back to
// the name where it cannot.
func (s *Subscribers) Listening() []Listener {
	s.mu.Lock()
	defer s.mu.Unlock()
	listening := make([]Listener, 0, len(s.connected))
	for _, one := range s.connected {
		listening = append(listening, Listener{
			Name: one.hello.Name, PID: one.pid, Since: one.since, Roles: one.hello.Roles,
		})
	}
	return listening
}

// Close stops accepting and drops everybody.
func (s *Subscribers) Close() error {
	err := s.listener.Close()
	s.mu.Lock()
	leaving := s.connected
	s.connected = nil
	s.mu.Unlock()
	for _, one := range leaving {
		one.close()
	}
	return err
}

// outbound is one thing waiting to be written to one subscriber.
type outbound struct {
	delta *agentnotify.Delta
	gone  *agentnotify.Gone
}

type subscriber struct {
	hello      agentnotify.Hello
	connection net.Conn
	reader     *bufio.Reader
	// pid is who the kernel says is on the other end, or 0 where the platform
	// cannot say. since is when the handshake completed.
	pid      int
	since    time.Time
	logger   *slog.Logger
	bound    int
	snapshot func(wantEnded bool) []agentnotify.Record

	mu         sync.Mutex
	pending    map[string]outbound
	order      []string
	overflowed bool
	resync     string
	done       bool
	signal     chan struct{}
}

// offer queues one change, coalescing it with anything already waiting for the
// same session.
//
// Coalescing is universal and no subscriber may opt out, because none needs to:
// a renderer draws the current state, and anything acting on change compares
// what it last did with what is true now (R22, D-10). A fifty-tool-call turn
// therefore becomes one render, not fifty.
func (s *subscriber) offer(key string, what outbound) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return
	}

	if _, waiting := s.pending[key]; !waiting {
		if len(s.pending) >= s.bound {
			// It is not keeping up. Throw the queue away and replace it with a
			// single snapshot: overflow degrades to a full redraw, never to a
			// wrong render (§A9.3, R15).
			s.pending = map[string]outbound{}
			s.order = nil
			s.overflowed = true
			s.resync = "overflow"
			s.wake()
			return
		}
		s.order = append(s.order, key)
	}
	s.pending[key] = what
	s.wake()
}

// ask queues a snapshot, replacing anything waiting: a snapshot answers every
// pending delta by construction.
func (s *subscriber) ask(why string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return
	}
	s.pending = map[string]outbound{}
	s.order = nil
	s.overflowed = true
	s.resync = why
	s.wake()
}

// wake nudges the writer without ever blocking. A signal already waiting says
// everything a second one would.
func (s *subscriber) wake() {
	select {
	case s.signal <- struct{}{}:
	default:
	}
}

// write is the only goroutine that touches this connection's write side, so a
// slow peer blocks here and nowhere else.
func (s *subscriber) write() {
	for range s.signal {
		s.mu.Lock()
		if s.done {
			s.mu.Unlock()
			return
		}
		overflowed, why := s.overflowed, s.resync
		pending, order := s.pending, s.order
		s.overflowed, s.resync = false, ""
		s.pending, s.order = map[string]outbound{}, nil
		s.mu.Unlock()

		if overflowed {
			if !s.send(agentnotify.Snapshot{
				Kind: agentnotify.KindSnapshot, Why: why,
				Sessions: s.snapshot(s.hello.WantEnded),
			}) {
				return
			}
			if why == "overflow" {
				s.logger.Warn("a subscriber fell behind and was sent a fresh snapshot")
			}
			continue
		}
		for _, key := range order {
			what := pending[key]
			var ok bool
			switch {
			case what.delta != nil:
				ok = s.send(*what.delta)
			case what.gone != nil:
				ok = s.send(*what.gone)
			default:
				ok = true
			}
			if !ok {
				return
			}
		}
	}
}

func (s *subscriber) send(message any) bool {
	encoded, err := json.Marshal(message)
	if err != nil {
		s.logger.Warn("cannot encode a message", "problem", err.Error())
		return true
	}
	_ = s.connection.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if _, err := s.connection.Write(append(encoded, '\n')); err != nil {
		s.logger.Info("a subscriber stopped reading", "problem", err.Error())
		return false
	}
	return true
}

// read handles what a subscriber says after the handshake, which today is only
// "send me a snapshot".
func (s *subscriber) read() {
	for {
		line, err := s.reader.ReadBytes('\n')
		if err != nil {
			return
		}
		kind, err := agentnotify.KindOf(line)
		if err != nil {
			s.logger.Warn("a subscriber said something unreadable", "problem", err.Error())
			continue
		}
		switch kind {
		case agentnotify.KindResync:
			s.ask("asked")
		default:
			// Carried and ignored: a newer subscriber within the same major
			// version may say things this build has never heard of (R12).
			s.logger.Debug("a subscriber said something this build ignores", "kind", kind)
		}
	}
}

func (s *subscriber) close() {
	s.mu.Lock()
	if s.done {
		s.mu.Unlock()
		return
	}
	s.done = true
	close(s.signal)
	s.mu.Unlock()
	_ = s.connection.Close()
}
