package encode

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"math"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/spytify/spytify/internal/pcm"
)

func ffmpegOrSkip(t *testing.T) *FFmpeg {
	t.Helper()
	f, err := Find("ffmpeg")
	if err != nil {
		t.Skip(err)
	}
	return f
}

func writeWAV(t *testing.T, dir string, f pcm.Format, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, "in.wav")
	w, err := pcm.CreateWAV(path, f)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func decode(t *testing.T, ff *FFmpeg, path string, f pcm.Format) []byte {
	t.Helper()
	codec := "pcm_s16le"
	if f.Sample == pcm.F32LE {
		codec = "pcm_f32le"
	}
	out, err := exec.Command(ff.Path, "-nostdin", "-loglevel", "error", "-i", path, "-c:a", codec, "-f", "wav", "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	_, data, err := pcm.DecodeWAV(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestFLACIsLosslessAndTrimIsSampleAccurate(t *testing.T) {
	ff := ffmpegOrSkip(t)
	dir := t.TempDir()
	f := pcm.Format{Rate: 44100, Channels: 2, Sample: pcm.S16LE}
	src := pcm.Synth{Seed: 9}.Render(f, 44100)
	in := writeWAV(t, dir, f, src)

	out := filepath.Join(dir, "out.flac.part")
	if err := ff.Encode(context.Background(), Job{In: in, Out: out, Format: "flac", Source: f}); err != nil {
		t.Fatal(err)
	}
	if got := decode(t, ff, out, f); !bytes.Equal(got, src) {
		t.Fatalf("flac round trip differs: %d bytes vs %d", len(got), len(src))
	}

	trimmed := filepath.Join(dir, "trim.wav.part")
	if err := ff.Encode(context.Background(), Job{In: in, Out: trimmed, Format: "wav", Source: f, TrimStart: 1000, TrimEnd: 30000}); err != nil {
		t.Fatal(err)
	}
	fb := f.FrameBytes()
	if got := decode(t, ff, trimmed, f); !bytes.Equal(got, src[1000*fb:30000*fb]) {
		t.Fatalf("trim is not sample accurate: %d bytes, want %d", len(got), (30000-1000)*fb)
	}
}

// rateOf is the sample rate path decodes at.
func rateOf(t *testing.T, ff *FFmpeg, path string) int {
	t.Helper()
	out, err := exec.Command(ff.Path, "-nostdin", "-loglevel", "error", "-i", path, "-t", "0.1", "-f", "wav", "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	f, _, err := pcm.DecodeWAV(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	return f.Rate
}

// loudnessOf is path's integrated loudness in LUFS, from the summary of
// ffmpeg's EBU R128 meter.
func loudnessOf(t *testing.T, ff *FFmpeg, path string) float64 {
	t.Helper()
	out, err := exec.Command(ff.Path, "-nostdin", "-hide_banner", "-nostats", "-i", path, "-af", "ebur128", "-f", "null", "-").CombinedOutput()
	if err != nil {
		t.Fatalf("ebur128: %v: %s", err, tail(string(out)))
	}
	_, summary, ok := strings.Cut(string(out), "Summary:")
	if !ok {
		t.Fatalf("no ebur128 summary: %s", tail(string(out)))
	}
	for _, line := range strings.Split(summary, "\n") {
		if f := strings.Fields(line); len(f) == 3 && f[0] == "I:" && f[2] == "LUFS" {
			v, err := strconv.ParseFloat(f[1], 64)
			if err != nil {
				t.Fatal(err)
			}
			return v
		}
	}
	t.Fatalf("no integrated loudness in the ebur128 summary: %s", summary)
	return 0
}

// Normalize off keeps a track as loud as it was recorded, in every format;
// on brings it to the target, up to -14 or down to -20 from the source's
// -17.5 LUFS.
func TestNormalizeOffKeepsTheLoudnessAndOnReachesTheTarget(t *testing.T) {
	ff := ffmpegOrSkip(t)
	f := pcm.Format{Rate: 48000, Channels: 2, Sample: pcm.F32LE}
	// The fade-in gives the track a loudness range: without one, loudnorm
	// leaves its linear mode for its dynamic one.
	in := writeWAV(t, t.TempDir(), f, pcm.Synth{Seed: 4, LevelDB: -10, FadeInMs: 1500}.Render(f, 5*48000))
	source := loudnessOf(t, ff, in)
	t.Logf("source %.1f LUFS", source)
	for _, format := range slices.Sorted(maps.Keys(presets)) {
		p := presets[format]
		t.Run(format, func(t *testing.T) {
			if p.Encoder != "" && !ff.HasEncoder(p.Encoder) {
				t.Skipf("ffmpeg has no %s", p.Encoder)
			}
			encode := func(t *testing.T, normalize bool, target float64) float64 {
				t.Helper()
				out := filepath.Join(t.TempDir(), "out."+p.Extension+".part")
				// 320 kbps: at 160, LAME by itself makes a track about
				// 0.5 dB quieter.
				j := Job{In: in, Out: out, Format: format, BitrateKbps: 320, Source: f, Normalize: normalize, TargetLUFS: target}
				if err := ff.Encode(context.Background(), j); err != nil {
					t.Fatal(err)
				}
				if rate := rateOf(t, ff, out); rate != f.Rate {
					t.Errorf("sample rate %d, want the source's %d", rate, f.Rate)
				}
				lufs := loudnessOf(t, ff, out)
				t.Logf("%.1f LUFS", lufs)
				return lufs
			}
			t.Run("off", func(t *testing.T) {
				if got := encode(t, false, -14); math.Abs(got-source) > 0.5 {
					t.Errorf("%.1f LUFS, want the source's %.1f within 0.5 LU", got, source)
				}
			})
			for _, target := range []float64{-14, -20} {
				t.Run(fmt.Sprintf("on %g LUFS", target), func(t *testing.T) {
					if got := encode(t, true, target); math.Abs(got-target) > 1 {
						t.Errorf("%.1f LUFS, want %g within 1 LU", got, target)
					}
				})
			}
		})
	}
}

func TestMP3RatesStayInWhatMP3Allows(t *testing.T) {
	for in, want := range map[int]int{22050: 22050, 44100: 44100, 48000: 48000, 88200: 44100, 96000: 48000, 176400: 44100, 192000: 48000} {
		if got := mp3Rate(in); got != want {
			t.Errorf("mp3Rate(%d) = %d, want %d", in, got, want)
		}
	}
}
