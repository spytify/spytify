//go:build linux

package capture

import (
	"context"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spytify/spytify/internal/target"
)

// What pw-dump --monitor writes, cut down to what finding the stream reads,
// in the order PipeWire 1.4 wrote it for a player that starts, plays and
// quits: the whole graph, the player's client and stream, the stream
// running at its rate, a port, then the stream and client removed.
// Metadata has no info at all, which is no removal.
var (
	monGraph = `[
  {"id": 31, "type": "PipeWire:Interface:Metadata", "props": {"metadata.name": "settings"}},
  {"id": 39, "type": "PipeWire:Interface:Node", "info": {"state": "suspended", "props": {"media.class": "Audio/Sink", "node.name": "speakers", "object.serial": 40}}}
]
`
	monPlayer = `[
  {"id": 47, "type": "PipeWire:Interface:Client", "info": {"props": {"application.name": "player"}}},
  {"id": 48, "type": "PipeWire:Interface:Node", "info": {"state": "suspended", "props": {"media.class": "Stream/Output/Audio", "client.id": 47, "object.serial": 60}}}
]
`
	monRunning = `[
  {"id": 39, "type": "PipeWire:Interface:Node", "info": {"state": "running", "props": {"media.class": "Audio/Sink", "node.name": "speakers", "object.serial": 40}}},
  {"id": 48, "type": "PipeWire:Interface:Node", "info": {"state": "running", "props": {"media.class": "Stream/Output/Audio", "client.id": 47, "object.serial": 60, "node.driver-id": 39},
    "params": {"Format": [{"rate": 44100, "channels": 2}]}}}
]
`
	monPort = `[
  {"id": 49, "type": "PipeWire:Interface:Port", "info": {"direction": "output"}}
]
`
	monGone = `[
  {"id": 48, "info": null},
  {"id": 47, "info": null}
]
`
)

func TestTheMonitorFollowsTheGraph(t *testing.T) {
	tg := target.Target{ProcessNames: []string{"player"}}
	m := newMonitor()
	if _, _, usable := m.find(tg); usable {
		t.Fatal("usable before the graph is read")
	}
	r, w := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- m.read(r) }()

	step := func(name, update string, signal bool) {
		t.Helper()
		if _, err := io.WriteString(w, update); err != nil {
			t.Fatal(err)
		}
		select {
		case <-m.changed:
			if !signal {
				t.Fatalf("%s: a change was signalled", name)
			}
		case <-time.After(300 * time.Millisecond):
			if signal {
				t.Fatalf("%s: no change signalled", name)
			}
		}
	}
	step("the graph", monGraph, true)
	if _, found, usable := m.find(tg); !usable || found {
		t.Fatalf("the graph alone: found %v, usable %v", found, usable)
	}
	step("the player", monPlayer, true)
	n, found, _ := m.find(tg)
	if !found || n.key != "60" || n.running {
		t.Fatalf("the player's stream: %+v, found %v", n, found)
	}
	step("running", monRunning, true)
	n, _, _ = m.find(tg)
	if !n.running || n.rate != 44100 || n.channels != 2 || n.driver != 39 || n.id != 48 {
		t.Fatalf("the running stream: %+v", n)
	}
	step("a port", monPort, false)
	step("gone", monGone, true)
	if _, found, usable := m.find(tg); !usable || found {
		t.Fatalf("once the player quits: found %v, usable %v", found, usable)
	}

	w.Close()
	m.end(<-done)
	select {
	case <-m.changed:
	default:
		t.Fatal("the end was not signalled")
	}
	if _, _, usable := m.find(tg); usable || m.err() == nil {
		t.Fatalf("usable after pw-dump ended, err %v", m.err())
	}
}

// A line pw-dump never writes, such as an error message on stdout, ends
// the monitor: the capture goes back to polling.
func TestTheMonitorEndsOnOutputItCannotRead(t *testing.T) {
	m := newMonitor()
	err := m.read(strings.NewReader(monGraph + "pw-dump: oops\n"))
	if err == nil || !strings.Contains(err.Error(), "output") {
		t.Fatalf("read: %v", err)
	}
}

// monitoredTools is fakeTools with a graph monitor the test writes to.
type monitoredTools struct {
	*fakeTools
	mon *pwMonitor
}

func (m monitoredTools) monitor(context.Context) (*pwMonitor, error) { return m.mon, nil }

// With a graph monitor, the capture follows the stream as the monitor sees
// it, well within a poll, and starts no pw-dump; once the monitor ends, it
// polls as before.
func TestTheCaptureFollowsTheMonitor(t *testing.T) {
	tools := monitoredTools{fakeTools: &fakeTools{}, mon: newMonitor()}
	r, w := io.Pipe()
	go func() { tools.mon.end(tools.mon.read(r)) }()
	p := &PipeWire{logf: t.Logf, backend: tools, clock: &fakeClock{}, frames: make(chan Chunk, 256), done: make(chan struct{})}
	if _, err := p.Open(context.Background(), target.Target{ProcessNames: []string{"player"}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	go func() {
		for range p.Frames() {
		}
	}()
	records := func(want ...string) {
		t.Helper()
		// Well under monitorPoll: only the monitor's signal is that fast.
		deadline := time.Now().Add(monitorPoll / 2)
		for !slices.Equal(tools.records(), want) {
			if time.Now().After(deadline) {
				t.Fatalf("recorded %v, want %v", tools.records(), want)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	io.WriteString(w, monGraph)
	io.WriteString(w, monPlayer)
	records("60")
	io.WriteString(w, monGone)
	io.WriteString(w, strings.ReplaceAll(monPlayer, `"object.serial": 60`, `"object.serial": 61`))
	records("60", "61")
	if h := p.Health(); h["capture.graph_monitor"] != "running" {
		t.Fatalf("health %v", h)
	}

	w.Close()
	tools.set(&streamNode{key: "62", rate: 48000, channels: 2, running: true})
	records("60", "61", "62")
	if h := p.Health(); h["capture.graph_monitor"] == "running" {
		t.Fatalf("health %v once the monitor ended", h)
	}
}
