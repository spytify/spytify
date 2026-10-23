//go:build linux

package capture

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spytify/spytify/internal/pcm"
	"github.com/spytify/spytify/internal/target"
)

// pollInterval bounds how much of a recreated stream is missed before the
// recorder follows it.
const pollInterval = 250 * time.Millisecond

// streamNode is a player's audio output stream as PipeWire or PulseAudio
// describes it.
type streamNode struct {
	key      string // object.serial or sink-input index
	rate     int
	channels int
	running  bool
	order    int64
}

// PipeWire records the target's output stream with pw-record, found by
// matching pw-dump's stream nodes to the target process. When the player
// recreates its stream (a new item at another rate, a pause that tore the
// stream down) the recorder follows the new node.
type PipeWire struct {
	logf    func(format string, args ...any)
	backend procBackend

	frames   chan Chunk
	overruns atomic.Int64
	cancel   context.CancelFunc
	done     chan struct{}
	once     sync.Once
}

type procBackend interface {
	name() string
	find(t target.Target) (streamNode, bool, error)
	record(ctx context.Context, n streamNode, f pcm.Format) *exec.Cmd
}

// NewLinux picks PipeWire when pw-record and pw-dump exist, PulseAudio's
// parec and pactl otherwise.
func NewLinux(logf func(format string, args ...any)) (*PipeWire, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	var b procBackend
	switch {
	case have("pw-record") && have("pw-dump"):
		b = pipewireTools{}
	case have("parec") && have("pactl"):
		b = pulseTools{}
	default:
		return nil, errors.New("neither PipeWire (pw-record, pw-dump) nor PulseAudio (parec, pactl) tools were found")
	}
	return &PipeWire{logf: logf, backend: b, frames: make(chan Chunk, 256), done: make(chan struct{})}, nil
}

