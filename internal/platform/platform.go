// Package platform picks the capture and metadata backends of the running
// OS, or the fake player for tests and demos.
package platform

import (
	"github.com/spytify/spytify/internal/capture"
	"github.com/spytify/spytify/internal/fake"
	"github.com/spytify/spytify/internal/meta"
	"github.com/spytify/spytify/internal/target"
	"github.com/spytify/spytify/pkg/proto"
)

// Backends are the OS backends; Logf receives their diagnostics.
type Backends struct {
	Logf func(format string, args ...any)
	// TapHelper is the macOS capture helper path ("" = next to spytifyd).
	TapHelper string
	// StateDir is spytifyd's; the macOS capture keeps there the device
	// rates it sets, to put them back.
	StateDir string
	// MuteOutput reads player.mute_output when a session starts.
	MuteOutput func() bool
}

func (b Backends) mute() bool { return b.MuteOutput != nil && b.MuteOutput() }

func (b Backends) logf(format string, args ...any) {
	if b.Logf != nil {
		b.Logf(format, args...)
	}
}

// Fake drives a session from a timeline file instead of a real player.
type Fake struct {
	Timeline *fake.Timeline
	Realtime bool
}

func (f Fake) New(target.Target) (capture.Capture, meta.NowPlaying, error) {
	p, err := fake.NewPlayer(f.Timeline, f.Realtime)
	if err != nil {
		return nil, nil, err
	}
	b := fake.NewBackend(p)
	return b, b, nil
}

func (f Fake) Probe() []proto.Check {
	return []proto.Check{{Name: "fake player", OK: true, Detail: f.Timeline.Name}}
}
