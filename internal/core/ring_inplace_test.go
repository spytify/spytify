package core

import (
	"math"
	"math/rand"
	"sort"
	"testing"

	"github.com/spytify/spytify/internal/pcm"
)

// copied is the ring search as it was before it read the ring in place: every
// level and peak was measured on a copy from Bytes. The in-place search must
// give the same numbers to the bit.
type copied struct{ *Ring }

func (o copied) rms(from, to int64) float64 {
	_, rms := o.format.Level(o.Bytes(from, to))
	return rms
}

func (o copied) quietest(lo, hi, win, step int64) (cut int64, db float64) {
	db = math.Inf(1)
	cut = lo
	if lo < o.start {
		lo = o.start
	}
	for at := lo - win/2; at+win/2 <= hi; at += step {
		from, to := at, at+win
		if from < o.start || to > o.end {
			continue
		}
		if level := o.rms(from, to); level < db-1e-9 {
			db, cut = level, at+win/2
		}
	}
	if math.IsInf(db, 1) {
		return lo, pcm.Silence
	}
	return cut, db
}

func (o copied) medianRMS(lo, hi, win int64) float64 {
	lo, hi = max(lo, o.start), min(hi, o.end)
	var levels []float64
	for at := lo; at+win <= hi; at += win {
		levels = append(levels, o.rms(at, at+win))
	}
	if len(levels) == 0 {
		return pcm.Silence
	}
	sort.Float64s(levels)
	return levels[len(levels)/2]
}

func (o copied) firstAbove(from int64, db float64, block int64) (int64, bool) {
	from = max(from, o.start)
	for at := from; at+block <= o.end; at += block {
		if o.rms(at, at+block) > db {
			data := o.Bytes(at, at+block)
			thr := math.Pow(10, db/20)
			for i := int64(0); i < block; i++ {
				for c := 0; c < o.format.Channels; c++ {
					if math.Abs(o.format.SampleAt(data, int(i)*o.format.Channels+c)) > thr {
						return at + i, true
					}
				}
			}
			return at, true
		}
	}
	return 0, false
}

func (o copied) peaks(lo, hi int64) []float64 {
	data := o.Bytes(lo, hi)
	peak := make([]float64, hi-lo)
	for i := range peak {
		for c := 0; c < o.format.Channels; c++ {
			if v := math.Abs(o.format.SampleAt(data, i*o.format.Channels+c)); v > peak[i] {
				peak[i] = v
			}
		}
	}
	return peak
}

func windowMax(peak []float64) float64 {
	m := 0.0
	for _, v := range peak {
		m = max(m, v)
	}
	return m
}

func (o copied) quietestPeak(lo, hi, win int64) (at int64, peakDB, medianDB float64) {
	lo, hi = max(lo, o.start), min(hi, o.end)
	n := int(hi - lo)
	if n < int(win) || win <= 0 {
		return lo, pcm.Silence, pcm.Silence
	}
	peak := o.peaks(lo, hi)
	best, bestPeak := 0, math.Inf(1)
	for i := 0; i+int(win) <= n; i++ {
		if m := windowMax(peak[i : i+int(win)]); m < bestPeak-1e-12 {
			best, bestPeak = i, m
		}
	}
	var tiles []float64
	for i := 0; i+int(win) <= n; i += int(win) {
		tiles = append(tiles, windowMax(peak[i:i+int(win)]))
	}
	sort.Float64s(tiles)
	return lo + int64(best), pcm.DBFS(bestPeak), pcm.DBFS(tiles[len(tiles)/2])
}

func (o copied) refineCut(cut, span, win int64) int64 {
	lo, hi := cut-span, cut+span
	lo = max(lo, o.start)
	if hi+win > o.end {
		hi = o.end - win
	}
	if hi < lo {
		return cut
	}
	peak := o.peaks(lo, hi+win)
	best, bestPeak := cut, math.Inf(1)
	for at := 0; at+int(win) <= len(peak); at++ {
		if m := windowMax(peak[at : at+int(win)]); m < bestPeak-1e-12 {
			best, bestPeak = lo+int64(at), m
		}
	}
	return best
}

func (o copied) extendBack(at int64, floorDB float64, run, maxBack, limit int64) int64 {
	limit = max(limit, o.start)
	from := max(at-maxBack, limit)
	data := o.Bytes(from, at)
	thr := math.Pow(10, floorDB/20)
	quiet, best := int64(0), at
	for i := at - 1; i >= from && quiet < run; i-- {
		loud := false
		for c := 0; c < o.format.Channels; c++ {
			if math.Abs(o.format.SampleAt(data, int(i-from)*o.format.Channels+c)) > thr {
				loud = true
				break
			}
		}
		if loud {
			best, quiet = i, 0
		} else {
			quiet++
		}
	}
	return best
}

func (o copied) silenceStart(floor int64, db float64, block int64) int64 {
	floor = max(floor, o.start)
	at := o.end
	for at-block >= floor && o.rms(at-block, at) <= db {
		at -= block
	}
	return at
}

// wrappedRing is a 2 s ring of tracks and gaps that has wrapped around
// several times, so its windows straddle the end of its buffer.
func wrappedRing(f pcm.Format, seed int64) *Ring {
	r := NewRing(f, 2)
	rnd := rand.New(rand.NewSource(seed))
	for i := int64(0); i < 12; i++ {
		ms := 50 + rnd.Int63n(700)
		if i%3 == 2 {
			r.Append(hush(f, ms))
			continue
		}
		r.Append(pcm.Synth{Seed: seed + i, LevelDB: -6 - float64(rnd.Intn(40)), FadeInMs: 30, FadeOut: 30}.Render(f, f.FramesFromMs(ms)))
	}
	return r
}

