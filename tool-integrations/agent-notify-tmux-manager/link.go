package main

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync"
)

// The list and the stand-in are two processes in two panes, joined by a unix
// socket the list listens on. Down it goes what the stand-in should show; up
// it come the two things that happen to a stand-in. The socket is also how a
// stand-in knows the list is gone, however it went: the kernel closes it.

// Showing is what the list tells the stand-in to draw: a title for its pane
// and a few lines for the middle of it.
type Showing struct {
	Title string   `json:"title"`
	Lines []string `json:"lines"`
}

// The two things a stand-in reports.
const (
	// gotFocus: its pane became the one a terminal is on. On the stage, that
	// is somebody arriving at the tab; holding a place, somebody arriving at
	// the window the agent lives in.
	gotFocus = "focus"
	// pressedEnter: Enter was typed into it, which asks for the agent whose
	// place it holds.
	pressedEnter = "enter"
)

// Link is the list's end.
type Link struct {
	listener  net.Listener
	directory string

	mutex   sync.Mutex
	standIn net.Conn
	showing Showing
}

// Listen opens the socket. `heard` is called with whatever a stand-in reports.
// The directory is made by hand because a unix socket's path is limited to
// about a hundred bytes.
func Listen(heard func(string)) (*Link, error) {
	directory, err := os.MkdirTemp("", "an-tab-")
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("unix", filepath.Join(directory, "s"))
	if err != nil {
		_ = os.RemoveAll(directory)
		return nil, err
	}
	link := &Link{listener: listener, directory: directory}
	go link.accept(heard)
	return link, nil
}

// Path is what a stand-in is started with.
func (l *Link) Path() string { return filepath.Join(l.directory, "s") }

// accept takes each stand-in that connects. The latest is the one spoken to:
// a respawned stand-in replaces the one that died.
func (l *Link) accept(heard func(string)) {
	for {
		connection, err := l.listener.Accept()
		if err != nil {
			return
		}
		l.mutex.Lock()
		l.standIn = connection
		l.send()
		l.mutex.Unlock()
		go func() {
			lines := bufio.NewScanner(connection)
			for lines.Scan() {
				heard(lines.Text())
			}
			connection.Close()
		}()
	}
}

// Show tells the stand-in what to draw, now and whenever one connects.
func (l *Link) Show(showing Showing) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.showing = showing
	l.send()
}

// send is called with the mutex held. A write that fails is a stand-in that
// is gone, which the next look at the server will see for itself.
func (l *Link) send() {
	if l.standIn == nil {
		return
	}
	line, _ := json.Marshal(l.showing)
	_, _ = l.standIn.Write(append(line, '\n'))
}

// Close ends the link, which is what tells a stand-in the list is gone.
func (l *Link) Close() {
	_ = l.listener.Close()
	l.mutex.Lock()
	if l.standIn != nil {
		_ = l.standIn.Close()
	}
	l.mutex.Unlock()
	_ = os.RemoveAll(l.directory)
}
