package encode

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
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

func TestNormalizeAndMP3(t *testing.T) {
	ff := ffmpegOrSkip(t)
	if !ff.HasEncoder("libmp3lame") {
		t.Skip("ffmpeg has no libmp3lame")
	}
	dir := t.TempDir()
	f := pcm.Format{Rate: 48000, Channels: 2, Sample: pcm.F32LE}
	in := writeWAV(t, dir, f, pcm.Synth{Seed: 4, LevelDB: -30}.Render(f, 3*48000))
	out := filepath.Join(dir, "out.mp3.part")
	if err := ff.Encode(context.Background(), Job{In: in, Out: out, Format: "mp3", BitrateKbps: 160, Source: f, Normalize: true, TargetLUFS: -14}); err != nil {
		t.Fatal(err)
	}
	decoded := decode(t, ff, out, f)
	_, rmsIn := f.Level(pcm.Synth{Seed: 4, LevelDB: -30}.Render(f, 3*48000))
	_, rmsOut := f.Level(decoded)
	if rmsOut < rmsIn+6 {
		t.Fatalf("normalisation did not raise a quiet track: in %.1f dBFS, out %.1f dBFS", rmsIn, rmsOut)
	}
}