// Reading the ring in place measures what the copies measured, to the bit,
// for every search the cut makes, with windows across the buffer's end.
func TestRingSearchesInPlaceMatchTheCopies(t *testing.T) {
	formats := []pcm.Format{
		stereo44,
		{Rate: 48000, Channels: 2, Sample: pcm.F32LE},
		{Rate: 22050, Channels: 1, Sample: pcm.S16LE},
	}
	for _, f := range formats {
		for seed := int64(1); seed <= 4; seed++ {
			r := wrappedRing(f, seed)
			o := copied{r}
			if r.start%r.cap == 0 {
				t.Fatalf("seed %d: the ring did not wrap", seed)
			}
			frame, step, block, ms1 := f.FramesFromMs(frameMs), f.FramesFromMs(stepMs), f.FramesFromMs(blockMs), f.FramesFromMs(1)
			for lo := r.start - frame; lo < r.end; lo += f.FramesFromMs(97) {
				hi := lo + f.FramesFromMs(300)
				if a, b := r.RMS(lo, hi), o.rms(lo, hi); a != b {
					t.Fatalf("%v seed %d RMS(%d,%d) = %v, copy %v", f, seed, lo, hi, a, b)
				}
				c1, d1 := r.Quietest(lo, hi, frame, step)
				c2, d2 := o.quietest(lo, hi, frame, step)
				if c1 != c2 || d1 != d2 {
					t.Fatalf("%v seed %d Quietest(%d,%d) = %d %v, copy %d %v", f, seed, lo, hi, c1, d1, c2, d2)
				}
				if a, b := r.MedianRMS(lo, hi, frame), o.medianRMS(lo, hi, frame); a != b {
					t.Fatalf("%v seed %d MedianRMS = %v, copy %v", f, seed, a, b)
				}
				for _, db := range []float64{-50, -20} {
					a1, ok1 := r.FirstAbove(lo, db, block)
					a2, ok2 := o.firstAbove(lo, db, block)
					if a1 != a2 || ok1 != ok2 {
						t.Fatalf("%v seed %d FirstAbove(%d,%v) = %d %v, copy %d %v", f, seed, lo, db, a1, ok1, a2, ok2)
					}
					if a, b := r.SilenceStart(lo, db, block), o.silenceStart(lo, db, block); a != b {
						t.Fatalf("%v seed %d SilenceStart = %d, copy %d", f, seed, a, b)
					}
				}
				at1, p1, m1 := r.QuietestPeak(lo, hi, ms1)
				at2, p2, m2 := o.quietestPeak(lo, hi, ms1)
				if at1 != at2 || p1 != p2 || m1 != m2 {
					t.Fatalf("%v seed %d QuietestPeak = %d %v %v, copy %d %v %v", f, seed, at1, p1, m1, at2, p2, m2)
				}
				mid := (lo + hi) / 2
				if a, b := r.RefineCut(mid, frame, ms1), o.refineCut(mid, frame, ms1); a != b {
					t.Fatalf("%v seed %d RefineCut(%d) = %d, copy %d", f, seed, mid, a, b)
				}
				if mid <= r.end {
					back := f.FramesFromMs(100)
					if a, b := r.ExtendBack(mid, -60, ms1, back, lo), o.extendBack(mid, -60, ms1, back, lo); a != b {
						t.Fatalf("%v seed %d ExtendBack(%d) = %d, copy %d", f, seed, mid, a, b)
					}
				}
			}
		}
	}
}

// The cut search slides a 20 ms window in 5 ms steps; measuring a window
// no longer copies it out of the ring.
func TestCutSearchDoesNotCopyTheRing(t *testing.T) {
	f := stereo44
	r := wrappedRing(f, 1)
	lo, hi := r.end-f.FramesFromMs(1300), r.end
	frame, step, block := f.FramesFromMs(frameMs), f.FramesFromMs(stepMs), f.FramesFromMs(blockMs)
	for name, search := range map[string]func(){
		"RMS":          func() { r.RMS(lo, lo+frame) },
		"Quietest":     func() { r.Quietest(lo, hi, frame, step) },
		"FirstAbove":   func() { r.FirstAbove(lo, 0, block) },
		"SilenceStart": func() { r.SilenceStart(lo, 0, block) },
		"ExtendBack":   func() { r.ExtendBack(hi, -60, 44, f.FramesFromMs(100), lo) },
	} {
		if n := testing.AllocsPerRun(20, search); n != 0 {
			t.Errorf("%s allocates %v times per call, want 0", name, n)
		}
	}
}

// BenchmarkQuietest is one natural end's search: a 1.3 s window around the
// declared end, 20 ms windows every 5 ms.
func BenchmarkQuietest(b *testing.B) {
	f := stereo44
	r := wrappedRing(f, 1)
	lo, hi := r.end-f.FramesFromMs(1300), r.end
	frame, step := f.FramesFromMs(frameMs), f.FramesFromMs(stepMs)
	b.ReportAllocs()
	for b.Loop() {
		r.Quietest(lo, hi, frame, step)
	}
}

// BenchmarkQuietestCopied is the same search measured on copies, as
// before.
func BenchmarkQuietestCopied(b *testing.B) {
	f := stereo44
	o := copied{wrappedRing(f, 1)}
	lo, hi := o.end-f.FramesFromMs(1300), o.end
	frame, step := f.FramesFromMs(frameMs), f.FramesFromMs(stepMs)
	b.ReportAllocs()
	for b.Loop() {
		o.quietest(lo, hi, frame, step)
	}
}
