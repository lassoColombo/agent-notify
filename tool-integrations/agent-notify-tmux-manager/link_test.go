package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"
)

func within(t *testing.T, what string, happened <-chan string) string {
	t.Helper()
	select {
	case got := <-happened:
		return got
	case <-time.After(5 * time.Second):
		t.Fatalf("%s never happened", what)
		return ""
	}
}

// TestTheLinkCarriesBothWays, and a stand-in that connects late is told what
// the one before it was.
func TestTheLinkCarriesBothWays(t *testing.T) {
	heard := make(chan string, 4)
	link, err := Listen(func(report string) { heard <- report })
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer link.Close()
	link.Show(Showing{Title: "alpha", Lines: []string{"alpha is in the agents tab"}})

	standIn, err := net.Dial("unix", link.Path())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer standIn.Close()
	told := make(chan string, 4)
	go func() {
		lines := bufio.NewScanner(standIn)
		for lines.Scan() {
			var showing Showing
			_ = json.Unmarshal(lines.Bytes(), &showing)
			told <- showing.Title
		}
		told <- "closed"
	}()

	if got := within(t, "being told what to show", told); got != "alpha" {
		t.Errorf("a stand-in that connected was told %q", got)
	}
	link.Show(Showing{Title: "beta"})
	if got := within(t, "being told again", told); got != "beta" {
		t.Errorf("told %q", got)
	}
	fmt.Fprintln(standIn, gotFocus)
	if got := within(t, "the report", heard); got != gotFocus {
		t.Errorf("the list heard %q", got)
	}

	// The list going is the link closing, which is all a stand-in is told.
	link.Close()
	if got := within(t, "the link closing", told); got != "closed" {
		t.Errorf("after the list closed, the stand-in read %q", got)
	}
}
