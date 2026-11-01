package core

import (
	"bytes"
	"testing"

	"github.com/spytify/spytify/internal/pcm"
)

func frameCount(f pcm.Format, data []byte) int64 { return int64(len(data) / f.FrameBytes()) }

// While paused, a silent run of 20 ms is cut out, a shorter one is kept,
// and sound after it is kept whole.
func TestExciseCutsSilentRunsOf20Ms(t *testing.T) {
	f := stereo44
	ms := f.FramesFromMs
	note := tone(f, 10, 4)
	for _, c := range []struct {
		name    string
		silence int64
		kept    int64
	}{
		{"20 ms", 20, ms(20)},
		{"30 ms", 30, ms(20)},
		{"15 ms", 15, ms(35)},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := &pauseExcision{pausing: true}
			r := NewRing(f, ringSeconds)
			data := append(append(append([]byte(nil), note...), hush(f, c.silence)...), note...)
			out := p.excise(f, data, r, r.Start, -50)
			if got := frameCount(f, out); got != c.kept {
				t.Fatalf("kept %d frames, want %d", got, c.kept)
			}
			if !bytes.Equal(out[:len(note)], note) || !bytes.Equal(out[len(out)-len(note):], note) {
				t.Fatal("the notes were not kept whole")
			}
			if p.excising || !p.pausing {
				t.Fatalf("after a note: %+v", *p)
			}
		})
	}
}

// A run that began in an earlier chunk is cut from the ring too, no
// earlier than the floor.
func TestExciseCutsTheStartOfARunLeftInTheRing(t *testing.T) {
	f := stereo44
	ms := f.FramesFromMs
	for _, c := range []struct {
		name  string
		floor int64 // ms from the ring's end, back
		end   int64
	}{
		{"whole run", 1000, ms(500)},
		{"floor", 5, ms(510)},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := &pauseExcision{pausing: true}
			r := NewRing(f, ringSeconds)
			first := append(tone(f, 500, 5), hush(f, 15)...)
			r.Append(p.excise(f, first, r, r.Start, -50))
			floor := r.End() - ms(c.floor)
			out := p.excise(f, append(hush(f, 10), tone(f, 10, 6)...), r, func() int64 { return floor }, -50)
			if r.End() != c.end || frameCount(f, out) != ms(10) {
				t.Fatalf("ring ends at %d, %d frames kept; want %d and %d", r.End(), frameCount(f, out), c.end, ms(10))
			}
		})
	}
}

// Once resumed, the first sound ends the pause: what follows is kept as
// it comes, silence included.
func TestExciseEndsAtTheFirstSoundAfterResuming(t *testing.T) {
	f := stereo44
	ms := f.FramesFromMs
	p := &pauseExcision{pausing: true, excising: true, resumed: true}
	r := NewRing(f, ringSeconds)
	data := append(append(hush(f, 10), tone(f, 10, 7)...), hush(f, 30)...)
	out := p.excise(f, data, r, r.Start, -50)
	if frameCount(f, out) != ms(40) || p.pausing || p.excising {
		t.Fatalf("kept %d frames, %+v", frameCount(f, out), *p)
	}
}

// A Paused notification that came after the audio stopped cuts the
// silence already captured back to where it began, and goes on cutting;
// under 20 ms of silence is kept and counted.
func TestTrimCutsBackTheSilenceCaptured(t *testing.T) {
	f := stereo44
	ms := f.FramesFromMs
	p := &pauseExcision{pausing: true}
	r := ringOf(f, tone(f, 500, 8), hush(f, 300))
	p.trim(r, 0, -50)
	if r.End() != ms(500) || !p.excising || p.quietRun != 0 {
		t.Fatalf("ring ends at %d, %+v", r.End(), *p)
	}

	p = &pauseExcision{pausing: true}
	r = ringOf(f, tone(f, 500, 8), hush(f, 10))
	p.trim(r, 0, -50)
	if r.End() != ms(510) || p.excising || p.quietRun != ms(10) {
		t.Fatalf("ring ends at %d, %+v", r.End(), *p)
	}

	p = &pauseExcision{pausing: true}
	r = ringOf(f, tone(f, 500, 8), hush(f, 300))
	p.trim(r, ms(700), -50)
	if r.End() != ms(700) {
		t.Fatalf("ring ends at %d, want the floor %d", r.End(), ms(700))
	}
}

