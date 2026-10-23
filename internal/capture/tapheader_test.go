package capture

import (
	"bufio"
	"strings"
	"testing"

	"github.com/spytify/spytify/internal/pcm"
)

func TestReadTapHeader(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("SPYTIFY-TAP f32le 44100 2\n\x00\x00\x80\x3f"))
	f, err := readTapHeader(r)
	if err != nil || f != (pcm.Format{Rate: 44100, Channels: 2, Sample: pcm.F32LE}) {
		t.Fatalf("format %v err %v", f, err)
	}
	if b, _ := r.Peek(4); len(b) != 4 {
		t.Fatal("header read consumed frames")
	}
	for _, bad := range []string{"RIFF....\n", "SPYTIFY-TAP f64le 48000 2\n", "SPYTIFY-TAP f32le 0 2\n", ""} {
		if _, err := readTapHeader(bufio.NewReader(strings.NewReader(bad))); err == nil {
			t.Errorf("header %q accepted", bad)
		}
	}
}
