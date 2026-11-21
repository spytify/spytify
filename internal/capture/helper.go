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
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spytify/spytify/internal/pcm"
	"github.com/spytify/spytify/internal/proc"
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

// A helper that exits without its header, as one refused permission or
// quarantined does at once, is started again after retryFirst, then twice
// as late each time up to retryLast; one that gave its header starts the
// wait over. A helper started every second for a whole session wrote its
// stderr to the log every second (#836).
var (
	retryFirst = time.Second
	retryLast  = 30 * time.Second
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
	// headerTimeout, frameTimeout, retryFirst and retryLast are the
	// package's, read once.
	headerTimeout, frameTimeout time.Duration
	retryFirst, retryLast       time.Duration

	frames   chan Chunk
	volumes  chan float64
	overruns atomic.Int64
	cancel   context.CancelFunc
	done     chan struct{}
	once     sync.Once

	// What the helper did, for Health: how many times it ran, its last
	// exit statuses, whether this run gave its header, whether it waits
	// for the player, and its last stderr lines without a device's name.
	mu      sync.Mutex
	runs    int
	exits   []int
	header  bool
	waiting bool
	stderr  lineRing
}

func newHelper(helper string, args func(target.Target) ([]string, bool), logf func(format string, args ...any)) *Helper {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Helper{logf: logf, helper: helper, args: args, headerTimeout: headerTimeout, frameTimeout: frameTimeout,
		retryFirst: retryFirst, retryLast: retryLast, frames: make(chan Chunk, queuedChunks), volumes: make(chan float64, 1), done: make(chan struct{})}
}

// ErrTapHelperNotFound is FindTapHelper's error when spytify-tap is neither
// next to the running binary nor on PATH: the window words it for the user.
var ErrTapHelperNotFound = errors.New("not found next to spytifyd or on PATH")

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
	return "", fmt.Errorf("%s %w; %s, or set SPYTIFY_TAP", name, ErrTapHelperNotFound, buildHint)
}

func (c *Helper) Open(ctx context.Context, t target.Target) (pcm.Format, error) {
	rctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	go c.run(rctx, t)
	return pcm.Format{}, nil
}

func (c *Helper) Frames() <-chan Chunk { return c.frames }
func (c *Helper) Overruns() int64      { return c.overruns.Load() }

// Volumes gives the player's volume in the Volume Mixer, from 0 to 1, each
// time the running helper says it changed: the Windows helper reads it
// while it records, which saves starting it once more to ask. A value not
// taken yet gives way to the next one.
func (c *Helper) Volumes() <-chan float64 { return c.volumes }

// volume hands v to Volumes, in place of a value not taken yet.
func (c *Helper) volume(v float64) {
	select {
	case <-c.volumes:
	default:
	}
	select {
	case c.volumes <- v:
	default:
	}
}

// Health says what the helper did; see Healther.
func (c *Helper) Health() map[string]string {
	c.mu.Lock()
	exits := make([]string, len(c.exits))
	for i, e := range c.exits {
		exits[i] = strconv.Itoa(e)
	}
	h := map[string]string{
		"capture.backend":            helperBackend,
		"capture.restarts":           strconv.Itoa(max(c.runs-1, 0)),
		"capture.last_exits":         strings.Join(exits, ","),
		"capture.header":             yesNo(c.header),
		"capture.waiting_for_player": yesNo(c.waiting),
	}
	c.mu.Unlock()
	if e := c.stderr.String(); e != "" {
		h["capture.stderr"] = e
	}
	return h
}

func (c *Helper) setWaiting(w bool) {
	c.mu.Lock()
	c.waiting = w
	c.mu.Unlock()
}