// fading ramps data's gain from full to -60 dB (out) or back, as a player
// does when it pauses and when it resumes. A real take's fades were
// straight lines in dB.
func fading(f pcm.Format, data []byte, out bool) []byte {
	b := append([]byte(nil), data...)
	n := len(b) / f.FrameBytes()
	for i := 0; i < n; i++ {
		x := float64(i) / float64(n)
		if !out {
			x = 1 - x
		}
		g := pow10(-60 * x / 20)
		for c := 0; c < f.Channels; c++ {
			k := i*f.Channels + c
			f.PutSample(b, k, f.SampleAt(b, k)*g)
		}
	}
	return b
}

// The player's fade-out before a pause and its fade-in after it meet where
// the pause was cut out: the sound falls into the cut and rises out of it.
func TestFadedSeam(t *testing.T) {
	f := stereo44
	music := func(ms int64) []byte { return tone(f, ms, 4) }
	note := tone(f, 10, 8)
	for _, c := range []struct {
		name  string
		audio [][]byte
		seam  int64 // ms
		faded bool
	}{
		{"fades meet", [][]byte{music(500), fading(f, music(150), true), fading(f, music(60), false), music(500)}, 650, true},
		{"cut without fades", [][]byte{music(500), music(500)}, 500, false},
		{"fade-out only", [][]byte{music(500), fading(f, music(150), true), music(500)}, 650, false},
		{"fade-in only", [][]byte{music(500), fading(f, music(60), false), music(500)}, 500, false},
		// Notes while paused, 18 ms of silence between them too short to
		// cut: the silence is near the cut, but the sound neither falls
		// into it nor rises out of it.
		{"silence kept near the cut", [][]byte{music(500), note, hush(f, 18), note, music(500)}, 538, false},
		{"silence after the fade-out", [][]byte{music(500), fading(f, music(150), true), hush(f, 500)}, 650, false},
		{"too near the ring's start", [][]byte{fading(f, music(150), true), fading(f, music(60), false), music(500)}, 150, false},
		{"too near its end", [][]byte{music(500), fading(f, music(150), true), fading(f, music(60), false), music(100)}, 650, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := NewRing(f, ringSeconds)
			for _, a := range c.audio {
				r.Append(a)
			}
			if dip, faded := fadedSeam(r, f.FramesFromMs(c.seam)); faded != c.faded {
				t.Fatalf("faded %v (%.1f dB under the music), want %v", faded, dip, c.faded)
			}
		})
	}
}

// Where sound comes back after a cut, excise marks the seam: the ring
// frame that sound lands on, whether the player said it resumed or not.
func TestExciseMarksTheSeam(t *testing.T) {
	f := stereo44
	note := tone(f, 10, 4)
	for _, resumed := range []bool{false, true} {
		p := &pauseExcision{pausing: true, resumed: resumed}
		r := NewRing(f, ringSeconds)
		r.Append(tone(f, 100, 4))
		data := append(append(append([]byte(nil), note...), hush(f, 50)...), note...)
		out := p.excise(f, data, r, r.Start, -50)
		if want := r.End() + frameCount(f, note); p.seam != want {
			t.Fatalf("resumed %v: seam at %d, want %d", resumed, p.seam, want)
		}
		if got := frameCount(f, out); got != 2*frameCount(f, note) {
			t.Fatalf("resumed %v: kept %d frames", resumed, got)
		}
	}
}
