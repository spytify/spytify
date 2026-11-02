// Package testkit holds what the tests of several packages share: finding
// ffmpeg, writing and decoding audio files, reading an event log. Only
// _test.go files import it.
package testkit

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/spytify/spytify/internal/pcm"
	"github.com/spytify/spytify/pkg/proto"
)

// Need is what find finds for name, as encode.Find does for ffmpeg or
// exec.LookPath for ffprobe; a test that finds nothing is skipped. It takes
// the finder because encode's own tests cannot import a package that
// imports encode.
func Need[T any](t testing.TB, name string, find func(string) (T, error)) T {
	t.Helper()
	v, err := find(name)
	if err != nil {
		t.Skip(err)
	}
	return v
}

// WriteWAV writes data, in format f, to a WAV file at path, and returns
// path.
func WriteWAV(t testing.TB, path string, f pcm.Format, data []byte) string {
	t.Helper()
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

// Decode is the file at path decoded by ffmpeg to samples of type sample,
// at the file's own rate and channels.
func Decode(t testing.TB, ffmpeg, path string, sample pcm.SampleType) (pcm.Format, []byte) {
	t.Helper()
	codec := "pcm_s16le"
	if sample == pcm.F32LE {
		codec = "pcm_f32le"
	}
	var stderr bytes.Buffer
	cmd := exec.Command(ffmpeg, "-nostdin", "-loglevel", "error", "-i", path, "-c:a", codec, "-f", "wav", "-")
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("decode %s: %v %s", path, err, stderr.Bytes())
	}
	f, data, err := pcm.DecodeWAV(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	return f, data
}

// Loudness is the integrated loudness of the file at path in LUFS, from
// the summary of ffmpeg's EBU R128 meter.
func Loudness(t testing.TB, ffmpeg, path string) float64 {
	t.Helper()
	out, err := exec.Command(ffmpeg, "-nostdin", "-hide_banner", "-nostats", "-i", path, "-af", "ebur128", "-f", "null", "-").CombinedOutput()
	if err != nil {
		t.Fatalf("ebur128: %v: %s", err, tail(out))
	}
	_, summary, ok := strings.Cut(string(out), "Summary:")
	if !ok {
		t.Fatalf("no ebur128 summary: %s", tail(out))
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

// tail is the end of ffmpeg's output: the meter writes a line every 100 ms.
func tail(out []byte) string {
	s := strings.TrimSpace(string(out))
	if len(s) > 600 {
		return "..." + s[len(s)-600:]
	}
	return s
}

// ReadEvents is the event log spytifyd --events writes, one JSON event a
// line.
func ReadEvents(t testing.TB, path string) []proto.Event {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []proto.Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var ev proto.Event
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			t.Fatal(err)
		}
		out = append(out, ev)
	}
	return out
}
