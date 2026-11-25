package encode

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spytify/spytify/internal/pcm"
)

// TestMain lets the test binary stand in for ffmpeg: started with
// SPYTIFY_FAKE_FFMPEG, it hangs without a word, or moves slowly through
// the audio, reporting its position as ffmpeg's -progress does.
func TestMain(m *testing.M) {
	switch os.Getenv("SPYTIFY_FAKE_FFMPEG") {
	case "hang":
		time.Sleep(time.Minute)
		os.Exit(0)
	case "slow":
		for i := 1; i <= 35; i++ {
			fmt.Printf("out_time_us=%d\nprogress=continue\n", i*500000)
			time.Sleep(200 * time.Millisecond)
		}
		fmt.Print("progress=end\n")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func fakeEncode(t *testing.T, mode string) (time.Duration, error) {
	t.Helper()
	t.Setenv("SPYTIFY_FAKE_FFMPEG", mode)
	// Wide enough for a race-built test binary to start under load.
	old := stallAfter
	stallAfter = 3 * time.Second
	t.Cleanup(func() { stallAfter = old })
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	j := Job{In: filepath.Join(dir, "in.wav"), Out: filepath.Join(dir, "out.mp3.part"), Format: "mp3", BitrateKbps: 320, Source: pcm.Format{Rate: 48000, Channels: 2, Sample: pcm.F32LE}}
	start := time.Now()
	_, err = (&FFmpeg{Path: exe}).Encode(context.Background(), j)
	return time.Since(start), err
}

// An ffmpeg that stops moving through the audio is stopped, and says so.
func TestAStalledFFmpegIsStopped(t *testing.T) {
	took, err := fakeEncode(t, "hang")
	if !errors.Is(err, ErrStalled) {
		t.Fatalf("err = %v, want ErrStalled", err)
	}
	if took > 20*time.Second {
		t.Errorf("stopped after %v", took)
	}
}

// An ffmpeg that moves, however slowly, is never stopped: a long track on
// a slow computer outlasts any fixed deadline (#956).
func TestASlowFFmpegThatMovesIsNotStopped(t *testing.T) {
	took, err := fakeEncode(t, "slow")
	if err != nil {
		t.Fatal(err)
	}
	if took < 2*stallAfter {
		t.Fatalf("the fake ran %v, not longer than the stall window", took)
	}
}
