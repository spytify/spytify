package encode

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/spytify/spytify/internal/pcm"
	"github.com/spytify/spytify/internal/tag"
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

func TestNormalizeAndMP3(t *testing.T) {
	ff := ffmpegOrSkip(t)
	if !ff.HasEncoder("libmp3lame") {
		t.Skip("ffmpeg has no libmp3lame")
	}
	dir := t.TempDir()
	f := pcm.Format{Rate: 48000, Channels: 2, Sample: pcm.F32LE}
	in := writeWAV(t, dir, f, pcm.Synth{Seed: 4, LevelDB: -30}.Render(f, 3*48000))
	out := filepath.Join(dir, "out.mp3.part")
	gain, err := ff.Encode(context.Background(), Job{In: in, Out: out, Format: "mp3", BitrateKbps: 160, Source: f, Normalize: true, TargetLUFS: -14})
	if err != nil {
		t.Fatal(err)
	}
	if gain != nil {
		t.Errorf("mp3 got a ReplayGain %+v", *gain)
	}
	decoded := decode(t, ff, out, f)
	_, rmsIn := f.Level(pcm.Synth{Seed: 4, LevelDB: -30}.Render(f, 3*48000))
	_, rmsOut := f.Level(decoded)
	if rmsOut < rmsIn+6 {
		t.Fatalf("normalisation did not raise a quiet track: in %.1f dBFS, out %.1f dBFS", rmsIn, rmsOut)
	}
}

// With Normalize on, FLAC and WAV hold the samples captured, as with it
// off; FLAC carries the gain to the target in its ReplayGain tags instead.
func TestNormalizeKeepsLosslessSamples(t *testing.T) {
	ff := ffmpegOrSkip(t)
	ctx := context.Background()
	for _, f := range []pcm.Format{{Rate: 44100, Channels: 2, Sample: pcm.S16LE}, {Rate: 48000, Channels: 2, Sample: pcm.F32LE}} {
		dir := t.TempDir()
		in := writeWAV(t, dir, f, pcm.Synth{Seed: 4, LevelDB: -30}.Render(f, 3*int64(f.Rate)))
		for _, format := range []string{"flac", "wav"} {
			job := Job{In: in, Format: format, Source: f, TrimStart: 1000, TrimEnd: 2 * int64(f.Rate), TargetLUFS: -14}
			job.Out = filepath.Join(dir, "off."+format+".part")
			if _, err := ff.Encode(ctx, job); err != nil {
				t.Fatal(err)
			}
			off := job.Out
			job.Normalize, job.Out = true, filepath.Join(dir, "on."+format+".part")
			gain, err := ff.Encode(ctx, job)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(decode(t, ff, job.Out, f), decode(t, ff, off, f)) {
				t.Errorf("%s %v: normalize changed the samples", format, f.Sample)
			}
			if format == "wav" {
				if gain != nil {
					t.Errorf("wav %v got a ReplayGain %+v", f.Sample, *gain)
				}
				continue
			}

			ln, err := ff.measure(ctx, job, []string{job.trimFilter()})
			if err != nil || ln == nil {
				t.Fatalf("measure: %v %v", ln, err)
			}
			i, _ := strconv.ParseFloat(ln.InputI, 64)
			tp, _ := strconv.ParseFloat(ln.InputTP, 64)
			if gain == nil || gain.DB != -14-i || gain.Peak != math.Pow(10, tp/20) {
				t.Fatalf("flac %v: gain %v, measured %s LUFS, %s dBTP", f.Sample, gain, ln.InputI, ln.InputTP)
			}
			if gain.DB < 6 {
				t.Errorf("a track at -30 dBFS gets %.2f dB to reach -14 LUFS", gain.DB)
			}
			track := tag.Track{Title: "Song", ReplayGain: &tag.ReplayGain{Gain: gain.DB, Peak: gain.Peak}}
			if err := (tag.Writer{FFmpeg: ff.Path}).Write(ctx, job.Out, "flac", track, tag.Options{}); err != nil {
				t.Fatal(err)
			}
			meta, err := exec.Command(ff.Path, "-nostdin", "-loglevel", "error", "-i", job.Out, "-f", "ffmetadata", "-").Output()
			if err != nil {
				t.Fatal(err)
			}
			tags := strings.ToUpper(string(meta))
			for _, want := range []string{fmt.Sprintf("REPLAYGAIN_TRACK_GAIN=%.2f DB\n", gain.DB), fmt.Sprintf("REPLAYGAIN_TRACK_PEAK=%.6f\n", gain.Peak)} {
				if !strings.Contains(tags, want) {
					t.Errorf("flac %v: %q missing from\n%s", f.Sample, want, meta)
				}
			}
			if !bytes.Equal(decode(t, ff, job.Out, f), decode(t, ff, off, f)) {
				t.Errorf("flac %v: tagging changed the samples", f.Sample)
			}
		}
	}
}

func TestMP3RatesStayInWhatMP3Allows(t *testing.T) {
	for in, want := range map[int]int{22050: 22050, 44100: 44100, 48000: 48000, 88200: 44100, 96000: 48000, 176400: 44100, 192000: 48000} {
		if got := mp3Rate(in); got != want {
			t.Errorf("mp3Rate(%d) = %d, want %d", in, got, want)
		}
	}
}
