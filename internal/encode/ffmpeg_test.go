package encode

import (
	"bytes"
	"context"
	"encoding/json"
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
	if _, err := ff.Encode(context.Background(), Job{In: in, Out: out, Format: "flac", Source: f}); err != nil {
		t.Fatal(err)
	}
	if got := decode(t, ff, out, f); !bytes.Equal(got, src) {
		t.Fatalf("flac round trip differs: %d bytes vs %d", len(got), len(src))
	}

	trimmed := filepath.Join(dir, "trim.wav.part")
	if _, err := ff.Encode(context.Background(), Job{In: in, Out: trimmed, Format: "wav", Source: f, TrimStart: 1000, TrimEnd: 30000}); err != nil {
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

// loudnormOf is path's integrated loudness in LUFS and true peak in dBTP,
// to two decimals, from loudnorm's meter: the one Normalize's gain comes
// from. On these short synths it reads 0.2 LU louder than ebur128.
func loudnormOf(t *testing.T, ff *FFmpeg, path string) (lufs, dbtp float64) {
	t.Helper()
	out, err := exec.Command(ff.Path, "-nostdin", "-hide_banner", "-i", path, "-af", "loudnorm=print_format=json", "-f", "null", "-").CombinedOutput()
	if err != nil {
		t.Fatalf("loudnorm: %v: %s", err, tail(string(out)))
	}
	s := string(out)
	var report struct {
		I  string `json:"input_i"`
		TP string `json:"input_tp"`
	}
	if start, end := strings.LastIndex(s, "{"), strings.LastIndex(s, "}"); start < 0 || end < start {
		t.Fatalf("no loudnorm report: %s", tail(s))
	} else if err := json.Unmarshal([]byte(s[start:end+1]), &report); err != nil {
		t.Fatal(err)
	}
	if lufs, err = strconv.ParseFloat(report.I, 64); err != nil {
		t.Fatal(err)
	}
	if dbtp, err = strconv.ParseFloat(report.TP, 64); err != nil {
		t.Fatal(err)
	}
	return lufs, dbtp
}

// spiky is a quiet synth with a 10 ms burst every half second: a track
// whose peaks are loud for its loudness. bodyDB is the synth's peak level
// and burstDB the bursts', in dBFS.
func spiky(f pcm.Format, frames int64, bodyDB, burstDB float64) []byte {
	b := pcm.Synth{Seed: 4, LevelDB: bodyDB, FadeInMs: 1500}.Render(f, frames)
	amp := math.Pow(10, burstDB/20)
	n := int(f.FramesFromMs(10))
	for start := f.Rate / 4; start+n <= int(frames); start += f.Rate / 2 {
		for i := range n {
			v := amp * (0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(n))) * math.Sin(2*math.Pi*1000*float64(i)/float64(f.Rate))
			for c := range f.Channels {
				k := (start+i)*f.Channels + c
				f.PutSample(b, k, f.SampleAt(b, k)+v)
			}
		}
	}
	return b
}

// samples are data's samples, interleaved.
func samples(f pcm.Format, data []byte) []float64 {
	x := make([]float64, len(data)/f.Sample.Bytes())
	for i := range x {
		x[i] = f.SampleAt(data, i)
	}
	return x
}

// oneGain fits the one gain, in dB, that best takes src to out, and says
// how far under out what that gain leaves unexplained is, in dB. out is
// first aligned on src, up to 32 frames either way, and 1% of each end is
// left out.
func oneGain(src, out []float64, channels int) (gainDB, restDB float64) {
	n := min(len(src), len(out))
	edge := n / 100
	sums := func(lag int) (so, ss, oo float64) {
		for i := edge; i < n-edge; i++ {
			s, o := src[i], out[i+lag*channels]
			so += s * o
			ss += s * s
			oo += o * o
		}
		return so, ss, oo
	}
	lag, best := 0, math.Inf(-1)
	for l := -32; l <= 32; l++ {
		if so, ss, oo := sums(l); so/math.Sqrt(ss*oo) > best {
			lag, best = l, so/math.Sqrt(ss*oo)
		}
	}
	so, ss, oo := sums(lag)
	g := so / ss
	var rest float64
	for i := edge; i < n-edge; i++ {
		d := out[i+lag*channels] - g*src[i]
		rest += d * d
	}
	return 20 * math.Log10(g), 10 * math.Log10(rest/oo)
}

// With Normalize on, a track gets one gain and is never compressed, even a
// quiet one whose bursts the gain to the target would take far past the
// true-peak limit. Aligned on the source, the file is the source at one
// gain, to 90 dB under the file or better. A 96 kHz source is written at
// 48 kHz, so there the file is checked against the one Normalize off
// writes: the gain and the resampling together.
func TestNormalizeAppliesOneGainAndNeverCompresses(t *testing.T) {
	ff := ffmpegOrSkip(t)
	for _, rate := range []int{48000, 96000} {
		f := pcm.Format{Rate: rate, Channels: 2, Sample: pcm.F32LE}
		src := spiky(f, int64(6*rate), -24, -6)
		in := writeWAV(t, t.TempDir(), f, src)
		for _, format := range []string{"flac", "wav"} {
			t.Run(fmt.Sprintf("%s from %d Hz", format, rate), func(t *testing.T) {
				encode := func(normalize bool) []float64 {
					out := filepath.Join(t.TempDir(), "out."+presets[format].Extension+".part")
					if _, err := ff.Encode(context.Background(), Job{In: in, Out: out, Format: format, Source: f, Normalize: normalize, TargetLUFS: -14}); err != nil {
						t.Fatal(err)
					}
					return samples(f, decode(t, ff, out, f))
				}
				want := samples(f, src)
				if fileRate(rate) != rate {
					want = encode(false)
				}
				i, tp := loudnormOf(t, ff, in)
				gain, rest := oneGain(want, encode(true), f.Channels)
				t.Logf("source %.2f LUFS, %.2f dBTP; gain %+.2f dB; what it leaves is %.1f dB under the file", i, tp, gain, -rest)
				if gain < 1 {
					t.Errorf("gain %+.2f dB, want the track raised", gain)
				}
				if rest > -90 {
					t.Errorf("what one gain leaves is only %.1f dB under the file, want 90 dB or more", -rest)
				}
			})
		}
	}
}

// Normalize's gain, in every format: a track louder than the target is
// lowered to it; a quieter one is raised to it when its true peak allows,
// else until its true peak reaches -1.5 dBTP; one whose true peak is over
// -1.5 dBTP already is not lowered. Encode reports the gain of a track its
// peaks keep under the target, and nothing for one that reaches it.
func TestNormalizeLowersToTheTargetAndRaisesAsFarAsThePeaksAllow(t *testing.T) {
	ff := ffmpegOrSkip(t)
	f := pcm.Format{Rate: 48000, Channels: 2, Sample: pcm.F32LE}
	const target, limit = -14.0, -1.5
	frames := int64(5 * f.Rate)
	for _, c := range []struct {
		name string
		data []byte
		// is says the source is what the case is about, from its loudness
		// and true peak; want is the file's; short, that the file ends
		// under the target.
		is    func(i, tp float64) bool
		want  func(i, tp float64) (float64, float64)
		short bool
	}{{
		name: "loud, lowered to the target",
		data: pcm.Synth{Seed: 4, LevelDB: -2, FadeInMs: 1500}.Render(f, frames),
		is:   func(i, tp float64) bool { return i > target+2 },
		want: func(i, tp float64) (float64, float64) { return target, tp + target - i },
	}, {
		name: "quiet with headroom, raised to the target",
		data: pcm.Synth{Seed: 4, LevelDB: -16, FadeInMs: 1500}.Render(f, frames),
		is:   func(i, tp float64) bool { return i < target-2 && tp+target-i < limit-2 },
		want: func(i, tp float64) (float64, float64) { return target, tp + target - i },
	}, {
		name:  "quiet without headroom, raised to -1.5 dBTP",
		data:  spiky(f, frames, -16, -6),
		is:    func(i, tp float64) bool { return tp < limit-2 && tp+target-i > limit+2 },
		want:  func(i, tp float64) (float64, float64) { return i + limit - tp, limit },
		short: true,
	}, {
		name:  "peak over -1.5 dBTP, not lowered",
		data:  spiky(f, frames, -16, -1),
		is:    func(i, tp float64) bool { return i < target-2 && tp > limit },
		want:  func(i, tp float64) (float64, float64) { return i, tp },
		short: true,
	}} {
		t.Run(c.name, func(t *testing.T) {
			in := writeWAV(t, t.TempDir(), f, c.data)
			i, tp := loudnormOf(t, ff, in)
			wantI, wantTP := c.want(i, tp)
			t.Logf("source %.2f LUFS, %.2f dBTP; want %.2f LUFS, %.2f dBTP", i, tp, wantI, wantTP)
			if !c.is(i, tp) {
				t.Fatal("the source is not what the case is about")
			}
			for _, format := range slices.Sorted(maps.Keys(presets)) {
				p := presets[format]
				t.Run(format, func(t *testing.T) {
					if p.Encoder != "" && !ff.HasEncoder(p.Encoder) {
						t.Skipf("ffmpeg has no %s", p.Encoder)
					}
					out := filepath.Join(t.TempDir(), "out."+p.Extension+".part")
					j := Job{In: in, Out: out, Format: format, BitrateKbps: 320, Source: f, Normalize: true, TargetLUFS: target}
					short, err := ff.Encode(context.Background(), j)
					if err != nil {
						t.Fatal(err)
					}
					switch {
					case !c.short && short != nil:
						t.Errorf("reported %+v, want nothing: the file reaches the target", *short)
					case c.short && short == nil:
						t.Errorf("reported nothing, want the gain of a file %.2f LU under the target", target-wantI)
					case c.short && (math.Abs(short.InputLUFS-i) > 0.01 || math.Abs(short.GainDB-(wantI-i)) > 0.01 || short.TargetLUFS != target):
						t.Errorf("reported %+v; want %.2f LUFS raised %+.2f dB toward %g LUFS", *short, i, wantI-i, target)
					}
					gotI, gotTP := loudnormOf(t, ff, out)
					t.Logf("%.2f LUFS, %.2f dBTP", gotI, gotTP)
					// A lossy codec moves the peaks a little.
					tolI, tolTP := 0.05, 0.05
					if p.Lossy {
						tolI, tolTP = 0.2, 0.5
					}
					if math.Abs(gotI-wantI) > tolI || math.Abs(gotTP-wantTP) > tolTP {
						t.Errorf("%.2f LUFS, %.2f dBTP; want %.2f LUFS within %g, %.2f dBTP within %g", gotI, gotTP, wantI, tolI, wantTP, tolTP)
					}
				})
			}
		})
	}
}

// The gain rule itself, for a -14 LUFS target.
func TestNormalizeGain(t *testing.T) {
	for _, c := range []struct {
		name      string
		i, tp, db float64
	}{
		{"louder: lowered to the target", -9, -0.5, -5},
		{"louder, peak over 0 dBTP: lowered to the target", -9, 1, -5},
		{"quieter with headroom: raised to the target", -20, -10, 6},
		{"quieter without headroom: raised to -1.5 dBTP", -20, -5, 3.5},
		{"quieter, peak at -1.5 dBTP: left as is", -20, -1.5, 0},
		{"quieter, peak over -1.5 dBTP: not lowered", -20, -0.5, 0},
		{"at the target: left as is", -14, -3, 0},
	} {
		if got := (loudness{I: c.i, TP: c.tp}).gain(-14); math.Abs(got-c.db) > 1e-9 {
			t.Errorf("%s: %g LUFS, %g dBTP gets %+g dB, want %+g", c.name, c.i, c.tp, got, c.db)
		}
	}
}

// Normalize off keeps a track as loud as it was recorded, in every format;
// on brings it to the target, up to -14 or down to -20 from the source's
// -17.5 LUFS.
func TestNormalizeOffKeepsTheLoudnessAndOnReachesTheTarget(t *testing.T) {
	ff := ffmpegOrSkip(t)
	f := pcm.Format{Rate: 48000, Channels: 2, Sample: pcm.F32LE}
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
				if _, err := ff.Encode(context.Background(), j); err != nil {
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
					if _, err := ff.Encode(context.Background(), j); err != nil {
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
	src := make([]byte, len(want)*4)
	for i, v := range want {
		f.PutSample(src, i, v)
	}
	in := writeWAV(t, t.TempDir(), f, src)
	for _, normalize := range []bool{false, true} {
		t.Run(map[bool]string{false: "as recorded", true: "normalized"}[normalize], func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out.flac.part")
			if _, err := ff.Encode(context.Background(), Job{In: in, Out: out, Format: "flac", Source: f, Normalize: normalize, TargetLUFS: -20}); err != nil {
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
