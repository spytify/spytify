package core

import (
	"math"
	"testing"

	"github.com/spytify/spytify/internal/meta"
	"github.com/spytify/spytify/internal/pcm"
)

var stereo44 = pcm.Format{Rate: 44100, Channels: 2, Sample: pcm.S16LE}

// finderFor is a cutFinder sized as a session sizes one for f with the
// default settings: -50 dBFS of silence and a 300 ms window.
func finderFor(f pcm.Format) *cutFinder {
	window := f.FramesFromMs(300)
	return &cutFinder{
		silenceDB: -50,
		window:    window,
		frame:     f.FramesFromMs(frameMs),
		step:      f.FramesFromMs(stepMs),
		block:     f.FramesFromMs(blockMs),
		holdback:  f.FramesFromMs(lateNotifyMs) + window,
	}
}

// ringOf is a ring holding the pieces one after the other.
func ringOf(f pcm.Format, pieces ...[]byte) *Ring {
	r := NewRing(f, ringSeconds)
	for _, p := range pieces {
		r.Append(p)
	}
	return r
}

func tone(f pcm.Format, ms int64, seed int64) []byte {
	return pcm.Synth{Seed: seed}.Render(f, f.FramesFromMs(ms))
}

func hush(f pcm.Format, ms int64) []byte {
	return make([]byte, f.FramesFromMs(ms)*int64(f.FrameBytes()))
}

// takeFrom is a take that started at its first sound at startMs, with a
// declared length.
func takeFrom(f pcm.Format, startMs, lengthMs int64) *segment {
	start := f.FramesFromMs(startMs)
	return &segment{start: start, origin: start, drained: start, soundAt: start, length: f.FramesFromMs(lengthMs), format: f}
}

// The window placed for each kind of change: the first track's from 1 s
// before the notice, a natural end's around the declared end, a skip's
// around the notice; never before what the take before has drained.
func TestPlanPlacesTheSearchWindow(t *testing.T) {
	f := stereo44
	ms := f.FramesFromMs
	cf := finderFor(f)
	r := ringOf(f, hush(f, 3000))
	next := meta.TrackInfo{Title: "B"}
	for _, c := range []struct {
		name                    string
		c                       *segment
		atMs                    int64
		restart                 bool
		kind                    changeKind
		lo, hi, expected, ready int64
		wantRestart             bool
	}{
		{"first", nil, 2500, false, changeFirst, ms(1500), ms(1500), ms(2500), ms(1500), false},
		{"first, the ring holding less than 1 s", nil, 500, false, changeFirst, 0, 0, ms(500), 0, false},
		{"natural end", takeFrom(f, 0, 2000), 1900, true, changeBoundary, ms(1700), ms(2300), ms(2000), ms(2300) + ms(frameMs)/2 + 1, false},
		{"skip", takeFrom(f, 0, 2800), 1000, false, changeInterrupt, 1, ms(1300), ms(1000), ms(1300) + ms(frameMs)/2 + 1, false},
		{"skip, the same track again", takeFrom(f, 0, 2800), 1000, true, changeInterrupt, 1, ms(1300), ms(1000), ms(1300) + ms(frameMs)/2 + 1, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := cf.plan(r, c.c, next, ms(c.atMs), c.restart)
			if p.kind != c.kind || p.lo != c.lo || p.hi != c.hi || p.expected != c.expected || p.ready != c.ready || p.restart != c.wantRestart {
				t.Fatalf("planned %+v", *p)
			}
		})
	}
	t.Run("no earlier than what was drained", func(t *testing.T) {
		c := takeFrom(f, 0, 2800)
		c.drained = ms(800)
		if p := cf.plan(r, c, next, ms(1000), false); p.lo != ms(800) {
			t.Fatalf("lo %d, want %d", p.lo, ms(800))
		}
	})
}

