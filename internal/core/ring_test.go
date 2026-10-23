package core

import (
	"bytes"
	"testing"

	"github.com/spytify/spytify/internal/pcm"
)

var mono = pcm.Format{Rate: 1000, Channels: 1, Sample: pcm.S16LE}

func frames(f pcm.Format, values ...float64) []byte {
	b := make([]byte, len(values)*f.FrameBytes())
	for i, v := range values {
		for c := 0; c < f.Channels; c++ {
			f.PutSample(b, i*f.Channels+c, v)
		}
	}
	return b
}

func constant(f pcm.Format, v float64, n int) []byte {
	values := make([]float64, n)
	for i := range values {
		values[i] = v
	}
	return frames(f, values...)
}

func TestRingWrapsAndKeepsIndices(t *testing.T) {
	r := NewRing(mono, 0.01) // 10 frames
	for i := 0; i < 25; i++ {
		lost := r.Append(frames(mono, float64(i)/100))
		if i >= 10 && lost != 1 {
			t.Fatalf("append %d lost %d", i, lost)
		}
	}
	if r.Start() != 15 || r.End() != 25 {
		t.Fatalf("start=%d end=%d", r.Start(), r.End())
	}
	want := frames(mono, 0.20, 0.21, 0.22)
	if got := r.Bytes(20, 23); !bytes.Equal(got, want) {
		t.Fatalf("Bytes(20,23) = %v want %v", got, want)
	}
	if got := r.Bytes(0, 16); len(got) != mono.FrameBytes() {
		t.Fatalf("clamped read returned %d bytes", len(got))
	}
	r.Truncate(22)
	r.Discard(18)
	if r.Start() != 18 || r.End() != 22 || r.Free() != 6 {
		t.Fatalf("after truncate/discard: start=%d end=%d", r.Start(), r.End())
	}
}

func TestQuietestFindsGap(t *testing.T) {
	r := NewRing(mono, 4)
	r.Append(constant(mono, 0.5, 1000))
	r.Append(constant(mono, 0, 100)) // silent gap at [1000, 1100)
	r.Append(constant(mono, 0.5, 1000))
	cut, db := r.Quietest(700, 1400, 20, 5)
	if db > -100 || cut < 1000 || cut > 1100 {
		t.Fatalf("cut=%d db=%v", cut, db)
	}
	// Earliest of the silent windows.
	if cut != 1010 {
		t.Fatalf("cut = %d, want the first silent window centred at 1010", cut)
	}
}

func TestExtendBackKeepsFadeIn(t *testing.T) {
	r := NewRing(mono, 4)
	r.Append(constant(mono, 0, 100))
	// A fade from -61 dBFS up: 0.0009, 0.0012, ... crosses -50 dBFS late.
	values := make([]float64, 50)
	for i := range values {
		values[i] = 0.0009 + float64(i)*0.0003
	}
	r.Append(frames(mono, values...))
	at, ok := r.FirstAbove(0, -50, 5)
	if !ok || at <= 100 {
		t.Fatalf("FirstAbove = %d %v", at, ok)
	}
	if got := r.ExtendBack(at, -62, 2, 100, 0); got != 100 {
		t.Fatalf("ExtendBack = %d, want the fade start at 100", got)
	}
	if got := r.ExtendBack(at, -62, 2, 100, at-3); got != at-3 {
		t.Fatalf("ExtendBack ignored its limit: %d", got)
	}
}

func TestFirstAboveAndSilenceStart(t *testing.T) {
	r := NewRing(mono, 4)
	r.Append(constant(mono, 0, 500))
	r.Append(constant(mono, 0.3, 500))
	at, ok := r.FirstAbove(0, -50, 5)
	if !ok || at != 500 {
		t.Fatalf("FirstAbove = %d %v", at, ok)
	}
	r.Append(constant(mono, 0, 300))
	if s := r.SilenceStart(0, -50, 10); s != 1000 {
		t.Fatalf("SilenceStart = %d", s)
	}
	if s := r.SilenceStart(1200, -50, 10); s != 1200 {
		t.Fatalf("SilenceStart with floor = %d", s)
	}
}
