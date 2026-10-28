//go:build linux

package capture

import (
	"context"
	"os/exec"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/spytify/spytify/internal/pcm"
	"github.com/spytify/spytify/internal/target"
)

// fakeTools stands in for pw-dump and pw-record: find returns the stream
// the test sets, and record streams silence from /dev/zero.
type fakeTools struct {
	mu       sync.Mutex
	node     *streamNode
	recorded []string
	silenced []string
	restored []string
	dropped  bool
}

func (f *fakeTools) set(n *streamNode) {
	f.mu.Lock()
	f.node = n
	f.mu.Unlock()
}

func (f *fakeTools) name() string { return "fake" }

func (f *fakeTools) find(target.Target) (streamNode, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.node == nil {
		return streamNode{}, false, nil
	}
	return *f.node, true, nil
}

func (f *fakeTools) record(ctx context.Context, n streamNode, _ pcm.Format) *exec.Cmd {
	f.mu.Lock()
	f.recorded = append(f.recorded, n.key)
	f.mu.Unlock()
	return exec.CommandContext(ctx, "cat", "/dev/zero")
}

func (f *fakeTools) silence(n streamNode) error {
	f.mu.Lock()
	f.silenced = append(f.silenced, n.key)
	f.mu.Unlock()
	return nil
}

func (f *fakeTools) unsilence(n streamNode) error {
	f.mu.Lock()
	f.restored = append(f.restored, n.key)
	f.mu.Unlock()
	return nil
}

func (f *fakeTools) dropSink() {
	f.mu.Lock()
	f.dropped = true
	f.mu.Unlock()
}

func (f *fakeTools) records() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.recorded)
}

// fakeClock stands in for pw-top and pw-metadata: each driver runs at the
// rate the test sets, and an unknown driver at no known rate.
type fakeClock struct {
	mu    sync.Mutex
	rates map[int]int
	s     clockSettings
}

func (c *fakeClock) set(driver, rate int) {
	c.mu.Lock()
	if c.rates == nil {
		c.rates = map[int]int{}
	}
	c.rates[driver] = rate
	c.mu.Unlock()
}

func (c *fakeClock) running(_ context.Context, driver int) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.rates[driver]
}

func (c *fakeClock) settings(context.Context) clockSettings {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.s
}

func openFake(t *testing.T, tools *fakeTools, mute bool) *PipeWire {
	t.Helper()
	return openFakeClock(t, tools, &fakeClock{}, mute)
}

func openFakeClock(t *testing.T, tools *fakeTools, clock *fakeClock, mute bool) *PipeWire {
	t.Helper()
	p := &PipeWire{MuteOutput: mute, logf: t.Logf, backend: tools, clock: clock, frames: make(chan Chunk, 256), done: make(chan struct{})}
	if _, err := p.Open(context.Background(), target.Target{ProcessNames: []string{"player"}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	return p
}

// waitFormat reads chunks until one comes in format f.
func waitFormat(t *testing.T, p *PipeWire, f pcm.Format) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case c, ok := <-p.Frames():
			if !ok {
				t.Fatal("the capture ended")
			}
			if c.Format == f {
				return
			}
		case <-deadline:
			t.Fatalf("no chunk in %v", f)
		}
	}
}

// When the player opens a new stream, as it does for an item at another
// rate, the capture records the new one in its format.
func TestTheCaptureFollowsANewStream(t *testing.T) {
	tools := &fakeTools{}
	tools.set(&streamNode{key: "55", rate: 44100, channels: 2, running: true})
	p := openFake(t, tools, false)
	waitFormat(t, p, pcm.Format{Rate: 44100, Channels: 2, Sample: pcm.F32LE})
	tools.set(&streamNode{key: "61", rate: 48000, channels: 2, running: true})
	waitFormat(t, p, pcm.Format{Rate: 48000, Channels: 2, Sample: pcm.F32LE})
	if got := tools.records(); !slices.Equal(got, []string{"55", "61"}) {
		t.Fatalf("recorded streams %v", got)
	}
}

// A stream the player keeps, moved to another output (a Bluetooth speaker
// gone, the default output changed) whose graph runs at the same rate, is
// the same stream: no restart.
func TestTheSameStreamOnAnotherOutputIsNotRestarted(t *testing.T) {
	tools, clock := &fakeTools{}, &fakeClock{}
	clock.set(33, 48000)
	clock.set(40, 48000)
	tools.set(&streamNode{key: "55", driver: 33, rate: 48000, channels: 2, running: true})
	p := openFakeClock(t, tools, clock, false)
	waitFormat(t, p, pcm.Format{Rate: 48000, Channels: 2, Sample: pcm.F32LE})
	tools.set(&streamNode{key: "55", rate: 48000, channels: 2, running: false})
	time.Sleep(4 * pollInterval)
	tools.set(&streamNode{key: "55", driver: 40, rate: 48000, channels: 2, running: true})
	time.Sleep(rateInterval + 4*pollInterval)
	if got := tools.records(); !slices.Equal(got, []string{"55"}) {
		t.Fatalf("recorded streams %v, want one", got)
	}
}