func have(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// Name says which sound server is used.
func (p *PipeWire) Name() string { return p.backend.name() }

func (p *PipeWire) Open(ctx context.Context, t target.Target) (pcm.Format, error) {
	rctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	go p.run(rctx, t)
	return pcm.Format{}, nil
}

func (p *PipeWire) Frames() <-chan Chunk { return p.frames }
func (p *PipeWire) Overruns() int64      { return p.overruns.Load() }

func (p *PipeWire) Close() error {
	p.once.Do(func() {
		if p.cancel != nil {
			p.cancel()
			<-p.done
		}
	})
	return nil
}

func (p *PipeWire) run(ctx context.Context, t target.Target) {
	defer close(p.done)
	defer close(p.frames)
	waiting := false
	for ctx.Err() == nil {
		node, ok, err := p.backend.find(t)
		if err != nil {
			p.logf("capture: %s: %v", p.backend.name(), err)
		}
		if !ok {
			if !waiting {
				p.logf("capture: waiting for an audio stream from %s", t.ProcessName())
				waiting = true
			}
			sleep(ctx, pollInterval)
			continue
		}
		waiting = false
		f := pcm.Format{Rate: node.rate, Channels: node.channels, Sample: pcm.F32LE}
		if f.Rate <= 0 {
			f.Rate = 44100
		}
		if f.Channels <= 0 {
			f.Channels = 2
		}
		p.logf("capture: %s stream %s, %s", p.backend.name(), node.key, f)
		p.stream(ctx, t, node, f)
		sleep(ctx, 100*time.Millisecond)
	}
}

// stream records one node until it goes away, is replaced, or changes rate.
func (p *PipeWire) stream(ctx context.Context, t target.Target, node streamNode, f pcm.Format) {
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := p.backend.record(sctx, node, f)
	out, err := cmd.StdoutPipe()
	if err != nil {
		p.logf("capture: %v", err)
		return
	}
	if err := cmd.Start(); err != nil {
		p.logf("capture: start %s: %v", cmd.Path, err)
		return
	}
	go func() {
		tick := time.NewTicker(pollInterval)
		defer tick.Stop()
		for {
			select {
			case <-sctx.Done():
				return
			case <-tick.C:
				now, ok, err := p.backend.find(t)
				if err != nil {
					continue
				}
				if !ok || now.key != node.key || (now.rate > 0 && now.rate != f.Rate) || (now.channels > 0 && now.channels != f.Channels) {
					cancel()
					return
				}
			}
		}
	}()
	chunk := make([]byte, f.FramesFromMs(10)*int64(f.FrameBytes()))
	for {
		if _, err := io.ReadFull(out, chunk); err != nil {
			break
		}
		c := Chunk{Data: append([]byte(nil), chunk...), Format: f}
		select {
		case p.frames <- c:
		case <-ctx.Done():
			cancel()
			_ = cmd.Wait()
			return
		default:
			p.overruns.Add(1)
		}
	}
	cancel()
	_ = cmd.Wait()
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

type pipewireTools struct{}

func (pipewireTools) name() string { return "pipewire" }

type pwObject struct {
	ID   int    `json:"id"`
	Type string `json:"type"`
	Info struct {
		Props  map[string]any              `json:"props"`
		State  string                      `json:"state"`
		Params map[string][]map[string]any `json:"params"`
	} `json:"info"`
}

func (pipewireTools) find(t target.Target) (streamNode, bool, error) {
	out, err := exec.Command("pw-dump", "--no-colors").Output()
	if err != nil {
		return streamNode{}, false, fmt.Errorf("pw-dump: %w", err)
	}
	return findPipeWireNode(out, t)
}

// findPipeWireNode picks the target's newest running output stream from a
// pw-dump document. Process properties live on the client object, which
// the node points to with client.id.
func findPipeWireNode(dump []byte, t target.Target) (streamNode, bool, error) {
	var objs []pwObject
	if err := json.Unmarshal(dump, &objs); err != nil {
		return streamNode{}, false, fmt.Errorf("pw-dump output: %w", err)
	}
	clients := map[int]map[string]any{}
	for _, o := range objs {
		if o.Type == "PipeWire:Interface:Client" {
			clients[o.ID] = o.Info.Props
		}
	}
	var best streamNode
	found := false
	for _, o := range objs {
		props := o.Info.Props
		if o.Type != "PipeWire:Interface:Node" || str(props["media.class"]) != "Stream/Output/Audio" {
			continue
		}
		client := clients[num(props["client.id"])]
		names := []string{str(client["application.process.binary"]), str(client["application.name"]), str(props["application.process.binary"]), str(props["application.name"]), str(props["node.name"])}
		match := false
		for _, n := range names {
			if t.Matches(n) {
				match = true
				break
			}
		}
		if !match {
			pid := num(client["application.process.id"])
			if pid == 0 {
				pid = num(props["application.process.id"])
			}
			match = pid > 0 && t.MatchesTree(pid)
		}
		if !match {
			continue
		}
		n := streamNode{key: strconv.Itoa(num(props["object.serial"])), running: o.Info.State == "running", order: int64(num(props["object.serial"]))}
		if formats := o.Info.Params["Format"]; len(formats) > 0 {
			n.rate = num(formats[0]["rate"])
			n.channels = num(formats[0]["channels"])
		}
		if !found || (n.running && !best.running) || (n.running == best.running && n.order > best.order) {
			best, found = n, true
		}
	}
	return best, found, nil
}

func (pipewireTools) record(ctx context.Context, n streamNode, f pcm.Format) *exec.Cmd {
	return exec.CommandContext(ctx, "pw-record",
		"--target", n.key,
		"--raw",
		"--rate", strconv.Itoa(f.Rate),
		"--channels", strconv.Itoa(f.Channels),
		"--format", "f32",
		"--latency", "100ms",
		"-P", "{ node.name = spytify-capture node.description = \"Spytify capture\" }",
		"-")
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func num(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case string:
		i, _ := strconv.Atoi(n)
		return i
	}
	return 0
}