// Each way a cut is found, and where it lands.
func TestResolveFindsTheCut(t *testing.T) {
	f := stereo44
	ms := f.FramesFromMs
	cf := finderFor(f)
	wave := func(n int64, amp float64) []byte {
		b := make([]byte, n*int64(f.FrameBytes()))
		for i := int64(0); i < n; i++ {
			v := amp
			if i%2 == 1 {
				v = -amp
			}
			for ch := 0; ch < f.Channels; ch++ {
				f.PutSample(b, int(i)*f.Channels+ch, v)
			}
		}
		return b
	}
	for _, c := range []struct {
		name  string
		ring  *Ring
		c     *segment
		atMs  int64
		by    string
		cutAt int64
	}{
		// A ends at 2 s, B starts at 2.5 s: the cut is where the silence
		// begins.
		{"silence", ringOf(f, tone(f, 2000, 1), hush(f, 500), tone(f, 1000, 2)), takeFrom(f, 0, 2000), 2500, "silence", ms(2000)},
		// No gap, no dip: the declared length.
		{"length", ringOf(f, tone(f, 2000, 1), tone(f, 1000, 2)), takeFrom(f, 0, 2000), 2000, "length", ms(2000)},
		// 100 ms 30 dB quieter at A's end.
		{"dip", ringOf(f, wave(ms(1900), 0.25), wave(ms(100), 0.25*math.Pow(10, -30.0/20)), wave(ms(1000), 0.25)), takeFrom(f, 0, 2000), 2000, "dip", ms(1900)},
		// A skip at 900 ms, where the peak drops 25 dB for 1 ms, 100 ms
		// before the notice.
		{"edge", ringOf(f, wave(ms(900), 0.25), wave(50, 0.25*math.Pow(10, -25.0/20)), wave(ms(1100)-50, 0.25)), takeFrom(f, 0, 3000), 1000, "edge", ms(900)},
		// A skip with nothing to cut at: the notice.
		{"notice", ringOf(f, wave(ms(2000), 0.25)), takeFrom(f, 0, 3000), 1000, "notice", ms(1000)},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := cf.plan(c.ring, c.c, meta.TrackInfo{Title: "B"}, ms(c.atMs), false)
			cut, by, ok := cf.resolve(c.ring, p, c.c, false)
			if !ok || by != c.by || cut != c.cutAt {
				t.Fatalf("cut at %d by %q (%v), want %d by %q", cut, by, ok, c.cutAt, c.by)
			}
		})
	}
}

// A change waits until the ring holds its window, unless forced; a first
// track waits for its first sound until the start timeout.
func TestResolveWaitsForItsAudio(t *testing.T) {
	f := stereo44
	ms := f.FramesFromMs
	cf := finderFor(f)
	r := ringOf(f, tone(f, 2000, 1), hush(f, 200))
	c := takeFrom(f, 0, 2000)
	p := cf.plan(r, c, meta.TrackInfo{Title: "B"}, ms(2100), false)
	if _, _, ok := cf.resolve(r, p, c, false); ok {
		t.Fatal("resolved before the ring held the window")
	}
	if cut, by, ok := cf.resolve(r, p, c, true); !ok || by != "silence" || cut != ms(2000) {
		t.Fatalf("forced: cut at %d by %q (%v)", cut, by, ok)
	}

	quiet := ringOf(f, hush(f, 1500))
	first := cf.plan(quiet, nil, meta.TrackInfo{Title: "A"}, ms(1000), false)
	if _, _, ok := cf.resolve(quiet, first, nil, false); ok {
		t.Fatal("a first track with no sound resolved before the start timeout")
	}
	quiet.Append(hush(f, 1600))
	if cut, by, ok := cf.resolve(quiet, first, nil, false); !ok || by != "notice" || cut != ms(1000) {
		t.Fatalf("after the start timeout: cut at %d by %q (%v)", cut, by, ok)
	}
}

// The cut never goes back into what the take before has drained.
func TestResolveKeepsWhatWasDrained(t *testing.T) {
	f := stereo44
	ms := f.FramesFromMs
	cf := finderFor(f)
	r := ringOf(f, tone(f, 2000, 1), hush(f, 500), tone(f, 1000, 2))
	c := takeFrom(f, 0, 2000)
	p := cf.plan(r, c, meta.TrackInfo{Title: "B"}, ms(2500), false)
	c.drained = ms(2200)
	if cut, _, _ := cf.resolve(r, p, c, false); cut != ms(2200) {
		t.Fatalf("cut at %d, want %d", cut, ms(2200))
	}
}

// A first sound is moved back over the quiet start of its fade-in; soundAt
// is not, and looks into the ring when the take has not found its own yet.
func TestFirstSoundAndSoundAt(t *testing.T) {
	f := stereo44
	ms := f.FramesFromMs
	cf := finderFor(f)
	r := ringOf(f, hush(f, 500), pcm.Synth{Seed: 3, FadeInMs: 50}.Render(f, ms(1000)))
	above, _ := r.FirstAbove(0, cf.silenceDB, cf.block)
	at, ok := cf.firstSound(r, 0)
	if !ok || at < ms(500) || at >= above {
		t.Fatalf("first sound at %d (%v), want from %d to before %d, the first frame above -50 dBFS", at, ok, ms(500), above)
	}
	if _, ok := cf.firstSound(r, ms(1500)); ok {
		t.Fatal("found a sound past the ring's end")
	}
	c := &segment{start: 0, soundAt: -1}
	if got := cf.soundAt(r, c); got != above {
		t.Fatalf("soundAt %d, want %d", got, above)
	}
	c.soundAt = 7
	if got := cf.soundAt(r, c); got != 7 {
		t.Fatalf("soundAt %d, want the take's own 7", got)
	}
	silent := ringOf(f, hush(f, 500))
	if got := cf.soundAt(silent, &segment{start: 3, soundAt: -1}); got != 3 {
		t.Fatalf("soundAt %d in silence, want the start", got)
	}
}
