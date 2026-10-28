package encode

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"math"
	"math/cmplx"
	"math/rand/v2"
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

// Up to 48 kHz, the file keeps the source's rate; above, it is written at
// the rate the source's divides into. In every format, normalized or not.
func TestAFileAbove48kHzIsWrittenAtHalfOrAQuarterOfTheRate(t *testing.T) {
	ff := ffmpegOrSkip(t)
	for _, format := range slices.Sorted(maps.Keys(presets)) {
		p := presets[format]
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			if p.Encoder != "" && !ff.HasEncoder(p.Encoder) {
				t.Skipf("ffmpeg has no %s", p.Encoder)
			}
			for _, c := range []struct{ source, want int }{{44100, 44100}, {48000, 48000}, {88200, 44100}, {96000, 48000}, {176400, 44100}, {192000, 48000}} {
				f := pcm.Format{Rate: c.source, Channels: 2, Sample: pcm.F32LE}
				in := writeWAV(t, t.TempDir(), f, pcm.Synth{Seed: 2}.Render(f, int64(c.source/2)))
				for _, normalize := range []bool{false, true} {
					out := filepath.Join(t.TempDir(), "out."+p.Extension+".part")
					j := Job{In: in, Out: out, Format: format, BitrateKbps: 320, Source: f, Normalize: normalize, TargetLUFS: -14}
					if err := ff.Encode(context.Background(), j); err != nil {
						t.Fatal(err)
					}
					if got := rateOf(t, ff, out); got != c.want {
						t.Errorf("source at %d Hz, normalize %v: file at %d Hz, want %d", c.source, normalize, got, c.want)
					}
				}
			}
		})
	}
}

// fft transforms x in place; its length is a power of two. The inverse is
// left unscaled.
func fft(x []complex128, inverse bool) {
	n := len(x)
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			x[i], x[j] = x[j], x[i]
		}
	}
	sign := -1.0
	if inverse {
		sign = 1
	}
	for size := 2; size <= n; size <<= 1 {
		step := cmplx.Rect(1, sign*2*math.Pi/float64(size))
		for start := 0; start < n; start += size {
			w := complex(1, 0)
			for k := start; k < start+size/2; k++ {
				u, v := x[k], x[k+size/2]*w
				x[k], x[k+size/2] = u+v, u-v
				w *= step
			}
		}
	}
}

// bandNoise is frames of interleaved stereo noise at rate, flat up to
// 20 kHz and empty above, at -20 dBFS RMS. frames is a power of two.
func bandNoise(rate, frames int) []float64 {
	rng := rand.New(rand.NewPCG(1, 2))
	// Left in the real part and right in the imaginary one: zeroing the
	// same bins on both sides of the spectrum filters each alike.
	x := make([]complex128, frames)
	for i := range x {
		x[i] = complex(rng.NormFloat64(), rng.NormFloat64())
	}
	fft(x, false)
	for k := range x {
		if float64(min(k, frames-k))*float64(rate)/float64(frames) > 20000 {
			x[k] = 0
		}
	}
	fft(x, true)
	var sum float64
	for _, v := range x {
		sum += real(v)*real(v) + imag(v)*imag(v)
	}
	gain := 0.1 / math.Sqrt(sum/float64(2*frames))
	out := make([]float64, 0, 2*frames)
	for _, v := range x {
		out = append(out, real(v)*gain, imag(v)*gain)
	}
	return out
}

// decodeAt is path's samples, interleaved, brought to rate by resample.
func decodeAt(t *testing.T, ff *FFmpeg, path string, rate int) []float64 {
	t.Helper()
	out, err := exec.Command(ff.Path, "-nostdin", "-loglevel", "error", "-i", path, "-af", resample(rate), "-c:a", "pcm_f32le", "-f", "wav", "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	f, data, err := pcm.DecodeWAV(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	x := make([]float64, len(data)/4)
	for i := range x {
		x[i] = f.SampleAt(data, i)
	}
	return x
}

// A 96 kHz source written at 48 kHz loses nothing up to 20 kHz: brought
// back to 96 kHz, the file differs from the source by 100 dB under it, or
// more. With ffmpeg's default resampler, it is only about 67 dB.
func TestA96kHzSourceWrittenAt48kHzKeepsEverythingUpTo20kHz(t *testing.T) {
	ff := ffmpegOrSkip(t)
	f := pcm.Format{Rate: 96000, Channels: 2, Sample: pcm.F32LE}
	want := bandNoise(f.Rate, 1<<19)
	// The fade-in gives the track a loudness range: without one, loudnorm
	// leaves its linear mode for its dynamic one.
	fade := int(f.FramesFromMs(1500))
	for i := range 2 * fade {
		want[i] *= float64(i/2) / float64(fade)
	}
	src := make([]byte, len(want)*4)
	for i, v := range want {
		f.PutSample(src, i, v)
	}
	in := writeWAV(t, t.TempDir(), f, src)
	for _, normalize := range []bool{false, true} {
		t.Run(map[bool]string{false: "as recorded", true: "normalized"}[normalize], func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out.flac.part")
			if err := ff.Encode(context.Background(), Job{In: in, Out: out, Format: "flac", Source: f, Normalize: normalize, TargetLUFS: -20}); err != nil {
				t.Fatal(err)
			}
			if rate := rateOf(t, ff, out); rate != 48000 {
				t.Fatalf("file at %d Hz, want 48000", rate)
			}
			got := decodeAt(t, ff, out, f.Rate)
			// The resampler rings at the edges: 2% of each end is left out.
			n := min(len(want), len(got))
			a, b := want[n/50:n-n/50], got[n/50:n-n/50]
			// Normalized, the file is the source at another gain.
			gain := 1.0
			if normalize {
				var ab, bb float64
				for i := range a {
					ab += a[i] * b[i]
					bb += b[i] * b[i]
				}
				gain = ab / bb
			}
			var diff, signal float64
			for i := range a {
				d := a[i] - gain*b[i]
				diff += d * d
				signal += a[i] * a[i]
			}
			db := 10 * math.Log10(diff/signal)
			t.Logf("difference %.1f dB under the source, gain %+.1f dB", -db, -20*math.Log10(gain))
			if db > -100 {
				t.Errorf("difference only %.1f dB under the source, want 100 or more", -db)
			}
		})
	}
}
