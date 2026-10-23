// Package capture records the PCM a single process sends to the OS mixer.
package capture

import (
	"context"
	"errors"
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
