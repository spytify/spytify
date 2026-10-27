package encode

import (
	"context"
	"errors"

	"github.com/spytify/spytify/internal/proc"
)

// ErrNoCover means the file carries no picture.
var ErrNoCover = errors.New("no embedded cover")

// Cover returns the picture embedded in an audio file: an ID3 APIC frame, a
// FLAC PICTURE block or an MP4 covr atom, copied out as it is stored.
func (f *FFmpeg) Cover(ctx context.Context, path string) ([]byte, error) {
	cmd := proc.CommandContext(ctx, f.Path, "-v", "error", "-nostdin", "-i", path, "-an", "-map", "0:v:0?", "-c:v", "copy", "-frames:v", "1", "-f", "image2pipe", "-")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, ErrNoCover
	}
	return out, nil
}
