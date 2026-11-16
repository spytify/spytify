package main

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spytify/spytify/internal/bus"
)

// events and tail -f say on stderr how many events the output was too slow
// for, once each time the count moves. The events come straight down the
// socket, as spytifyd writes them.
func TestLostEventsAreSaid(t *testing.T) {
	// macOS caps socket paths at 104 bytes, shorter than t.TempDir().
	dir, err := os.MkdirTemp("", "spy")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "s.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	send := make(chan struct{})
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		<-send
		line := []byte(`{"jsonrpc":"2.0","method":"session.state","params":{"state":"recording"}}` + "\n")
		for range 1030 {
			if _, err := conn.Write(line); err != nil {
				return
			}
		}
		time.Sleep(5 * time.Second)
	}()
	c, err := bus.Dial(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var stderr bytes.Buffer
	var told int64
	lost(c, &stderr, &told)
	if stderr.Len() != 0 {
		t.Fatalf("said %q with nothing lost", stderr.String())
	}
	close(send)
	deadline := time.Now().Add(5 * time.Second)
	for c.Dropped() != 6 {
		if time.Now().After(deadline) {
			t.Fatalf("%d events lost, want 6", c.Dropped())
		}
		time.Sleep(10 * time.Millisecond)
	}
	lost(c, &stderr, &told)
	lost(c, &stderr, &told)
	if got, want := stderr.String(), "spytify-cli: 6 events lost: the output did not keep up\n"; got != want {
		t.Fatalf("said %q, want %q", got, want)
	}
}
