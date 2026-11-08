// Package capture records the PCM a single process sends to the OS mixer.
package capture

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/spytify/spytify/internal/pcm"
	"github.com/spytify/spytify/internal/target"
)

// Chunk is a run of interleaved frames. A chunk whose Format differs from
// the previous one marks a stream the player recreated in another format;
// the session never lets such a change land inside one file.
type Chunk struct {
	Data   []byte
	Format pcm.Format
}

// Capture is one OS capture backend.
type Capture interface {
	// Open finds the target's audio stream and starts delivering frames.
	Open(ctx context.Context, t target.Target) (pcm.Format, error)
	// Frames delivers chunks until the stream ends or Close is called;
	// the channel is then closed.
	Frames() <-chan Chunk
	// Overruns counts chunks dropped because the session fell behind.
	Overruns() int64
	Close() error
}

// Healther is a capture that says what it knows of itself, for the report
// of a capture that delivers nothing: facts as strings, under keys that
// start with "capture.", without a device's name or a path. Health is
// safe from any goroutine.
type Healther interface {
	Health() map[string]string
}

var (
	ErrUnsupported = errors.New("capture is not available on this platform in the alpha")
	ErrNoStream    = errors.New("the target process has no audio stream yet")
)

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// stderrLines and stderrBytes bound what a capture program's stderr adds to
// its health: its last lines, the newest kept when they are too long.
const (
	stderrLines = 5
	stderrBytes = 1000
)

// lineRing keeps the last lines a capture program wrote to stderr. It is
// written line by line or as an io.Writer, and read from any goroutine.
type lineRing struct {
	mu    sync.Mutex
	lines []string
	part  []byte
}

func (r *lineRing) add(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.push(line)
}

func (r *lineRing) push(line string) {
	line = strings.TrimRight(line, "\r")
	if len(line) > stderrBytes {
		line = line[len(line)-stderrBytes:]
	}
	r.lines = append(r.lines, line)
	if len(r.lines) > stderrLines {
		r.lines = r.lines[len(r.lines)-stderrLines:]
	}
}

func (r *lineRing) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.part = append(r.part, p...)
	for {
		i := bytes.IndexByte(r.part, '\n')
		if i < 0 {
			break
		}
		r.push(string(r.part[:i]))
		r.part = r.part[i+1:]
	}
	if len(r.part) > stderrBytes {
		r.part = r.part[len(r.part)-stderrBytes:]
	}
	return len(p), nil
}

// String is the lines kept, oldest first, joined with newlines.
func (r *lineRing) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := strings.Join(r.lines, "\n")
	if len(s) > stderrBytes {
		s = strings.ToValidUTF8(s[len(s)-stderrBytes:], "")
	}
	return s
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
