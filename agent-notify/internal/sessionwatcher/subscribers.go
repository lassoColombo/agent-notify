package sessionwatcher

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync"
	"time"

	"github.com/lassoColombo/agent-notify/internal/paths"
	"github.com/lassoColombo/agent-notify/session"
)

// Subscribers is the fan-out half of the session-watcher: every connected
// tool-integration, and the one slot in front of each.
//
// It sends the whole world, every time, and that one decision is most of this
// file. There used to be deltas, a queue per subscriber, coalescing by session
// key, a bound on that queue, and a degradation path that threw the queue away
// and sent a snapshot when it overflowed — five mechanisms to deliver what one
// slot holding the newest world delivers by construction. A snapshot was
// already the answer to every hard case (a cold start, a reconnection, an
// overflow); making it the answer to the easy case too is what deleted the
// other four.
//
// Nothing here may affect anything else. A display may be slow, may crash, may
// be killed, may be a shell script — none of that may reach the agent, the
// store, the session-watcher or another display (§A12.3, R13). So every write
// happens on that subscriber's own goroutine, behind its own slot, and the
// worst a stuck one can do is miss worlds it would have painted over anyway.
type Subscribers struct {
	listener net.Listener
	logger   *slog.Logger
	// theWorldNow is asked at the moment of writing rather than carried in: a
	// write that has been waiting on a slow reader should send what is true
	// now, not what was true when it was asked for.
	theWorldNow func(wantEnded bool) []session.Record

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
	layout paths.Layout, held *TheOnlyRunningWatcher, logger *slog.Logger,
	theWorldNow func(wantEnded bool) []session.Record,
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
	return &Subscribers{
		listener: listener, logger: logger,
		theWorldNow: theWorldNow, since: time.Now().UTC(),
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

	var hello session.Hello
	if err := json.Unmarshal(line, &hello); err != nil || hello.Kind != session.KindHello {
		s.refuse(connection, "the first message must be a hello")
		return
	}
	if hello.Name == "" {
		s.refuse(connection, "a subscriber must say what it is called")
		return
	}
	welcome, err := json.Marshal(session.Welcome{
		Kind: session.KindWelcome, Version: session.Version,
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

	joined := &subscriber{
		hello: hello, connection: connection, reader: reader,
		logger:      s.logger.With("subscriber", hello.Name),
		theWorldNow: s.theWorldNow,
		wake:        make(chan struct{}, 1),
	}
	s.mu.Lock()
	s.connected = append(s.connected, joined)
	s.mu.Unlock()

	s.logger.Info("a subscriber connected",
		"name", hello.Name, "wake_on", hello.WakeOn)

	// Once, now, with nothing having moved: a subscriber that has just arrived
	// has a world to be told about and no change to be told of (§A7.6).
	joined.poke()
	go joined.write()
	joined.read()

	s.drop(joined)
}

func (s *Subscribers) refuse(connection net.Conn, reason string) {
	defer connection.Close()
	s.logger.Warn("refused a subscriber", "reason", reason)
	encoded, err := json.Marshal(session.Refused{
		Kind: session.KindRefused, Reason: reason, Version: session.Version,
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

// PokeEveryone tells every subscriber that the world may have moved.
//
// It never blocks, and it says nothing about WHAT moved: each subscriber works
// that out for itself against the world it last sent, which is the same
// question a renderer answers for itself on the same tick. That is why
// record-agent-event can never be stalled by a slow display, which is the
// failure this whole shape exists to prevent (§A9.3, R1).
func (s *Subscribers) PokeEveryone() {
	s.mu.Lock()
	listening := append([]*subscriber(nil), s.connected...)
	s.mu.Unlock()

	for _, one := range listening {
		one.poke()
	}
}

// Connected is who is listening: for doctor, and for a test waiting to play
// something at a display that has arrived.
//
// Names, and nothing else. It used to answer with a pid as well, asked of the
// kernel because the supervisor had to tell the child it started from something
// merely calling itself that name (R25) — and core starts none of this now.
func (s *Subscribers) Connected() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make([]string, 0, len(s.connected))
	for _, one := range s.connected {
		names = append(names, one.hello.Name)
	}
	return names
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

// A subscriber is one connection, and the goroutine that writes to it.
//
// It keeps what a renderer keeps and for the same reason — one slot to be woken
// in, and a picture of what it last handed over — but the picture answers a
// different question here. A renderer's says what CHANGED, because the display
// on the other end is a fresh process that remembers nothing. This one only
// says whether to write at all: the client on the far end survives its own
// reconnection and this session-watcher's restart, so it is the end that can
// say what changed, and it is the end that does.
type subscriber struct {
	hello       session.Hello
	connection  net.Conn
	reader      *bufio.Reader
	logger      *slog.Logger
	theWorldNow func(wantEnded bool) []session.Record

	// wake holds at most one. That single slot is both halves of the problem:
	// it COALESCES, because every write carries the whole world and a waiting
	// one is therefore never worth keeping beside a newer one; and it
	// SERIALISES, because the loop reading it writes one world at a time.
	wake chan struct{}

	// sent is the world this connection was last given, touched only by the
	// write goroutine. It is what keeps `wake_on` meaning something on the
	// wire: without it, a subscriber that asked about kernels alone is written
	// to every time an agent spends a token (R23).
	sent session.LastShown

	mu   sync.Mutex
	done bool
}

// poke asks for a write, and never waits for one.
func (s *subscriber) poke() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return
	}
	select {
	case s.wake <- struct{}{}:
	default:
		// One is already waiting, and it will send the same world this one
		// would have.
	}
}

// write is the only goroutine that touches this connection's write side, so a
// slow peer blocks here and nowhere else.
func (s *subscriber) write() {
	for range s.wake {
		if !s.sendTheWorld() {
			return
		}
	}
}

func (s *subscriber) sendTheWorld() bool {
	first := !s.sent.HasSeenAView()
	moved, departed := s.sent.Replace(s.theWorldNow(s.hello.WantEnded), s.hello.WakeOn)
	if !first && len(moved) == 0 && !departed {
		// Nothing this subscriber asked about moved. The changes are thrown
		// away rather than sent: they are this end's answer to "is this worth
		// writing", and the far end's memory outlives this connection, so its
		// answer to "what changed" is the one that survives a restart (R22).
		return true
	}

	// ViewOf is what a renderer calls here too. Only half of what it returns is
	// wanted on the wire — the world, not the changes — and calling it anyway is
	// what marks this connection as having been handed something.
	world := s.sent.ViewOf(moved)
	encoded, err := json.Marshal(session.Snapshot{
		Kind: session.KindSnapshot, Sessions: world.Sessions,
	})
	if err != nil {
		s.logger.Warn("cannot encode the world", "problem", err.Error())
		return true
	}
	_ = s.connection.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if _, err := s.connection.Write(append(encoded, '\n')); err != nil {
		s.logger.Info("a subscriber stopped reading", "problem", err.Error())
		return false
	}
	return true
}

// read exists to notice the far end going away.
//
// A subscriber says one thing — its hello — and after that the conversation is
// one-way. It used to be able to ask for a fresh snapshot, back when there was
// such a thing as being out of date; every write is one now.
func (s *subscriber) read() {
	for {
		line, err := s.reader.ReadBytes('\n')
		if err != nil {
			return
		}
		// Carried and ignored: a newer subscriber within the same major
		// version may say things this build has never heard of (R12).
		if kind, err := session.KindOf(line); err == nil {
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
	close(s.wake)
	s.mu.Unlock()
	_ = s.connection.Close()
}
