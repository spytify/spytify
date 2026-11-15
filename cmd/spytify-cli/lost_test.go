package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spytify/spytify/internal/bus"
	"github.com/spytify/spytify/pkg/proto"
)

// events and tail -f say on stderr how many events the output was too slow
// for, once each time the count moves.
func TestLostEventsAreSaid(t *testing.T) {
	// macOS caps socket paths at 104 bytes, shorter than t.TempDir().
	dir, err := os.MkdirTemp("", "spy")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "s.sock")
	srv := bus.NewServer(func(context.Context, string, json.RawMessage) (any, error) { return "ok", nil }, t.Logf)
	if err := srv.ListenUnix(path); err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	ctx := context.Background()
	c, err := bus.Dial(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Call(ctx, "ping", nil, nil); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	var told int64
	lost(c, &stderr, &told)
	if stderr.Len() != 0 {
		t.Fatalf("said %q with nothing lost", stderr.String())
	}
	for range 1030 {
		srv.Broadcast(proto.EventSessionState, proto.SessionStateEvent{State: proto.StateRecording})
	}
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