// PipeWire converts a 44.1 kHz stream to the 48 kHz its graph runs at:
// the capture records 48 kHz, so it does not convert it back, and the
// stream's own rate, which it gets a moment after it appears, starts no
// restart.
func TestTheCaptureRecordsAtTheRatePipeWireRunsAt(t *testing.T) {
	tools, clock := &fakeTools{}, &fakeClock{}
	clock.set(33, 48000)
	tools.set(&streamNode{key: "55", driver: 33, channels: 2, running: true})
	p := openFakeClock(t, tools, clock, false)
	waitFormat(t, p, pcm.Format{Rate: 48000, Channels: 2, Sample: pcm.F32LE})
	tools.set(&streamNode{key: "55", driver: 33, rate: 44100, channels: 2, running: true})
	time.Sleep(rateInterval + 4*pollInterval)
	if got := tools.records(); !slices.Equal(got, []string{"55"}) {
		t.Fatalf("recorded streams %v, want one", got)
	}
}

// When the graph runs at another rate, as when the stream moves to a
// Bluetooth headset in a call, the capture starts again at that rate.
func TestTheCaptureFollowsTheRatePipeWireRunsAt(t *testing.T) {
	tools, clock := &fakeTools{}, &fakeClock{}
	clock.set(33, 48000)
	tools.set(&streamNode{key: "55", driver: 33, rate: 44100, channels: 2, running: true})
	p := openFakeClock(t, tools, clock, false)
	waitFormat(t, p, pcm.Format{Rate: 48000, Channels: 2, Sample: pcm.F32LE})
	clock.set(33, 44100)
	waitFormat(t, p, pcm.Format{Rate: 44100, Channels: 2, Sample: pcm.F32LE})
	clock.set(40, 16000)
	tools.set(&streamNode{key: "55", driver: 40, rate: 44100, channels: 2, running: true})
	waitFormat(t, p, pcm.Format{Rate: 16000, Channels: 2, Sample: pcm.F32LE})
	if got := tools.records(); !slices.Equal(got, []string{"55", "55", "55"}) {
		t.Fatalf("recorded streams %v", got)
	}
}

// Without pw-top's rate, PipeWire's settings choose it; without them, the
// stream's rate, which the capture then follows.
func TestTheCaptureFallsBackOnPipeWiresSettings(t *testing.T) {
	tools := &fakeTools{}
	tools.set(&streamNode{key: "55", driver: 33, rate: 44100, channels: 2, running: true})
	p := openFakeClock(t, tools, &fakeClock{s: clockSettings{allowed: []int{48000}}}, false)
	waitFormat(t, p, pcm.Format{Rate: 48000, Channels: 2, Sample: pcm.F32LE})
	tools = &fakeTools{}
	tools.set(&streamNode{key: "55", driver: 33, rate: 44100, channels: 2, running: true})
	p = openFakeClock(t, tools, &fakeClock{s: clockSettings{allowed: []int{44100, 48000}}}, false)
	waitFormat(t, p, pcm.Format{Rate: 44100, Channels: 2, Sample: pcm.F32LE})
	tools.set(&streamNode{key: "55", driver: 33, rate: 48000, channels: 2, running: true})
	waitFormat(t, p, pcm.Format{Rate: 48000, Channels: 2, Sample: pcm.F32LE})
}

// With no stream, the capture waits for one instead of ending.
func TestTheCaptureWaitsForAStream(t *testing.T) {
	tools := &fakeTools{}
	p := openFake(t, tools, false)
	time.Sleep(3 * pollInterval)
	if got := tools.records(); len(got) != 0 {
		t.Fatalf("recorded %v with no stream", got)
	}
	tools.set(&streamNode{key: "70", rate: 48000, channels: 2, running: true})
	waitFormat(t, p, pcm.Format{Rate: 48000, Channels: 2, Sample: pcm.F32LE})
}

// A muted player plays into the muted sink while it is recorded, and on
// the default output again once the capture closes.
func TestAMutedPlayerGetsItsOutputBack(t *testing.T) {
	tools := &fakeTools{}
	tools.set(&streamNode{key: "55", rate: 48000, channels: 2, running: true})
	p := openFake(t, tools, true)
	waitFormat(t, p, pcm.Format{Rate: 48000, Channels: 2, Sample: pcm.F32LE})
	p.Close()
	tools.mu.Lock()
	defer tools.mu.Unlock()
	if !slices.Equal(tools.silenced, []string{"55"}) || !slices.Equal(tools.restored, []string{"55"}) || !tools.dropped {
		t.Fatalf("silenced %v, restored %v, sink dropped %v", tools.silenced, tools.restored, tools.dropped)
	}
}
