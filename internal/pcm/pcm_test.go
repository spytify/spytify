package pcm

import (
	"bytes"
	"math"
	"path/filepath"
	"testing"
)

func TestWAVRoundTrip(t *testing.T) {
	for _, f := range []Format{{44100, 2, S16LE}, {48000, 1, F32LE}} {
		data := Synth{Seed: 3}.Render(f, 4410)
		path := filepath.Join(t.TempDir(), "x.wav")
		w, err := CreateWAV(path, f)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data[:len(data)/2]); err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data[len(data)/2:]); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		gotF, got, err := ReadWAV(path)
		if err != nil {
			t.Fatal(err)
		}
		if gotF != f || !bytes.Equal(got, data) {
			t.Fatalf("%v: round trip mismatch: %v, %d bytes vs %d", f, gotF, len(got), len(data))
		}
		wf, err := OpenWAV(path)
		if err != nil {
			t.Fatal(err)
		}
		fb := int64(f.FrameBytes())
		if wf.Format() != f || wf.Frames() != 4410 {
			t.Fatalf("%v: OpenWAV sees %v, %d frames", f, wf.Format(), wf.Frames())
		}
		for _, r := range [][2]int64{{0, 10}, {100, 3000}, {4400, 5000}, {-5, 3}} {
			part, err := wf.ReadFrames(r[0], r[1])
			if err != nil {
				t.Fatal(err)
			}
			from, to := max(r[0], 0), min(r[1], 4410)
			if !bytes.Equal(part, data[from*fb:to*fb]) {
				t.Errorf("%v: ReadFrames%v differs", f, r)
			}
		}
		wf.Close()
	}
}

func TestWriteRejectsPartialFrames(t *testing.T) {
	w, err := CreateWAV(filepath.Join(t.TempDir(), "x.wav"), Format{44100, 2, S16LE})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if _, err := w.Write(make([]byte, 3)); err == nil {
		t.Fatal("partial frame accepted")
	}
}

func TestLevel(t *testing.T) {
	f := Format{44100, 1, F32LE}
	b := make([]byte, 4*100)
	for i := 0; i < 100; i++ {
		f.PutSample(b, i, 0.5)
	}
	peak, rms := f.Level(b)
	if math.Abs(peak-(-6.0206)) > 0.01 || math.Abs(rms-peak) > 0.01 {
		t.Fatalf("peak=%v rms=%v", peak, rms)
	}
	if p, _ := f.Level(make([]byte, 400)); p != Silence {
		t.Fatalf("silence peak = %v", p)
	}
}

func TestCompareDetectsBleed(t *testing.T) {
	f := Format{44100, 2, S16LE}
	a := Synth{Seed: 1, FadeInMs: 20, FadeOut: 20}.Render(f, 44100)
	b := Synth{Seed: 2, FadeInMs: 20, FadeOut: 20}.Render(f, 44100)

	if c := Compare(f, a, f, a, -60, 10); !c.Matches(2, -50) || c.LagFrames != 0 {
		t.Fatalf("identical signals: %+v", c)
	}

	shifted := append(make([]byte, 50*f.FrameBytes()), a...)
	if c := Compare(f, a, f, shifted, -60, 10); c.LagFrames != 0 {
		// Leading silence is trimmed, so a pure offset is not a lag.
		t.Fatalf("offset by silence: %+v", c)
	}

	bleed := append(append([]byte{}, a...), b[:4410*f.FrameBytes()]...)
	if c := Compare(f, a, f, bleed, -60, 10); c.Matches(2, -50) {
		t.Fatalf("100 ms of the next track went unnoticed: %+v", c)
	}

	cut := a[:len(a)-2205*f.FrameBytes()]
	if c := Compare(f, a, f, cut, -60, 10); c.Matches(2, -50) {
		t.Fatalf("missing 50 ms went unnoticed: %+v", c)
	}
}

func TestFramesMs(t *testing.T) {
	f := Format{Rate: 48000, Channels: 2}
	if f.FramesFromMs(300) != 14400 || f.MsFromFrames(14400) != 300 {
		t.Fatal("conversion")
	}
}
