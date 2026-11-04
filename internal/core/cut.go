package core

import (
	"github.com/spytify/spytify/internal/meta"
	"github.com/spytify/spytify/internal/pcm"
)

const (
	// A notification may come this late after the audio changed; windows
	// and the drain hold-back cover it.
	lateNotifyMs = 1000
	// A notification this much earlier than the expected end is a skip or
	// a seek, not jitter.
	earlySlackMs   = 1000
	earlyNotifyMs  = 300
	startTimeoutMs = 2000
	frameMs        = 20
	stepMs         = 5
	blockMs        = 5
	// A window this many dB under the median of the search window is a
	// boundary even when it is not silent.
	dipDB = 15
	// After a skip, the next track starts where the peak level of 1 ms
	// windows drops this far under its median.
	skipDipDB = 20
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
	// skipLo and skipHi frame the notice of a boundary named before its
	// window, where a skip would have left its edge; both 0 otherwise.
	skipLo, skipHi int64
}

// earliest is the first frame the change's cut may land on: the drain
// writes no further until it is cut.
func (ch *change) earliest() int64 {
	if ch.skipHi > 0 && ch.skipLo < ch.lo {
		return ch.skipLo
	}
	return ch.lo
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
// c being the take it ends, if any. A playing player may name the next
// track up to earlySlackMs before the take's end; a paused one stopped
// where it was, so a change it names short of the end is a skip.
func (cf *cutFinder) plan(r *Ring, c *segment, info meta.TrackInfo, at int64, restart, paused bool) *change {
	ms := r.Format().FramesFromMs
	ch := &change{info: info, at: at, restart: restart}
	slack := ms(earlySlackMs)
	if paused {
		slack = 0
	}
	switch {
	case c == nil:
		ch.kind = changeFirst
		ch.lo = at - ms(lateNotifyMs)
		ch.expected = at
	case c.length > 0 && at >= c.origin+c.length-cf.window-slack:
		// At its end, the same item again is a new take (repeat), and the
		// one before it is complete.
		ch.restart = false
		e := c.origin + c.length
		late := cf.soundAt(r, c) + c.length
		ch.kind, ch.lo, ch.hi, ch.expected = changeBoundary, e-cf.window, late+cf.window, e
		if at < ch.lo {
			// Named before its window: Next pressed in the take's last
			// second, or a notice early for the take's natural end.
			ch.skipLo, ch.skipHi = at-ms(lateNotifyMs), at+ms(earlyNotifyMs)
		}
	default:
		ch.kind, ch.lo, ch.hi, ch.expected = changeInterrupt, at-ms(lateNotifyMs), at+ms(earlyNotifyMs), at
		if paused {
			// Named while paused, the item changed after the sound stopped
			// for the pause: none of the audio before it is the new item's.
			ch.lo = at
		}
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
	if ch.skipHi > 0 && ch.skipLo < floor {
		ch.skipLo = floor
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
		if p.skipHi > p.skipLo {
			// With no silence or dip where the take was to end, a skip's
			// edge near the notice is where it ended.
			at, peak, median := r.QuietestPeak(p.skipLo, min(p.skipHi, r.End()), f1ms(r))
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
