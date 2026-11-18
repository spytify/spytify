package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spytify/spytify/pkg/proto"
)

// countingWriter counts the writes that reach it, as stdout's system calls.
type countingWriter struct {
	strings.Builder
	writes int
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.writes++
	return w.Builder.Write(p)
}

// Events that wait are written in one go, not one system call each, and
// every one of them is there, as a JSON line.
func TestFollowWritesABurstAtOnce(t *testing.T) {
	sub := make(chan proto.Event, 300)
	for range 300 {
		sub <- proto.Event{Method: proto.EventSessionState, Params: json.RawMessage(`{"state":"recording"}`)}
	}
	close(sub)
	out := &countingWriter{}
	err := followEvents(context.Background(), sub, out, writeEvent)
	if err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("followEvents ended with %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) != 300 {
		t.Fatalf("%d lines, want 300", len(lines))
	}
	var m struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if json.Unmarshal([]byte(lines[299]), &m) != nil || m.Method != proto.EventSessionState || string(m.Params) != `{"state":"recording"}` {
		t.Fatalf("last line %q", lines[299])
	}
	// About 60 bytes a line in a 4 KB buffer: a handful of writes.
	if out.writes > 10 {
		t.Fatalf("%d writes for 300 events", out.writes)
	}
}
