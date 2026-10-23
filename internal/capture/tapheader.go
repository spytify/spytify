package capture

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/spytify/spytify/internal/pcm"
)

// readTapHeader parses spytify-tap's first line:
// "SPYTIFY-TAP f32le <rate> <channels>".
func readTapHeader(r *bufio.Reader) (pcm.Format, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return pcm.Format{}, err
	}
	var sample string
	var f pcm.Format
	if n, _ := fmt.Sscanf(strings.TrimSpace(line), "SPYTIFY-TAP %s %d %d", &sample, &f.Rate, &f.Channels); n != 3 {
		return pcm.Format{}, fmt.Errorf("unexpected spytify-tap header %q", strings.TrimSpace(line))
	}
	switch sample {
	case "f32le":
		f.Sample = pcm.F32LE
	case "s16le":
		f.Sample = pcm.S16LE
	default:
		return pcm.Format{}, fmt.Errorf("unexpected spytify-tap sample format %q", sample)
	}
	if !f.Valid() {
		return pcm.Format{}, fmt.Errorf("unexpected spytify-tap format %q", strings.TrimSpace(line))
	}
	return f, nil
}
