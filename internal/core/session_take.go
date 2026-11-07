package core

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/spytify/spytify/internal/capture"
	"github.com/spytify/spytify/internal/meta"
	"github.com/spytify/spytify/internal/pcm"
	"github.com/spytify/spytify/internal/settings"
	"github.com/spytify/spytify/pkg/proto"
)

func (s *Session) onChunk(ch capture.Chunk) {
	if len(ch.Data) == 0 || !ch.Format.Valid() {
		return
	}
	s.lastChunk = time.Now()
	if s.stalled {
		s.stalled = false
		s.deps.log.Infof("audio from the capture again")
	}
	if s.ring == nil || ch.Format != s.ring.Format() {
		s.checkQuality(ch.Format)
	}
	switch {
	case s.ring == nil:
		s.formatChange(ch.Format)
	case ch.Format != s.ring.Format() && s.ring.End() == 0:
		// Metadata arrived before any audio and the ring guessed the
		// format; no frame has been captured, so nothing changes format.
		s.reformat(ch.Format)
	case ch.Format != s.ring.Format():
		s.formatChange(ch.Format)
	}
	peaks, rms := ch.Format.ChannelLevels(ch.Data)
	if slices.Max(peaks) > s.silenceDB {
		s.lastSound = time.Now()
		s.trackHeard = true
		s.hear()
	}
	data := ch.Data
	if s.pausing {
		if data = s.excise(ch.Format, data, s.ring, s.pauseFloor, s.silenceDB); len(data) == 0 {
			s.progress()
			return
		}
	}
	s.makeRoom(int64(len(data) / ch.Format.FrameBytes()))
	if lost := s.ring.Append(data); lost > 0 {
		s.deps.fin.addOverruns(lost)
		s.deps.emit(proto.EventError, proto.ErrorEvent{Code: proto.ErrorRingOverflow, Msg: fmt.Sprintf("%d frames lost", lost)})
		if s.cur != nil && !s.cur.ghost {
			s.cur.partial = true
		}
	}
	s.checkSeam()
	s.meter(peaks, rms, len(ch.Data)/ch.Format.FrameBytes())
	s.progress()
}

// makeRoom keeps the ring from overflowing when no notification comes: the
// pending change is resolved with what is there, and a track that outlives
// its declared length loses that length.
func (s *Session) makeRoom(n int64) {
	if s.ring.Free() >= n {
		return
	}
	need := s.ring.Start() + n - s.ring.Free()
	if s.pend != nil && s.pend.earliest() < need {
		s.tryResolve(true)
	}
	if c := s.cur; c != nil && c.drained < need {
		if c.provisional {
			s.expireProvisional(c)
		}
		if c.awaitStart {
			c.awaitStart = false
			s.announce(c)
		}
		if c.length > 0 && need > c.start+c.length-s.window {
			if !s.warnedLen {
				s.deps.log.Infof("no track change by the declared end of %q; recording on until the next one", c.info.Title)
				s.warnedLen = true
			}
			c.length = 0
		}
		s.write(c, c.drained, need)
	}
	s.ring.Discard(need)
}

func (s *Session) progress() {
	if s.pend != nil {
		s.tryResolve(false)
	}
	if c := s.cur; c != nil {
		if c.provisional && s.ring.End()-c.start > s.ms(provisionalMs) {
			s.expireProvisional(c)
		}
		if c.awaitStart {
			s.tryStart(c, false)
		}
	}
	s.drain()
}

func (s *Session) drain() {
	r := s.ring
	c := s.cur
	if c == nil {
		r.Discard(r.End() - s.holdback)
		return
	}
	limit := r.End() - s.holdback
	if c.awaitStart || c.provisional {
		limit = c.start
	}
	if c.length > 0 {
		if e := c.origin + c.length - s.window; limit > e {
			limit = e
		}
	}
	if s.pend != nil && limit > s.pend.earliest() {
		limit = s.pend.earliest()
	}
	if limit > c.drained {
		s.write(c, c.drained, limit)
	}
	keep := c.drained
	if c.awaitStart || c.provisional {
		keep = c.start
	}
	r.Discard(keep)
}

