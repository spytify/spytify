//go:build !linux && !darwin && !windows

package platform

import (
	"runtime"

	"github.com/spytify/spytify/internal/capture"
	"github.com/spytify/spytify/internal/meta"
	"github.com/spytify/spytify/internal/target"
	"github.com/spytify/spytify/pkg/proto"
)

// New fails: the alpha records on Linux only; macOS and Windows backends
// are milestone M5. spytifyd --fake works everywhere.
func (b Backends) New(target.Target) (capture.Capture, meta.NowPlaying, error) {
	return nil, nil, capture.ErrUnsupported
}

func (b Backends) Probe() []proto.Check {
	return []proto.Check{
		{Name: "capture", OK: false, Detail: "no " + runtime.GOOS + " capture backend in the alpha (Linux only)"},
	}
}