// exited notes a run's exit status, the last three kept.
func (c *Helper) exited(code int) {
	c.mu.Lock()
	c.exits = append(c.exits, code)
	if len(c.exits) > 3 {
		c.exits = c.exits[len(c.exits)-3:]
	}
	c.mu.Unlock()
}

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
	// retry is the wait before the next run; said, the one last logged.
	retry, said := c.retryFirst, time.Duration(0)
	for ctx.Err() == nil {
		args, ok := c.args(t)
		if !ok {
			if !waiting {
				c.logf("capture: waiting for the player to start")
				waiting = true
				c.setWaiting(true)
			}
			sleep(ctx, time.Second)
			continue
		}
		waiting = false
		c.setWaiting(false)
		code := c.stream(ctx, args, t)
		if ctx.Err() != nil {
			return
		}
		c.exited(code)
		c.mu.Lock()
		header := c.header
		c.mu.Unlock()
		if header {
			retry, said = c.retryFirst, 0
		}
		if code == helperExitChanged {
			continue
		}
		if retry > c.retryFirst && retry != said {
			c.logf("capture: the helper keeps ending with no audio; trying again in %s", retry)
			said = retry
		}
		sleep(ctx, retry)
		if !header {
			retry = min(retry*2, c.retryLast)
		}
	}
}

// stream runs the helper once and returns its exit status.
func (c *Helper) stream(ctx context.Context, args []string, t target.Target) int {
	c.mu.Lock()
	c.runs++
	c.header = false
	c.mu.Unlock()
	cmd := proc.CommandContext(ctx, c.helper, args...)
	cmd.Cancel = func() error { return stopHelper(cmd.Process) }
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
	logged := make(chan struct{})
	go func() {
		defer close(logged)
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			line := strings.TrimPrefix(sc.Text(), "spytify-tap: ")
			// Not logged nor kept for the health: the slider is reported
			// as a volume change, and it would push out the lines that
			// say what the helper did.
			if v, ok := volumeLine(line); ok {
				c.volume(v)
				continue
			}
			c.logf("capture: %s", line)
			c.stderr.add(deviceless(line))
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
	timer := time.NewTimer(c.headerTimeout)
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
				c.mu.Lock()
				c.header = true
				c.mu.Unlock()
				c.readFrames(ctx, r, h.f, cmd.Process)
			}
			break wait
		case <-timer.C:
			if c.running != nil && !c.running(t) {
				timer.Reset(c.headerTimeout)
				continue
			}
			c.logf("capture: the helper got no answer from the audio system in %s; starting it again", c.headerTimeout)
			// Wait closes the pipes, which ends the header read.
			_ = cmd.Process.Kill()
			break wait
		}
	}
	// Wait closes stderr: the lines the helper wrote as it exited are read
	// first, unless something it started keeps the pipe open.
	select {
	case <-logged:
	case <-time.After(cmd.WaitDelay):
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
	stalled := time.AfterFunc(c.frameTimeout, func() {
		c.logf("capture: no audio from the helper for %s; starting it again", c.frameTimeout)
		_ = p.Kill()
	})
	defer stalled.Stop()
	for {
		if _, err := io.ReadFull(r, chunk); err != nil {
			return
		}
		stalled.Reset(c.frameTimeout)
		select {
		case c.frames <- Chunk{Data: append([]byte(nil), chunk...), Format: f}:
		case <-ctx.Done():
			return
		default:
			c.overruns.Add(1)
		}
	}
}

// volumeLine reads a helper's "volume 0.850" line.
func volumeLine(line string) (float64, bool) {
	s, ok := strings.CutPrefix(line, "volume ")
	if !ok {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 || v > 1 {
		return 0, false
	}
	return v, true
}

// The helper names the output device in these lines: Windows' mix rate
// lines, macOS's tap line.
var (
	mixRateLine = regexp.MustCompile(`^(recording \d+ at \d+ Hz, the mix rate of ).*?(, the default output)?$`)
	mixesLine   = regexp.MustCompile(`^.* (now mixes at \d+ Hz)$`)
	tapLine     = regexp.MustCompile(`^(tapping .*?\]) on .* (at \d+ Hz \(tap reports \d+ Hz\))$`)
)

// deviceless is a helper's stderr line with the output device's name, which
// the user chose, replaced by "a device".
func deviceless(line string) string {
	switch {
	case mixRateLine.MatchString(line):
		return mixRateLine.ReplaceAllString(line, "${1}a device$2")
	case mixesLine.MatchString(line):
		return mixesLine.ReplaceAllString(line, "a device $1")
	case tapLine.MatchString(line):
		return tapLine.ReplaceAllString(line, "$1 on a device $2")
	}
	return line
}