func (s *Session) write(c *segment, from, to int64) {
	if from < c.start {
		from = c.start
	}
	if to <= from {
		if to > c.drained {
			c.drained = to
		}
		return
	}
	data := s.ring.Bytes(from, to)
	if c.soundAt < 0 {
		if at, ok := firstAbove(s.ring.Format(), data, s.silenceDB, s.block); ok {
			c.soundAt = from + at
		}
	}
	if c.w != nil {
		if _, err := c.w.Write(data); err != nil {
			s.dropWriter(c)
			c.reason = proto.SkipIncomplete
			if !s.outputLost(err) {
				s.deps.emit(proto.EventError, proto.ErrorEvent{Code: proto.ErrorWrite, Msg: err.Error()})
			}
		}
	}
	c.drained = to
}

func (s *Session) dropWriter(c *segment) {
	if c.w != nil {
		path := c.w.Path()
		_ = c.w.Close()
		_ = os.Remove(path)
		c.w = nil
	}
}

// tryResolve cuts at the pending change once the audio it needs is in the
// ring, or right away when force is set.
func (s *Session) tryResolve(force bool) bool {
	p := s.pend
	r := s.ring
	if p == nil {
		return false
	}
	cut, by, ok := s.resolve(r, p, s.cur, force)
	if !ok {
		return false
	}
	f := r.Format()
	s.deps.log.Debugf("cut for %q at %d ms (kind %d, noticed at %d ms, window %d..%d ms)",
		p.info.Title, f.MsFromFrames(cut), p.kind, f.MsFromFrames(p.at), f.MsFromFrames(p.lo), f.MsFromFrames(p.hi))
	s.pend = nil
	how := &proto.TrackCut{Kind: p.kind.String(), By: by, OffsetMs: f.MsFromFrames(cut) - f.MsFromFrames(p.at)}
	s.cutAt(cut, p.info, p.kind == changeFirst, p.restart, how)
	return true
}

// cutAt ends the current segment at cut and starts the next one there.
func (s *Session) cutAt(cut int64, info meta.TrackInfo, first, restart bool, how *proto.TrackCut) {
	prev := s.cur
	afterAd := false
	if prev != nil && restart {
		prev.partial = true
	}
	if prev != nil {
		s.write(prev, prev.drained, cut)
		s.closeSegment(prev, cut)
		afterAd = prev.reason == proto.SkipAd
	}
	s.cur = nil
	if s.stop == stopGraceful || s.done {
		s.finish()
		return
	}
	seg := s.newSegment(info, cut)
	seg.cut = how
	if afterAd && !first {
		// The next track starts at its first sound, which keeps an
		// advert's tail out of it.
		seg.awaitStart = true
		seg.startDeadline = cut + s.ms(startTimeoutMs)
	}
	s.cur = seg
	s.announce(seg)
	s.refreshState()
	s.stopIfDiskFull()
}

func (s *Session) tryStart(c *segment, force bool) {
	at, ok := s.firstSound(s.ring, c.start)
	if !ok {
		if !force && s.ring.End() < c.startDeadline {
			return
		}
		at = c.start
	}
	s.deps.log.Debugf("%q starts at its first sound, %d ms", c.info.Title, s.ring.Format().MsFromFrames(at))
	c.start, c.drained, c.awaitStart, c.soundAt = at, at, false, -1
	s.announce(c)
}

func (s *Session) newSegment(info meta.TrackInfo, start int64) *segment {
	s.n++
	f := s.ring.Format()
	seg := &segment{
		id:      fmt.Sprintf("%s-%03d", s.id, s.n),
		info:    info,
		format:  f,
		start:   start,
		origin:  start,
		drained: start,
		length:  f.FramesFromMs(info.LengthMs),
		soundAt: -1,
		// Recorded from a volume still under 100%, the take is quieter.
		lowVolume: s.volumeLow,
	}
	s.classify(seg)
	return seg
}

