package core

import (
	"github.com/spytify/spytify/internal/meta"
	"github.com/spytify/spytify/internal/pcm"
)

type changeKind int

const (
	changeFirst changeKind = iota
	changeBoundary
	changeInterrupt
)

type change struct {
	kind     changeKind
	info     meta.TrackInfo
	at       int64
	lo, hi   int64
	expected int64
	ready    int64
	// restart: the same item started over, so the take before is partial.
	restart bool
}

// String names the kind as track.started's cut says it.
func (k changeKind) String() string {
	switch k {
	case changeFirst:
		return "first"
	case changeBoundary:
		return "boundary"
	}
	return "interrupt"
}

// cutFinder finds where one track ends and the next begins: plan places
// the window to search when the player names a track, and resolve picks
// the cut in it once the ring holds that window. It works on the ring and
// the take it is given, and keeps only its sizes, in frames of the ring's
// format, which the session sets in tune.
type cutFinder struct {
	silenceDB float64
	window    int64
	frame     int64
	step      int64
	block     int64
	holdback  int64
}

// plan places the search window for a track change noticed at sample at,
// c being the take it ends, if any.
func (cf *cutFinder) plan(r *Ring, c *segment, info meta.TrackInfo, at int64, restart bool) *change {
	ms := r.Format().FramesFromMs
	ch := &change{info: info, at: at, restart: restart}
	switch {
	case c == nil:
		ch.kind = changeFirst
		ch.lo = at - ms(lateNotifyMs)
		ch.expected = at
	case c.length > 0 && at >= c.origin+c.length-cf.window-ms(earlySlackMs):
		// At its end, the same item again is a new take (repeat), and the
		// one before it is complete.
		ch.restart = false
		e := c.origin + c.length
		late := cf.soundAt(r, c) + c.length
		ch.kind, ch.lo, ch.hi, ch.expected = changeBoundary, e-cf.window, late+cf.window, e
	default:
		ch.kind, ch.lo, ch.hi, ch.expected = changeInterrupt, at-ms(lateNotifyMs), at+ms(earlyNotifyMs), at
	}
	floor := r.Start()
	if c != nil {
		if c.drained > floor {
			floor = c.drained
		}
		if c.start+1 > floor {
			floor = c.start + 1
		}
	}
	if ch.lo < floor {
		ch.lo = floor
	}
	if ch.hi < ch.lo {
		ch.hi = ch.lo
	}
	if ch.expected < ch.lo {
		ch.expected = ch.lo
	}
	ch.ready = ch.hi + cf.frame/2 + 1
	if ch.kind == changeFirst {
		ch.ready = ch.lo
	}
	return ch
}

// soundAt is where the segment's audio begins, looking into the ring when
// the drain has not reached it yet; start when there is no sound so far.
func (cf *cutFinder) soundAt(r *Ring, c *segment) int64 {
	if c.soundAt >= 0 {
		return c.soundAt
	}
	if at, ok := r.FirstAbove(maxInt(c.start, r.Start()), cf.silenceDB, cf.block); ok {
		return at
	}
	return c.start
}

// firstAbove is Ring.FirstAbove over a byte slice: the first frame of the
// first block louder than db.
func firstAbove(f pcm.Format, data []byte, db float64, block int64) (int64, bool) {
	fb := int64(f.FrameBytes())
	n := int64(len(data)) / fb
	thr := pow10(db / 20)
	for at := int64(0); at < n; at += block {
		end := at + block
		if end > n {
			end = n
		}
		if _, rms := f.Level(data[at*fb : end*fb]); rms <= db {
			continue
		}
		for i := at; i < end; i++ {
			for ch := 0; ch < f.Channels; ch++ {
				v := f.SampleAt(data, int(i)*f.Channels+ch)
				if v > thr || v < -thr {
					return i, true
				}
			}
		}
		return at, true
	}
	return 0, false
}

// resolve picks the cut for the change p once the ring holds the audio its
// search needs, or right away when force is set; ok is false while it
// waits. c is the take the cut ends, if any.
func (cf *cutFinder) resolve(r *Ring, p *change, c *segment, force bool) (cut int64, by string, ok bool) {
	if !force && r.End() < p.ready {
		return 0, "", false
	}
	switch p.kind {
	case changeFirst:
		at, found := cf.firstSound(r, p.lo)
		by = "first_sound"
		if !found {
			by = "notice"
			if !force && r.End() < p.at+r.Format().FramesFromMs(startTimeoutMs) {
				return 0, "", false
			}
			at = p.at
			if at > r.End() {
				at = r.End()
			}
		}
		cut = at
	default:
		hi := p.hi
		if hi > r.End() {
			hi = r.End()
		}
		q, db := r.Quietest(p.lo, hi, cf.frame, cf.step)
		refine := func(c int64) int64 { return r.RefineCut(c, cf.frame, f1ms(r)) }
		var edge int64
		edgeOK := false
		if p.kind == changeInterrupt {
			at, peak, median := r.QuietestPeak(p.lo, hi, f1ms(r))
			edge, edgeOK = at, peak <= median-skipDipDB
		}
		switch {
		case db <= cf.silenceDB:
			cut, by = refine(q), "silence"
		case db <= r.MedianRMS(p.lo, hi, cf.frame)-dipDB:
			// No silence, but a clear dip: two tracks faded into each
			// other.
			cut, by = refine(q), "dip"
		case edgeOK:
			// A skip cuts a track mid-note; the next one starts where the
			// peak level drops for a millisecond, at its first sample.
			cut, by = edge, "edge"
		case p.kind == changeBoundary && c != nil:
			// Gapless: trust the declared length, counted from the
			// track's first sound.
			cut, by = cf.soundAt(r, c)+c.length, "length"
		default:
			cut, by = p.expected, "notice"
		}
		if cut > r.End() {
			cut = r.End()
		}
	}
	if c != nil && cut < c.drained {
		cut = c.drained
	}
	return cut, by, true
}

func f1ms(r *Ring) int64 { return r.Format().FramesFromMs(1) }

// firstSound is where a track starts when it starts at its first sound:
// the first block above silence_db, moved back over the quiet beginning of
// its fade-in, no earlier than from.
func (cf *cutFinder) firstSound(r *Ring, from int64) (int64, bool) {
	at, ok := r.FirstAbove(from, cf.silenceDB, cf.block)
	if !ok {
		return 0, false
	}
	f := r.Format()
	return r.ExtendBack(at, cf.silenceDB-12, f.FramesFromMs(1), f.FramesFromMs(100), from), true
}
