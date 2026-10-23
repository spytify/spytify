package core

import (
	"math"
	"path/filepath"
	"testing"

	"github.com/spytify/spytify/internal/pcm"
)

// scanTrimWhole is scanTrim as it was, reading the whole file: the
// windowed scan must find the same frames.
func scanTrimWhole(path string, db float64) (start, end int64, ok bool, err error) {
	format, data, err := pcm.ReadWAV(path)
	if err != nil {
		return 0, 0, false, err
	}
	block := format.FramesFromMs(10)
	fb := int64(format.FrameBytes())
	total := int64(len(data)) / fb
	thr := math.Pow(10, db/20)
	loudFrame := func(i int64) bool {
		for c := 0; c < format.Channels; c++ {
			if math.Abs(format.SampleAt(data, int(i)*format.Channels+c)) > thr {
				return true
			}
		}
		return false
	}
	loudSpan := func(from, to int64) bool {
		from, to = max(from, 0), min(to, total)
		_, rms := format.Level(data[from*fb : to*fb])
		return rms > db
	}
	start = -1
	for i := int64(0); i < total; i++ {
		if loudFrame(i) && loudSpan(i, i+block) {
			start = i
			break
		}
	}
	if start < 0 {
		return 0, 0, false, nil
	}
	end = start + 1
	for j := total - 1; j >= start; j-- {
		if loudFrame(j) && loudSpan(j+1-block, j+1) {
			end = j + 1
			break
		}
	}
	floor := thr / 4
	quietRun := format.FramesFromMs(1)
	maxExtend := format.FramesFromMs(100)
	above := func(i int64) bool {
		for c := 0; c < format.Channels; c++ {
			if math.Abs(format.SampleAt(data, int(i)*format.Channels+c)) > floor {
				return true
			}
		}
		return false
	}
	for i, quiet := start-1, int64(0); i >= 0 && start-i <= maxExtend && quiet < quietRun; i-- {
		if above(i) {
			start, quiet = i, 0
		} else {
			quiet++
		}
	}
	for i, quiet := end, int64(0); i < total && i-end <= maxExtend && quiet < quietRun; i++ {
		if above(i) {
			end, quiet = i+1, 0
		} else {
			quiet++
		}
	}
	return start, end, true, nil
}

func TestScanTrimReadsWindowsAndFindsTheSameFrames(t *testing.T) {
	cases := []struct {
		name              string
		f                 pcm.Format
		lead, sound, tail int64 // ms
	}{
		{"short", pcm.Format{Rate: 44100, Channels: 2, Sample: pcm.S16LE}, 200, 300, 0},
		{"across windows", pcm.Format{Rate: 48000, Channels: 2, Sample: pcm.F32LE}, 3990, 5000, 3995},
		{"sound at the window edge", pcm.Format{Rate: 44100, Channels: 1, Sample: pcm.S16LE}, 4000, 20, 8000},
		{"silent", pcm.Format{Rate: 44100, Channels: 2, Sample: pcm.S16LE}, 6000, 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "take.wav")
			w, err := pcm.CreateWAV(path, c.f)
			if err != nil {
				t.Fatal(err)
			}
			fb := c.f.FrameBytes()
			write := func(b []byte) {
				if _, err := w.Write(b); err != nil {
					t.Fatal(err)
				}
			}
			write(make([]byte, c.f.FramesFromMs(c.lead)*int64(fb)))
			if c.sound > 0 {
				write(pcm.Synth{Seed: 5, FadeInMs: 40, FadeOut: 40}.Render(c.f, c.f.FramesFromMs(c.sound)))
			}
			write(make([]byte, c.f.FramesFromMs(c.tail)*int64(fb)))
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			ws, we, wok, werr := scanTrimWhole(path, -50)
			s, e, ok, err := scanTrim(path, -50)
			if err != nil || werr != nil {
				t.Fatal(err, werr)
			}
			if s != ws || e != we || ok != wok {
				t.Fatalf("windowed %d..%d %v, whole %d..%d %v", s, e, ok, ws, we, wok)
			}
			if ok != (c.sound > 0) {
				t.Fatalf("found sound: %v", ok)
			}
		})
	}
}