// classify decides whether a segment is recorded, and opens its temp file.
func (s *Session) classify(seg *segment) {
	v := s.deps.values()
	seg.reason, seg.recorded = "", recordedFile{}
	short := v.Int(settings.RecorderSkipShortUntitled)
	if seg.info.IsAdvert() || (short > 0 && seg.info.LengthMs > 0 && seg.info.LengthMs < short*1000 && seg.info.Album == "") {
		seg.reason = proto.SkipAd
	}
	if seg.reason == "" && v.Duplicates() == settings.DuplicatesSkip {
		if f, ok := s.deps.fin.find(seg.info, v); ok {
			seg.reason, seg.recorded = proto.SkipDuplicate, f
		}
	}
	if seg.reason == "" && !seg.ghost && !seg.provisional {
		if free, ok := diskFree(v.Path(settings.OutputDir)); ok && free < minFreeBytes {
			mb := int64(free / 1e6)
			s.deps.emit(proto.EventError, proto.ErrorEvent{Code: proto.ErrorDiskFull, Msg: fmt.Sprintf("only %d MB free in the output folder", mb), FreeMB: &mb})
			seg.reason = proto.SkipIncomplete
			if v.Bool(settings.RecorderPauseOnDiskFull) {
				go func() { _ = s.deps.control(proto.CmdPause) }()
			}
			s.diskFull = true
			return
		}
		w, err := s.tempWAV(seg.id, seg.format)
		if err != nil {
			seg.reason = proto.SkipIncomplete
			if !s.outputLost(err) {
				s.deps.emit(proto.EventError, proto.ErrorEvent{Code: proto.ErrorTempFile, Msg: err.Error()})
			}
			return
		}
		seg.w = w
	}
}

// tempWAV creates a track's temp file. A temp folder deleted while the
// session runs, by hand with the files around it, is made again.
func (s *Session) tempWAV(id string, f pcm.Format) (*pcm.WAVWriter, error) {
	path := filepath.Join(s.tmpDir, id+".wav")
	w, err := pcm.CreateWAV(path, f)
	if errors.Is(err, fs.ErrNotExist) && os.MkdirAll(s.tmpDir, 0o755) == nil {
		return pcm.CreateWAV(path, f)
	}
	return w, err
}

// outputLost tells, once a temp file could not be created or written to,
// whether the output folder went with it: its drive unplugged or its share
// gone. Nothing more can be recorded then, this track included, and the
// recording stops at once. The folder is only looked at after such an
// error.
func (s *Session) outputLost(err error) bool {
	if _, gone := os.Stat(s.tmpDir); gone == nil {
		return false
	}
	s.deps.emit(proto.EventError, proto.ErrorEvent{Code: proto.ErrorOutputDir, Msg: "the output folder cannot be reached, so the recording stopped: " + err.Error()})
	s.Stop(true)
	return true
}

func (s *Session) announce(seg *segment) {
	if seg.ghost || seg.provisional || seg.announced {
		return
	}
	seg.announced = true
	ev := proto.TrackStartedEvent{Track: trackOf(seg), Cut: seg.cut}
	if seg.format.Valid() {
		ev.Source = &proto.AudioSource{SampleRate: seg.format.Rate, Channels: seg.format.Channels}
	}
	s.deps.emit(proto.EventTrackStarted, ev)
	switch seg.reason {
	case proto.SkipAd, proto.SkipSeeked:
		s.deps.fin.skip(seg.id, seg.reason)
	case proto.SkipDuplicate:
		s.deps.fin.skipped(proto.TrackSkippedEvent{ID: seg.id, Reason: seg.reason, Path: seg.recorded.path, Of: seg.recorded.of})
	}
	// A duplicate is skipped once announced: one paused at its start waits
	// for Play, as next would make a paused player play the next track.
	if seg.reason == proto.SkipDuplicate && s.deps.values().Bool(settings.RecorderSkipDuplicatesForward) {
		go func() {
			if err := s.deps.control(proto.CmdNext); err != nil {
				s.deps.log.Infof("skip to next: %v", err)
			}
		}()
	}
}

