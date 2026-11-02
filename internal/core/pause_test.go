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
