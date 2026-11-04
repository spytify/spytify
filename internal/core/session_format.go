package core

import (
	"fmt"

	"github.com/spytify/spytify/internal/meta"
	"github.com/spytify/spytify/internal/pcm"
	"github.com/spytify/spytify/pkg/proto"
)

// After a format change, a track change within this window names the
// new stream rather than cutting it.
const provisionalMs = 3000

func (s *Session) defaultFormat() pcm.Format {
	return pcm.Format{Rate: 44100, Channels: 2, Sample: pcm.S16LE}
}

// formatChange makes sure a stream recreated in another format never lands
// inside one file.
func (s *Session) formatChange(f pcm.Format) {
	old := s.ring
	s.ring = NewRing(f, ringSeconds)
	s.seam = 0
	s.tune(f)
	if old == nil {
		return
	}
	s.deps.log.Infof("stream format changed from %s to %s", old.Format(), f)
	if s.pend != nil {
		saved := s.ring
		s.ring = old
		s.tryResolve(true)
		s.ring = saved
	}
	c := s.cur
	if c == nil {
		return
	}
	saved := s.ring
	s.ring = old
	s.write(c, c.drained, old.End())
	playedMs := old.Format().MsFromFrames(old.End() - c.start)
	justStarted := !c.ghost && !c.provisional && old.End()-c.start <= old.Format().FramesFromMs(provisionalMs)
	if justStarted {
		// The track had only just been cut when the stream was recreated:
		// what it holds is the gap, and the track itself is in the new
		// stream.
		s.dropWriter(c)
		s.ring = saved
		// Unless the take held sound for longer than the cut's window: the
		// track was playing, and the stream was recreated for another
		// reason, such as a call taking the headset. It plays on in the
		// new stream for what is left of its length, and what the old
		// stream held is lost from its file.
		heard := c.soundAt >= 0 && old.End()-c.soundAt > old.Format().FramesFromMs(f.MsFromFrames(s.window))
		lostMs := int64(0)
		if heard {
			lostMs = old.Format().MsFromFrames(old.End() - c.soundAt)
		}
		c.format, c.start, c.origin, c.drained, c.soundAt = f, 0, 0, 0, -1
		c.length = f.FramesFromMs(max(0, c.info.LengthMs-lostMs))
		c.partial = lostMs > completeToleranceMs
		c.awaitStart, c.startDeadline = true, s.ms(startTimeoutMs)
		if c.reason == "" {
			if w, err := s.tempWAV(c.id, f); err == nil {
				c.w = w
			} else {
				c.reason = proto.SkipIncomplete
			}
		}
		return
	}
	if !c.ghost && !c.provisional {
		c.partial = c.length > 0 && old.End()-c.start < c.length-old.Format().FramesFromMs(2000)
		s.closeSegment(c, old.End())
	}
	s.ring = saved
	next := &segment{
		id:          c.id + "+",
		info:        c.info,
		format:      f,
		ghost:       c.ghost,
		provisional: true,
		soundAt:     -1,
	}
	if c.info.LengthMs > playedMs {
		next.length = f.FramesFromMs(c.info.LengthMs - playedMs)
	}
	s.cur = next
}

// reformat replaces an empty ring with one in format f, carrying lengths
// and windows over in milliseconds.
func (s *Session) reformat(f pcm.Format) {
	old := s.ring.Format()
	conv := func(frames int64) int64 { return f.FramesFromMs(old.MsFromFrames(frames)) }
	s.ring = NewRing(f, ringSeconds)
	s.tune(f)
	if c := s.cur; c != nil {
		c.format = f
		c.length = conv(c.length)
		c.startDeadline = conv(c.startDeadline)
		if c.w != nil {
			s.dropWriter(c)
			if w, err := s.tempWAV(c.id, f); err == nil {
				c.w = w
			} else {
				c.reason = proto.SkipIncomplete
			}
		}
	}
	if p := s.pend; p != nil {
		p.lo, p.hi, p.expected, p.ready = conv(p.lo), conv(p.hi), conv(p.expected), conv(p.ready)
		if p.skipHi > 0 {
			p.skipLo, p.skipHi = conv(p.skipLo), conv(p.skipHi)
		}
	}
}

// adopt names a provisional segment after a format change.
func (s *Session) adopt(c *segment, info meta.TrackInfo) {
	s.n++
	c.id = fmt.Sprintf("%s-%03d", s.id, s.n)
	c.info = info
	c.ghost, c.provisional = false, false
	c.length = c.format.FramesFromMs(info.LengthMs)
	c.awaitStart, c.startDeadline = true, c.start+s.ms(startTimeoutMs)
	s.classify(c)
	s.announce(c)
	s.refreshState()
	s.stopIfDiskFull()
}

// expireProvisional keeps the stream after a format change as the rest of
// the same item, which is never recorded as a fragment.
func (s *Session) expireProvisional(c *segment) {
	c.provisional = false
	c.ghost = true
}