func (s *Session) closeSegment(c *segment, end int64) {
	if c.ghost || c.provisional {
		return
	}
	// The window was never told of the take: it never played.
	if !c.announced {
		s.dropWriter(c)
		return
	}
	played := end - c.start
	if c.awaitStart || played < 0 {
		played = 0
	}
	if c.w == nil {
		if c.reason == proto.SkipIncomplete && c.announced {
			s.deps.fin.skip(c.id, c.reason)
		}
		return
	}
	// Cut at its own start, as when the player names the next track at
	// once, the take holds nothing: it is skipped, with nothing to keep.
	if played == 0 {
		s.dropWriter(c)
		if c.reason == "" {
			s.deps.fin.skip(c.id, proto.SkipIncomplete)
		}
		return
	}
	path := c.w.Path()
	if err := c.w.Close(); err != nil {
		s.deps.emit(proto.EventError, proto.ErrorEvent{Code: proto.ErrorWrite, Msg: err.Error()})
	}
	c.w = nil
	info := c.info
	if p := s.covers[info.ArtURL]; p != "" {
		info.ArtURL = coverURL(p)
	}
	s.deps.fin.enqueue(finishJob{
		id:        c.id,
		info:      info,
		tmp:       path,
		format:    c.format,
		played:    played,
		length:    c.length,
		partial:   c.partial,
		rejected:  c.reason,
		pauseFade: c.pauseFade,
		lowVolume: c.lowVolume,
	})
	if s.opts.SingleTrack {
		s.stop = stopGraceful
	}
}

// pauseFloor is the earliest frame a pause may still cut out of the ring.
func (s *Session) pauseFloor() int64 {
	r := s.ring
	floor := r.End() - s.ms(lateNotifyMs)
	if c := s.cur; c != nil {
		floor = maxInt(floor, maxInt(c.drained, c.start))
	}
	if p := s.pend; p != nil {
		floor = maxInt(floor, p.lo)
		if p.kind != changeFirst {
			// A frame of the silence a pause runs into is left at the
			// start of the search: it was the gap between the two
			// tracks, and the search finds the cut in it.
			floor = maxInt(floor, p.lo+s.frame)
		}
	}
	return maxInt(floor, r.Start())
}

// trimPause handles a Paused notification that came after the audio
// stopped: the silence already captured is cut back to its first frame.
func (s *Session) trimPause() {
	if s.ring == nil {
		return
	}
	s.trim(s.ring, s.pauseFloor(), s.silenceDB)
}

// checkSeam looks at the last pause cut out, once the ring holds
// seamSideMs past it. If the player's fades meet there, a dip that is
// heard, the take is saved marked " (pause fade)"; a player that does not
// fade leaves no mark. A cut outside the take, or during a track change,
// is let go.
func (s *Session) checkSeam() {
	at := s.seam
	if at == 0 || s.ring.End() < at+s.ms(seamSideMs) {
		return
	}
	s.seam = 0
	c := s.cur
	limit := s.deps.values().Float(settings.RecorderPauseFadeDb)
	if limit <= 0 || c == nil || c.ghost || c.provisional || c.awaitStart || c.reason != "" || s.pend != nil || at-s.ms(seamSideMs) < c.start {
		return
	}
	dip, faded := fadedSeam(s.ring, at, limit)
	if !faded {
		s.deps.log.Debugf("%q: a pause was cut out at %d ms, %.0f dB under the music", c.info.Title, s.ring.Format().MsFromFrames(at), dip)
		return
	}
	s.deps.log.Infof("%q: the player faded out and in around a pause, %.0f dB under the music where it was cut; the file is marked (pause fade)", c.info.Title, dip)
	c.pauseFade = true
}

// reject ends c before its end: it is not saved, but the audio it has is
// held for track.keep. A take nobody saw start is only dropped.
func (s *Session) reject(c *segment) {
	if c.w == nil {
		// No file to keep: a track whose file could not be opened is
		// still said to be skipped.
		s.closeSegment(c, s.ring.End())
		return
	}
	if !c.announced {
		s.dropWriter(c)
		return
	}
	if c.reason == "" {
		c.reason = proto.SkipIncomplete
		s.deps.fin.skip(c.id, c.reason)
	}
	s.write(c, c.drained, s.ring.End())
	s.closeSegment(c, s.ring.End())
}
