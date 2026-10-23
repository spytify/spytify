//go:build darwin && macos_capture

// Package macos records a process through spytify-tap and the Core Audio
// capture backend, and checks the frames against what the process played.
// It needs the System Audio Recording permission for the terminal running
// it; macOS asks the first time.
//
//	go test -tags macos_capture -v ./test/macos/
package macos

import (
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/spytify/spytify/internal/capture"
	"github.com/spytify/spytify/internal/pcm"
	"github.com/spytify/spytify/internal/target"
)

func buildHelper(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "spytify-tap")
	src := filepath.Join("..", "..", "macos", "spytify-tap")
	cmd := exec.Command("swiftc", "-O", "-target", "arm64-apple-macos14.2",
		"-Xlinker", "-sectcreate", "-Xlinker", "__TEXT", "-Xlinker", "__info_plist", "-Xlinker", filepath.Join(src, "Info.plist"),
		filepath.Join(src, "main.swift"), "-o", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cannot build spytify-tap: %v\n%s", err, b)
	}
	return out
}

// signal is three tones with gaps, like three short tracks.
func signal(f pcm.Format) []byte {
	var out []byte
	for i, ms := range []int64{1500, 1200, 1800} {
		out = append(out, make([]byte, f.FramesFromMs(400)*int64(f.FrameBytes()))...)
		out = append(out, pcm.Synth{Seed: int64(i + 1), FadeInMs: 20, FadeOut: 20}.Render(f, f.FramesFromMs(ms))...)
	}
	return append(out, make([]byte, f.FramesFromMs(400)*int64(f.FrameBytes()))...)
}

func TestTapRecordsAProcess(t *testing.T) {
	if !capture.TapSupported(capture.MacOSVersion()) {
		t.Skip("macOS older than 14.2")
	}
	helper := buildHelper(t)
	src := pcm.Format{Rate: 44100, Channels: 2, Sample: pcm.S16LE}
	data := signal(src)
	wav := filepath.Join(t.TempDir(), "signal.wav")
	w, err := pcm.CreateWAV(wav, src)
	if err != nil {
		t.Fatal(err)
	}
	w.Write(data)
	w.Close()

	c, err := capture.NewDarwin(helper, true, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	// Other software on a Mac plays sounds with afplay too; the test
	// targets its own child through the process ancestry instead.
	self, _ := os.Executable()
	if _, err := c.Open(context.Background(), target.Target{ProcessNames: []string{filepath.Base(self)}}); err != nil {
		t.Fatal(err)
	}
	// Let the helper start waiting before the player appears.
	time.Sleep(300 * time.Millisecond)
	player := exec.Command("afplay", wav)
	if err := player.Start(); err != nil {
		t.Fatal(err)
	}
	var got []byte
	var gotF pcm.Format
	done := make(chan error, 1)
	go func() { done <- player.Wait() }()
	finished := false
	deadline := time.After(30 * time.Second)
	for !finished {
		select {
		case ch, ok := <-c.Frames():
			if !ok {
				finished = true
				break
			}
			gotF = ch.Format
			got = append(got, ch.Data...)
		case <-done:
			time.AfterFunc(500*time.Millisecond, func() { c.Close() })
			done = nil
		case <-deadline:
			t.Fatal("capture did not end")
		}
	}
	if len(got) == 0 {
		t.Fatal("no frames captured")
	}
	if gotF.Rate != src.Rate {
		t.Skipf("the output device runs at %d Hz, the signal at %d Hz; the comparison needs equal rates", gotF.Rate, src.Rate)
	}
	_, rms := gotF.Level(got)
	if rms <= pcm.Silence+1 {
		t.Fatal("captured frames are silent: grant System Audio Recording to this terminal in System Settings > Privacy & Security")
	}
	// The stereo mixdown applies a constant gain; fit it, then compare.
	gain := fitGain(src, data, gotF, got)
	scaled := scale(gotF, got, 1/gain)
	cmp := pcm.Compare(src, data, gotF, scaled, -60, 10)
	t.Logf("gain %.3f (%.1f dB), lag %.2f ms, residual %.1f dBFS, %+d frames", gain, 20*math.Log10(gain), cmp.LagMs, cmp.ErrorDBFS, cmp.LengthDelta)
	if !cmp.Matches(2, -50) || cmp.LengthDelta != 0 {
		_ = os.WriteFile(filepath.Join(os.TempDir(), "spytify-tap-capture.raw"), got, 0o644)
		t.Fatalf("captured audio does not match what afplay played")
	}
}

func fitGain(wf pcm.Format, want []byte, gf pcm.Format, got []byte) float64 {
	_, a := wf.Level(want)
	_, b := gf.Level(got)
	// Levels over the whole run, silence included on both sides.
	return math.Pow(10, (b-a)/20) * math.Sqrt(float64(len(got)/gf.FrameBytes())/float64(len(want)/wf.FrameBytes()))
}

func scale(f pcm.Format, b []byte, k float64) []byte {
	out := make([]byte, len(b))
	n := len(b) / f.Sample.Bytes()
	for i := 0; i < n; i++ {
		f.PutSample(out, i, f.SampleAt(b, i)*k)
	}
	return out
}
