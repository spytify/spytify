//go:build darwin

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
	"syscall"
	"time"

	"github.com/spytify/spytify/internal/pcm"
	"github.com/spytify/spytify/internal/target"
)

// Exit statuses of spytify-tap.
const (
	tapExitChanged = 3
)

// CoreAudio records the target with spytify-tap, a small Swift helper that
// uses a Core Audio process tap (macOS 14.2 and later) and writes a header
// line then f32 frames to stdout. The helper exits when the player's
// processes or the output format change, and is started again here.
type CoreAudio struct {
	logf   func(format string, args ...any)
	helper string
	mute   bool

	frames   chan Chunk
	overruns atomic.Int64
	cancel   context.CancelFunc
	done     chan struct{}
	once     sync.Once
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
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), "spytify-tap")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	if p, err := exec.LookPath("spytify-tap"); err == nil {
		return p, nil
	}
	return "", errors.New("spytify-tap not found next to spytifyd or on PATH; build it with scripts/build-macos.sh or set SPYTIFY_TAP")
}

// MacOSVersion returns the running macOS version, such as "14.2.1".
func MacOSVersion() string {
	v, err := syscall.Sysctl("kern.osproductversion")
	if err != nil {
		return ""
	}
	return v
}

// TapSupported reports whether this macOS has process taps (14.2+).
func TapSupported(version string) bool {
	var major, minor int
	fmt.Sscanf(version, "%d.%d", &major, &minor)
	return major > 14 || (major == 14 && minor >= 2)
}

// NewDarwin prepares the macOS backend. mute makes the player inaudible on
// the speakers while it is recorded (player.mute_output).
func NewDarwin(helperPath string, mute bool, logf func(format string, args ...any)) (*CoreAudio, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	if v := MacOSVersion(); v != "" && !TapSupported(v) {
		return nil, fmt.Errorf("macOS %s has no process taps; macOS 14.2 or later is needed", v)
	}
	helper, err := FindTapHelper(helperPath)
	if err != nil {
		return nil, err
	}
	return &CoreAudio{logf: logf, helper: helper, mute: mute, frames: make(chan Chunk, 256), done: make(chan struct{})}, nil
}

// TapArgs are the spytify-tap arguments that select the target.
func TapArgs(t target.Target, mute bool) []string {
	var args []string
	if t.BundleID != "" {
		args = append(args, "--bundle-id", t.BundleID)
	}
	for _, n := range t.ProcessNames {
		args = append(args, "--name", n)
	}
	if mute {
		args = append(args, "--mute")
	}
	return args
}

func (c *CoreAudio) Open(ctx context.Context, t target.Target) (pcm.Format, error) {
	rctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	go c.run(rctx, TapArgs(t, c.mute))
	return pcm.Format{}, nil
}

func (c *CoreAudio) Frames() <-chan Chunk { return c.frames }
func (c *CoreAudio) Overruns() int64      { return c.overruns.Load() }

func (c *CoreAudio) Close() error {
	c.once.Do(func() {
		if c.cancel != nil {
			c.cancel()
			<-c.done
		}
	})
	return nil
}

func (c *CoreAudio) run(ctx context.Context, args []string) {
	defer close(c.done)
	defer close(c.frames)
	for ctx.Err() == nil {
		code := c.stream(ctx, args)
		if ctx.Err() != nil {
			return
		}
		if code != tapExitChanged {
			sleep(ctx, time.Second)
		}
	}
}

// stream runs the helper once and returns its exit status.
func (c *CoreAudio) stream(ctx context.Context, args []string) int {
	cmd := exec.CommandContext(ctx, c.helper, args...)
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 2 * time.Second
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
	if f, err := readTapHeader(r); err != nil {
		if !errors.Is(err, io.EOF) {
			c.logf("capture: %v", err)
		}
	} else {
		c.readFrames(ctx, r, f)
	}
	err = cmd.Wait()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return 0
}

func (c *CoreAudio) readFrames(ctx context.Context, r io.Reader, f pcm.Format) {
	chunk := make([]byte, f.FramesFromMs(10)*int64(f.FrameBytes()))
	for {
		if _, err := io.ReadFull(r, chunk); err != nil {
			return
		}
		select {
		case c.frames <- Chunk{Data: append([]byte(nil), chunk...), Format: f}:
		case <-ctx.Done():
			return
		default:
			c.overruns.Add(1)
		}
	}
}
