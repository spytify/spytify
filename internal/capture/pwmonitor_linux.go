//go:build linux

package capture

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/spytify/spytify/internal/proc"
	"github.com/spytify/spytify/internal/target"
)

// monitorPoll is how often the stream is looked for while the graph
// monitor runs. The monitor says when the graph changes, so the poll only
// catches a change it missed, from what it read: it starts no program.
const monitorPoll = 2 * time.Second

// graphMonitor is a backend that can follow the graph for the whole
// capture, instead of reading all of it at every poll.
type graphMonitor interface {
	monitor(ctx context.Context) (*pwMonitor, error)
}

// pwMonitor follows PipeWire's graph with one pw-dump --monitor (#833).
// pw-dump writes the whole graph as a JSON array, then one array per
// change, each object whole, a removed one as {"id": N, "info": null}. It
// keeps the nodes and clients, which is all finding the stream reads.
type pwMonitor struct {
	// changed receives after a node or client changed, and once more when
	// the monitor ends.
	changed chan struct{}

	mu    sync.Mutex
	objs  map[int]pwObject
	ready bool // the whole graph was read
	ended error
}

func newMonitor() *pwMonitor {
	return &pwMonitor{changed: make(chan struct{}, 1), objs: map[int]pwObject{}}
}

func (pipewireTools) monitor(ctx context.Context) (*pwMonitor, error) {
	cmd := proc.CommandContext(ctx, "pw-dump", "--monitor", "--no-colors")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("pw-dump --monitor: %w", err)
	}
	m := newMonitor()
	go func() {
		err := m.read(out)
		// Wait reaps pw-dump; its status says more than a closed pipe.
		if werr := cmd.Wait(); werr != nil && ctx.Err() == nil {
			err = fmt.Errorf("pw-dump --monitor: %w", werr)
		}
		m.end(err)
	}()
	return m, nil
}

// pwEntry is an object of pw-dump's output as read first: info stays raw,
// "null" for a removed object.
type pwEntry struct {
	ID   int             `json:"id"`
	Type string          `json:"type"`
	Info json.RawMessage `json:"info"`
}

// read applies pw-dump's arrays until r ends or holds something else.
func (m *pwMonitor) read(r io.Reader) error {
	dec := json.NewDecoder(r)
	for {
		var update []pwEntry
		if err := dec.Decode(&update); err != nil {
			if errors.Is(err, io.EOF) {
				return errors.New("pw-dump --monitor ended")
			}
			return fmt.Errorf("pw-dump --monitor output: %w", err)
		}
		if m.apply(update) {
			m.signal()
		}
	}
}

// apply keeps what an array says of the nodes and clients, and reports
// whether any of them changed. The first array is the whole graph.
func (m *pwMonitor) apply(update []pwEntry) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	changed := !m.ready
	m.ready = true
	for _, e := range update {
		if string(e.Info) == "null" {
			if _, ok := m.objs[e.ID]; ok {
				delete(m.objs, e.ID)
				changed = true
			}
			continue
		}
		if e.Type != "PipeWire:Interface:Node" && e.Type != "PipeWire:Interface:Client" {
			continue
		}
		o := pwObject{ID: e.ID, Type: e.Type}
		if json.Unmarshal(e.Info, &o.Info) != nil {
			continue
		}
		m.objs[e.ID] = o
		changed = true
	}
	return changed
}

func (m *pwMonitor) signal() {
	select {
	case m.changed <- struct{}{}:
	default:
	}
}

// end notes why the monitor stopped; from then on find says it cannot
// answer.
func (m *pwMonitor) end(err error) {
	m.mu.Lock()
	m.ended = err
	if m.ended == nil {
		m.ended = errors.New("pw-dump --monitor ended")
	}
	m.mu.Unlock()
	m.signal()
}

// find picks the target's stream from the graph read so far. usable is
// false until the whole graph is read and once the monitor has ended:
// the caller then looks with a pw-dump of its own.
func (m *pwMonitor) find(t target.Target) (n streamNode, found, usable bool) {
	m.mu.Lock()
	if !m.ready || m.ended != nil {
		m.mu.Unlock()
		return streamNode{}, false, false
	}
	objs := make([]pwObject, 0, len(m.objs))
	for _, o := range m.objs {
		objs = append(objs, o)
	}
	m.mu.Unlock()
	n, found = pickPipeWireNode(objs, t)
	return n, found, true
}

// err is why the monitor ended, nil while it runs.
func (m *pwMonitor) err() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ended
}
