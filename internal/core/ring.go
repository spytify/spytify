package core

import (
	"math"
	"sort"

	"github.com/spytify/spytify/internal/pcm"
)

// Ring holds the most recent frames of the capture in one format, addressed
// by absolute frame index since the stream (or its last format change)
// started. The session drains frames out of it into track files; whatever
// has not been drained is still available for the boundary search.
type Ring struct {
	format pcm.Format
	buf    []byte
	cap    int64
	start  int64
	end    int64
}

// NewRing allocates seconds of frames.
func NewRing(f pcm.Format, seconds float64) *Ring {
	n := int64(math.Ceil(seconds * float64(f.Rate)))
	return &Ring{format: f, buf: make([]byte, n*int64(f.FrameBytes())), cap: n}
}

func (r *Ring) Format() pcm.Format { return r.format }
func (r *Ring) Start() int64       { return r.start }
func (r *Ring) End() int64         { return r.end }
func (r *Ring) Cap() int64         { return r.cap }
func (r *Ring) Len() int64         { return r.end - r.start }
func (r *Ring) Free() int64        { return r.cap - r.Len() }

// Append adds whole frames. Frames that do not fit push the oldest out; the
// return value counts them, and the caller treats any as an overrun.
func (r *Ring) Append(data []byte) (lost int64) {
	fb := int64(r.format.FrameBytes())
	n := int64(len(data)) / fb
	if n > r.cap {
		data = data[(n-r.cap)*fb:]
		lost += n - r.cap
		r.start = r.end + n - r.cap
		r.end = r.start
		n = r.cap
	}
	if over := r.Len() + n - r.cap; over > 0 {
		r.start += over
		lost += over
	}
	for written := int64(0); written < n; {
		pos := (r.end + written) % r.cap
		run := r.cap - pos
		if run > n-written {
			run = n - written
		}
		copy(r.buf[pos*fb:(pos+run)*fb], data[written*fb:(written+run)*fb])
		written += run
	}
	r.end += n
	return lost
}

// Bytes copies frames [from, to), clamped to what the ring holds.
func (r *Ring) Bytes(from, to int64) []byte {
	if from < r.start {
		from = r.start
	}
	if to > r.end {
		to = r.end
	}
	if to <= from {
		return nil
	}
	fb := int64(r.format.FrameBytes())
	out := make([]byte, (to-from)*fb)
	for done := int64(0); done < to-from; {
		pos := (from + done) % r.cap
		run := r.cap - pos
		if run > to-from-done {
			run = to - from - done
		}
		copy(out[done*fb:(done+run)*fb], r.buf[pos*fb:(pos+run)*fb])
		done += run
	}
	return out
}

// Discard forgets frames before upTo.
func (r *Ring) Discard(upTo int64) {
	if upTo > r.end {
		upTo = r.end
	}
	if upTo > r.start {
		r.start = upTo
	}
}

// Truncate drops frames at or after to, for a pause noticed late.
func (r *Ring) Truncate(to int64) {
	if to < r.start {
		to = r.start
	}
	if to < r.end {
		r.end = to
	}
}

// RMS is the level of frames [from, to) in dBFS.
func (r *Ring) RMS(from, to int64) float64 {
	_, rms := r.format.Level(r.Bytes(from, to))
	return rms
}

// Quietest slides a window of win frames over [lo, hi] in steps of step
// frames and returns the middle of the quietest one and its level. Ties go
// to the earliest window, so a cut lands at the start of a silent gap.
func (r *Ring) Quietest(lo, hi, win, step int64) (cut int64, db float64) {
	db = math.Inf(1)
	cut = lo
	if lo < r.start {
		lo = r.start
	}
	for at := lo - win/2; at+win/2 <= hi; at += step {
		from, to := at, at+win
		if from < r.start || to > r.end {
			continue
		}
		if level := r.RMS(from, to); level < db-1e-9 {
			db, cut = level, at+win/2
		}
	}
	if math.IsInf(db, 1) {
		return lo, pcm.Silence
	}
	return cut, db
}

// MedianRMS is the median level of consecutive win-frame windows in [lo, hi).
func (r *Ring) MedianRMS(lo, hi, win int64) float64 {
	if lo < r.start {
		lo = r.start
	}
	if hi > r.end {
		hi = r.end
	}
	var levels []float64
	for at := lo; at+win <= hi; at += win {
		levels = append(levels, r.RMS(at, at+win))
	}
	if len(levels) == 0 {
		return pcm.Silence
	}
	sort.Float64s(levels)
	return levels[len(levels)/2]
}

// FirstAbove returns the first frame at or after from that starts a block of
// block frames louder than db, scanning to the end of the ring.
func (r *Ring) FirstAbove(from int64, db float64, block int64) (int64, bool) {
	if from < r.start {
		from = r.start
	}
	for at := from; at+block <= r.end; at += block {
		if r.RMS(at, at+block) > db {
			// Refine to the frame inside the block.
			data := r.Bytes(at, at+block)
			thr := math.Pow(10, db/20)
			for i := int64(0); i < block; i++ {
				for c := 0; c < r.format.Channels; c++ {
					if math.Abs(r.format.SampleAt(data, int(i)*r.format.Channels+c)) > thr {
						return at + i, true
					}
				}
			}
			return at, true
		}
	}
	return 0, false
}

// SilenceStart walks back from the end of the ring over blocks quieter than
// db and returns where that trailing silence begins, no earlier than floor.
func (r *Ring) SilenceStart(floor int64, db float64, block int64) int64 {
	if floor < r.start {
		floor = r.start
	}
	at := r.end
	for at-block >= floor && r.RMS(at-block, at) <= db {
		at -= block
	}
	return at
}
