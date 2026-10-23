//go:build darwin || windows

package capture

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spytify/spytify/internal/pcm"
	"github.com/spytify/spytify/internal/target"
)

// helperExitChanged is the status a capture helper exits with when what it
// records went away or changed (the player's processes, the output rate);
// it is started again at once.
const helperExitChanged = 3

// A helper that stays silent this long is stuck in the audio system and is
// started again: a running tap delivers frames, silent or not. Before its
// first frame the helper may be waiting for the user to answer macOS's
// permission prompt, which killing it would close: that wait is long.
var (
	headerTimeout = 90 * time.Second
	frameTimeout  = 3 * time.Second
)

// Helper records the target through a small native program, spytify-tap,
// that writes a header line then frames to stdout: a Core Audio process
// tap on macOS, WASAPI process loopback on Windows. The helper exits when
// what it records changes, and is started again here.
type Helper struct {
	logf   func(format string, args ...any)
	helper string
	// args returns the helper's arguments for the next run, or false while
	// the target cannot be recorded yet.
	args func(target.Target) ([]string, bool)
	// running reports whether the target runs; while it does not, a helper
	// that waits for it has nothing to say yet.
	running func(target.Target) bool

	frames   chan Chunk
	overruns atomic.Int64
	cancel   context.CancelFunc
	done     chan struct{}
	once     sync.Once
}

func newHelper(helper string, args func(target.Target) ([]string, bool), logf func(format string, args ...any)) *Helper {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Helper{logf: logf, helper: helper, args: args, frames: make(chan Chunk, 256), done: make(chan struct{})}
}

// FindTapHelper locates spytify-tap: the given path, next to the running
// binary, then on PATH.
func FindTapHelper(path string) (string, error) {
	if path != "" {
		if _, err := os.Stat(path); err != nil {
			return "", fmt.Errorf("SPYTIFY_TAP: %w", err)
		}
		return path, nil
	}
	name := "spytify-tap" + exeSuffix
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), name)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("%s not found next to spytifyd or on PATH; %s, or set SPYTIFY_TAP", name, buildHint)
}

func (c *Helper) Open(ctx context.Context, t target.Target) (pcm.Format, error) {
	rctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	go c.run(rctx, t)
	return pcm.Format{}, nil
}

func (c *Helper) Frames() <-chan Chunk { return c.frames }
func (c *Helper) Overruns() int64      { return c.overruns.Load() }

func (c *Helper) Close() error {
	c.once.Do(func() {
		if c.cancel != nil {
			c.cancel()
			<-c.done
		}
	})
	return nil
}

func (c *Helper) run(ctx context.Context, t target.Target) {
	defer close(c.done)
	defer close(c.frames)
	waiting := false
	for ctx.Err() == nil {
		args, ok := c.args(t)
		if !ok {
			if !waiting {
				c.logf("capture: waiting for the player to start")
				waiting = true
			}
			sleep(ctx, time.Second)
			continue
		}
		waiting = false
		code := c.stream(ctx, args, t)
		if ctx.Err() != nil {
			return
		}
		if code != helperExitChanged {
			sleep(ctx, time.Second)
		}
	}
}

// stream runs the helper once and returns its exit status.
func (c *Helper) stream(ctx context.Context, args []string, t target.Target) int {
	cmd := exec.CommandContext(ctx, c.helper, args...)
	cmd.Cancel = func() error { return stopHelper(cmd.Process) }
	cmd.WaitDelay = 2 * time.Second
	hideWindow(cmd)
	out, err := cmd.StdoutPipe()
	if err != nil {
		c.logf("capture: %v", err)
		return 1
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		c.logf("capture: %v", err)
		return 1
	}
	if err := cmd.Start(); err != nil {
		c.logf("capture: start %s: %v", c.helper, err)
		return 1
	}
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			c.logf("capture: %s", strings.TrimPrefix(sc.Text(), "spytify-tap: "))
		}
	}()
	r := bufio.NewReaderSize(out, 1<<16)
	type header struct {
		f   pcm.Format
		err error
	}
	got := make(chan header, 1)
	go func() {
		f, err := readTapHeader(r)
		got <- header{f, err}
	}()
	timer := time.NewTimer(headerTimeout)
	defer timer.Stop()
wait:
	for {
		select {
		case h := <-got:
			if h.err != nil {
				if !errors.Is(h.err, io.EOF) {
					c.logf("capture: %v", h.err)
				}
			} else {
				c.readFrames(ctx, r, h.f, cmd.Process)
			}
			break wait
		case <-timer.C:
			if c.running != nil && !c.running(t) {
				timer.Reset(headerTimeout)
				continue
			}
			c.logf("capture: the helper got no answer from the audio system in %s; starting it again", headerTimeout)
			// Wait closes the pipes, which ends the header read.
			_ = cmd.Process.Kill()
			break wait
		}
	}
	err = cmd.Wait()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return 0
}

func (c *Helper) readFrames(ctx context.Context, r io.Reader, f pcm.Format, p *os.Process) {
	chunk := make([]byte, f.FramesFromMs(10)*int64(f.FrameBytes()))
	stalled := time.AfterFunc(frameTimeout, func() {
		c.logf("capture: no audio from the helper for %s; starting it again", frameTimeout)
		_ = p.Kill()
	})
	defer stalled.Stop()
	for {
		if _, err := io.ReadFull(r, chunk); err != nil {
			return
		}
		stalled.Reset(frameTimeout)
		select {
		case c.frames <- Chunk{Data: append([]byte(nil), chunk...), Format: f}:
		case <-ctx.Done():
			return
		default:
			c.overruns.Add(1)
		}
	}
}
