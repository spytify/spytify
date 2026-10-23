package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spytify/spytify/internal/capture"
	"github.com/spytify/spytify/internal/meta"
	"github.com/spytify/spytify/internal/pcm"
	"github.com/spytify/spytify/internal/settings"
	"github.com/spytify/spytify/pkg/proto"
)

const (
	ringSeconds = 4.0
	// A notification may come this late after the audio changed; windows
	// and the drain hold-back cover it.
	lateNotifyMs = 1000
	// A notification this much earlier than the expected end is a skip or
	// a seek, not jitter.
	earlySlackMs  = 1000
	earlyNotifyMs = 300
	// A track whose position is under this when the session starts is
	// recorded from its first sound; otherwise the session waits for the
	// next one.
	freshStartMs = 300
	// After a format change, a track change within this window names the
	// new stream rather than cutting it.
	provisionalMs  = 2000
	startTimeoutMs = 2000
	frameMs        = 20
	stepMs         = 5
	blockMs        = 5
	// A window this many dB under the median of the search window is a
	// boundary even when it is not silent.
	dipDB = 15
)

type stopMode int

const (
	stopNone stopMode = iota
	stopGraceful
	stopNow
)

// SessionOptions changes how a session ends.
type SessionOptions struct {
	SingleTrack bool
	// EndOnStop ends the session when the player stops after having
	// played (spytifyd --once).
	EndOnStop bool
	// IdleExit ends the session after this long without audio while the
	// player is paused or stopped; 0 disables it.
	IdleExit time.Duration
}

type segment struct {
	id      string
	info    meta.TrackInfo
	format  pcm.Format
	start   int64
	length  int64
	drained int64
	w       *pcm.WAVWriter
	reason  proto.SkipReason
	// soundAt is the first frame above silence at or after start, or -1.
	// A player that leaves a gap between items starts the item there; a
	// player that streams items back to back starts it at start. The next
	// boundary is searched across both.
	soundAt int64
	// ghost is audio the session does not record: a track already playing
	// when the session started, or the rest of one cut by a format change.
	ghost bool
	// awaitStart holds the start until the first frame above silence.
	awaitStart    bool
	startDeadline int64
	provisional   bool
	announced     bool
	partial       bool
}

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
}

// sessionDeps is what a session needs from the core.
type sessionDeps struct {
	values   func() settings.Values
	emit     func(method string, payload any)
	log      *Logger
	fin      *finisher
	control  func(cmd string) error
	identify func(key string)
}

// Session turns a stream of frames and now-playing events into track files.
// One goroutine (Run) owns all of its state; Status and Stop are safe from
// any goroutine.
type Session struct {
	id   string
	deps sessionDeps
	opts SessionOptions

	ring      *Ring
	cur       *segment
	pend      *change
	status    meta.Status
	initial   *meta.TrackInfo
	initPos   int64
	gotStatus bool
	seen      bool
	dropQuiet bool
	stop      stopMode
	done      bool
	n         int
	tmpDir    string

	silenceDB float64
	window    int64
	frame     int64
	step      int64
	block     int64
	holdback  int64

	lastSound time.Time
	lastLevel time.Time
	peak, sum float64
	count     int64
	warnedLen bool

	stopCh chan stopMode
	mu     sync.Mutex
	snap   proto.SessionStatusResult
}

func newSession(id string, deps sessionDeps, opts SessionOptions) *Session {
	s := &Session{id: id, deps: deps, opts: opts, status: meta.Stopped, stopCh: make(chan stopMode, 4), lastSound: time.Now()}
	s.snap = proto.SessionStatusResult{State: proto.StateWaiting, SessionID: id}
	return s
}

// Status is a snapshot for session.status.
func (s *Session) Status() proto.SessionStatusResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.snap
	if s.snap.Track != nil {
		t := *s.snap.Track
		out.Track = &t
	}
	out.Counters = s.deps.fin.counters()
	return out
}

// Stop asks the session to end: now discards the current track, otherwise
// a track past recorder.min_seconds is allowed to finish.
func (s *Session) Stop(now bool) {
	if now {
		s.stopCh <- stopNow
	} else {
		s.stopCh <- stopGraceful
	}
}

func (s *Session) setState(st proto.SessionState) {
	s.mu.Lock()
	changed := s.snap.State != st
	s.snap.State = st
	s.mu.Unlock()
	if changed {
		s.deps.emit(proto.EventSessionState, proto.SessionStateEvent{State: st})
	}
}

func (s *Session) state() proto.SessionState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snap.State
}

// refreshState derives the state from what is playing.
func (s *Session) refreshState() {
	switch {
	case s.stop == stopGraceful:
		s.setState(proto.StateStopping)
	case s.status == meta.Paused:
		s.setState(proto.StatePaused)
	case s.cur == nil || s.cur.ghost:
		s.setState(proto.StateWaiting)
	case s.cur.reason == proto.SkipAd:
		s.setState(proto.StateAd)
	default:
		s.setState(proto.StateRecording)
	}
	s.mu.Lock()
	s.snap.Track = nil
	if s.cur != nil && !s.cur.ghost && !s.cur.provisional {
		t := trackOf(s.cur)
		s.snap.Track = &t
	}
	s.mu.Unlock()
}

func trackOf(seg *segment) proto.Track {
	return proto.Track{
		ID:       seg.id,
		Artist:   strings.Join(seg.info.Artists, ", "),
		Title:    seg.info.Title,
		Album:    seg.info.Album,
		LengthMs: seg.info.LengthMs,
		CoverURL: seg.info.ArtURL,
	}
}

func (s *Session) tune(f pcm.Format) {
	v := s.deps.values()
	s.silenceDB = v.Float("recorder.silence_db")
	s.window = f.FramesFromMs(v.Int("recorder.boundary_window_ms"))
	s.frame = f.FramesFromMs(frameMs)
	s.step = f.FramesFromMs(stepMs)
	s.block = f.FramesFromMs(blockMs)
	s.holdback = f.FramesFromMs(lateNotifyMs) + s.window
}

func (s *Session) ms(v int64) int64 { return s.ring.Format().FramesFromMs(v) }

// Run processes frames and events until the session ends, then waits for
// the last files to be finished.
func (s *Session) Run(ctx context.Context, frames <-chan capture.Chunk, events <-chan meta.Event) {
	v := s.deps.values()
	s.tmpDir = filepath.Join(v.Path("output.dir"), ".spytify")
	if err := os.MkdirAll(s.tmpDir, 0o755); err != nil {
		s.deps.emit(proto.EventError, proto.ErrorEvent{Code: "output_dir", Msg: err.Error(), Fatal: true})
		s.done = true
	}
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for !s.done {
		select {
		case <-ctx.Done():
			s.handleStop(stopNow)
		case mode := <-s.stopCh:
			s.handleStop(mode)
		case ch, ok := <-frames:
			if !ok {
				s.deps.log.Infof("capture stream ended")
				s.endOfPlayback()
				s.finish()
				continue
			}
			s.onChunk(ch)
		case ev, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			s.onMeta(ev)
		case <-tick.C:
			s.onTick()
		}
	}
	s.setState(proto.StateStopping)
	s.deps.fin.wait()
	s.setState(proto.StateIdle)
}

func (s *Session) onTick() {
	if s.opts.IdleExit > 0 && s.seen && (s.status != meta.Playing) && time.Since(s.lastSound) > s.opts.IdleExit {
		s.deps.log.Infof("player idle for %s, ending the session", s.opts.IdleExit)
		s.endOfPlayback()
		s.finish()
	}
}

func (s *Session) onChunk(ch capture.Chunk) {
	if len(ch.Data) == 0 || !ch.Format.Valid() {
		return
	}
	if s.ring == nil || ch.Format != s.ring.Format() {
		s.formatChange(ch.Format)
	}
	peak, rms := ch.Format.Level(ch.Data)
	if peak > s.silenceDB {
		s.lastSound = time.Now()
	}
	if s.dropQuiet && peak <= s.silenceDB {
		return
	}
	s.makeRoom(int64(len(ch.Data) / ch.Format.FrameBytes()))
	if lost := s.ring.Append(ch.Data); lost > 0 {
		s.deps.fin.addOverruns(lost)
		s.deps.emit(proto.EventError, proto.ErrorEvent{Code: "ring_overflow", Msg: fmt.Sprintf("%d frames lost", lost)})
		if s.cur != nil && !s.cur.ghost {
			s.cur.partial = true
		}
	}
	s.meter(peak, rms, len(ch.Data)/ch.Format.Sample.Bytes())
	s.progress()
}

func (s *Session) meter(peak, rms float64, samples int) {
	if peak > s.peak || s.count == 0 {
		s.peak = peak
	}
	lin := pcmPow(rms)
	s.sum += lin * lin * float64(samples)
	s.count += int64(samples)
	if time.Since(s.lastLevel) < 100*time.Millisecond {
		return
	}
	if st := s.state(); st == proto.StateRecording && s.count > 0 {
		s.deps.emit(proto.EventLevel, proto.LevelEvent{PeakDBFS: round1(s.peak), RMSDBFS: round1(pcm.DBFS(sqrt(s.sum / float64(s.count))))})
	}
	s.lastLevel = time.Now()
	s.peak, s.sum, s.count = pcm.Silence, 0, 0
}

// makeRoom keeps the ring from overflowing when no notification comes: the
// pending change is resolved with what is there, and a track that outlives
// its declared length loses that length.
func (s *Session) makeRoom(n int64) {
	if s.ring.Free() >= n {
		return
	}
	need := s.ring.Start() + n - s.ring.Free()
	if s.pend != nil && s.pend.lo < need {
		s.tryResolve(true)
	}
	if c := s.cur; c != nil && c.drained < need {
		if c.provisional {
			s.expireProvisional(c)
		}
		if c.awaitStart {
			c.awaitStart = false
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
		if e := c.start + c.length - s.window; limit > e {
			limit = e
		}
	}
	if s.pend != nil && limit > s.pend.lo {
		limit = s.pend.lo
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
			s.deps.emit(proto.EventError, proto.ErrorEvent{Code: "write", Msg: err.Error()})
			s.dropWriter(c)
			c.reason = proto.SkipIncomplete
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

func (s *Session) onMeta(ev meta.Event) {
	if s.ring == nil {
		s.formatChange(s.defaultFormat())
	}
	at := s.ring.End()
	switch ev.Kind {
	case meta.TrackChanged:
		if !s.gotStatus {
			info := ev.Track
			s.initial, s.initPos = &info, ev.PositionMs
			return
		}
		s.onTrack(ev.Track, at)
	case meta.StatusChanged:
		first := !s.gotStatus
		s.gotStatus = true
		s.onStatus(ev.Status, ev.PositionMs, at, first)
	case meta.PlayerGone:
		s.deps.log.Infof("player interface gone")
		s.endOfPlayback()
	}
	s.progress()
}

func (s *Session) defaultFormat() pcm.Format {
	return pcm.Format{Rate: 44100, Channels: 2, Sample: pcm.S16LE}
}

func sameItem(a, b meta.TrackInfo) bool {
	if a.ID != "" || b.ID != "" {
		return a.ID == b.ID
	}
	return a.Title == b.Title && strings.Join(a.Artists, ",") == strings.Join(b.Artists, ",")
}

func (s *Session) onTrack(info meta.TrackInfo, at int64) {
	if info.Empty() {
		s.endOfPlayback()
		return
	}
	if s.pend != nil && sameItem(s.pend.info, info) {
		s.pend.info = info
		return
	}
	if c := s.cur; c != nil && s.pend == nil && sameItem(c.info, info) {
		if c.provisional {
			s.adopt(c, info)
			return
		}
		if c.length == 0 && info.LengthMs > 0 && !s.warnedLen {
			c.length = c.format.FramesFromMs(info.LengthMs)
		}
		c.info = info
		s.refreshState()
		return
	}
	if c := s.cur; c != nil && c.provisional {
		s.adopt(c, info)
		return
	}
	if s.pend != nil {
		s.tryResolve(true)
	}
	s.pend = s.plan(info, at)
}

// plan places the search window for a track change noticed at sample at.
func (s *Session) plan(info meta.TrackInfo, at int64) *change {
	r := s.ring
	c := s.cur
	ch := &change{info: info, at: at}
	switch {
	case c == nil:
		ch.kind = changeFirst
		ch.lo = at - s.ms(lateNotifyMs)
		ch.expected = at
	case c.length > 0 && at >= c.start+c.length-s.window-s.ms(earlySlackMs):
		e := c.start + c.length
		late := s.soundAt(c) + c.length
		ch.kind, ch.lo, ch.hi, ch.expected = changeBoundary, e-s.window, late+s.window, e
	default:
		ch.kind, ch.lo, ch.hi, ch.expected = changeInterrupt, at-s.ms(lateNotifyMs), at+s.ms(earlyNotifyMs), at
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
	ch.ready = ch.hi + s.frame/2 + 1
	if ch.kind == changeFirst {
		ch.ready = ch.lo
	}
	return ch
}

// soundAt is where the segment's audio begins, looking into the ring when
// the drain has not reached it yet; start when there is no sound so far.
func (s *Session) soundAt(c *segment) int64 {
	if c.soundAt >= 0 {
		return c.soundAt
	}
	if at, ok := s.ring.FirstAbove(maxInt(c.start, s.ring.Start()), s.silenceDB, s.block); ok {
		return at
	}
	return c.start
}

func maxInt(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
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

// tryResolve cuts at the pending change once the audio it needs is in the
// ring, or right away when force is set.
func (s *Session) tryResolve(force bool) bool {
	p := s.pend
	r := s.ring
	if p == nil || (!force && r.End() < p.ready) {
		return false
	}
	var cut int64
	switch p.kind {
	case changeFirst:
		at, ok := r.FirstAbove(p.lo, s.silenceDB, s.block)
		if !ok {
			if !force && r.End() < p.at+s.ms(startTimeoutMs) {
				return false
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
		q, db := r.Quietest(p.lo, hi, s.frame, s.step)
		switch {
		case db <= s.silenceDB:
			cut = q
		case db <= r.MedianRMS(p.lo, hi, s.frame)-dipDB:
			// No silence, but a clear dip: two tracks faded into each
			// other.
			cut = q
		case p.kind == changeBoundary && s.cur != nil:
			// Gapless: trust the declared length, counted from the
			// track's first sound.
			cut = s.soundAt(s.cur) + s.cur.length
		default:
			cut = p.expected
		}
		if cut > r.End() {
			cut = r.End()
		}
	}
	if c := s.cur; c != nil && cut < c.drained {
		cut = c.drained
	}
	f := r.Format()
	s.deps.log.Debugf("cut for %q at %d ms (kind %d, noticed at %d ms, window %d..%d ms)",
		p.info.Title, f.MsFromFrames(cut), p.kind, f.MsFromFrames(p.at), f.MsFromFrames(p.lo), f.MsFromFrames(p.hi))
	s.pend = nil
	s.cutAt(cut, p.info, p.kind == changeFirst)
	return true
}

// cutAt ends the current segment at cut and starts the next one there.
func (s *Session) cutAt(cut int64, info meta.TrackInfo, first bool) {
	prev := s.cur
	afterAd := false
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
	if afterAd && !first {
		// The next track starts at its first sound, which keeps an
		// advert's tail out of it.
		seg.awaitStart = true
		seg.startDeadline = cut + s.ms(startTimeoutMs)
	}
	s.cur = seg
	s.announce(seg)
	s.refreshState()
}

func (s *Session) tryStart(c *segment, force bool) {
	at, ok := s.ring.FirstAbove(c.start, s.silenceDB, s.block)
	if !ok {
		if !force && s.ring.End() < c.startDeadline {
			return
		}
		at = c.start
	}
	if c.length > 0 {
		c.length -= at - c.start
		if c.length < 0 {
			c.length = 0
		}
	}
	s.deps.log.Debugf("%q starts at its first sound, %d ms", c.info.Title, s.ring.Format().MsFromFrames(at))
	c.start, c.drained, c.awaitStart, c.soundAt = at, at, false, -1
}

func (s *Session) newSegment(info meta.TrackInfo, start int64) *segment {
	s.n++
	f := s.ring.Format()
	seg := &segment{
		id:      fmt.Sprintf("%s-%03d", s.id, s.n),
		info:    info,
		format:  f,
		start:   start,
		drained: start,
		length:  f.FramesFromMs(info.LengthMs),
		soundAt: -1,
	}
	s.classify(seg)
	return seg
}

// classify decides whether a segment is recorded, and opens its temp file.
func (s *Session) classify(seg *segment) {
	v := s.deps.values()
	seg.reason = ""
	if !v.Bool("recorder.record_everything") {
		short := v.Int("recorder.skip_short_untitled")
		if seg.info.IsAdvert() || (short > 0 && seg.info.LengthMs > 0 && seg.info.LengthMs < short*1000 && seg.info.Album == "") {
			seg.reason = proto.SkipAd
		}
	}
	if seg.reason == "" && v.String("recorder.duplicates") == "skip" && s.deps.fin.recorded(seg.info, v) {
		seg.reason = proto.SkipDuplicate
		if v.Bool("recorder.skip_duplicates_forward") {
			go func() {
				if err := s.deps.control(proto.CmdNext); err != nil {
					s.deps.log.Infof("skip to next: %v", err)
				}
			}()
		}
	}
	if seg.reason == "" && !seg.ghost && !seg.provisional {
		if free, ok := freeBytes(v.Path("output.dir")); ok && free < minFreeBytes {
			s.deps.emit(proto.EventError, proto.ErrorEvent{Code: "disk_full", Msg: fmt.Sprintf("only %d MB free in the output folder", free>>20)})
			seg.reason = proto.SkipIncomplete
			if v.Bool("recorder.pause_on_disk_full") {
				go func() { _ = s.deps.control(proto.CmdPause) }()
			}
			s.stop = stopGraceful
			return
		}
		w, err := pcm.CreateWAV(filepath.Join(s.tmpDir, seg.id+".wav"), seg.format)
		if err != nil {
			s.deps.emit(proto.EventError, proto.ErrorEvent{Code: "temp_file", Msg: err.Error()})
			seg.reason = proto.SkipIncomplete
			return
		}
		seg.w = w
	}
}

func (s *Session) announce(seg *segment) {
	if seg.ghost || seg.provisional || seg.announced {
		return
	}
	seg.announced = true
	s.deps.emit(proto.EventTrackStarted, proto.TrackStartedEvent{Track: trackOf(seg)})
	switch seg.reason {
	case proto.SkipAd, proto.SkipDuplicate:
		s.deps.fin.skip(seg.id, seg.reason)
	case "":
		if s.deps.identify != nil {
			s.deps.identify(seg.id)
		}
	}
}

func (s *Session) closeSegment(c *segment, end int64) {
	if c.ghost || c.provisional {
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
	path := c.w.Path()
	if err := c.w.Close(); err != nil {
		s.deps.emit(proto.EventError, proto.ErrorEvent{Code: "write", Msg: err.Error()})
	}
	c.w = nil
	s.deps.fin.enqueue(finishJob{
		id:      c.id,
		info:    c.info,
		tmp:     path,
		format:  c.format,
		played:  played,
		length:  c.length,
		partial: c.partial,
	})
	if s.opts.SingleTrack {
		s.stop = stopGraceful
	}
}

func (s *Session) onStatus(st meta.Status, positionMs, at int64, first bool) {
	s.status = st
	if first {
		s.startFromInitial(st, at)
	}
	switch st {
	case meta.Playing:
		s.seen = true
		s.dropQuiet = false
		s.lastSound = time.Now()
	case meta.Paused:
		s.dropQuiet = true
		s.trimPause()
	case meta.Stopped:
		s.dropQuiet = false
		if !first {
			s.endOfPlayback()
		}
	}
	s.refreshState()
}

// startFromInitial handles what was already playing when the session began.
func (s *Session) startFromInitial(st meta.Status, at int64) {
	info := s.initial
	s.initial = nil
	if info == nil || info.Empty() || st == meta.Stopped {
		return
	}
	f := s.ring.Format()
	pos := s.initPos
	if st == meta.Playing && pos >= 0 && pos <= freshStartMs {
		seg := s.newSegment(*info, at)
		seg.awaitStart = true
		seg.startDeadline = at + s.ms(startTimeoutMs)
		s.cur = seg
		s.announce(seg)
		return
	}
	ghost := &segment{id: s.id + "-pre", info: *info, format: f, ghost: true, start: at, drained: at, soundAt: -1}
	if pos >= 0 && info.LengthMs > pos {
		ghost.length = f.FramesFromMs(info.LengthMs - pos)
	}
	s.cur = ghost
	s.deps.log.Infof("%q is already playing; recording starts with the next track", info.Title)
}

// trimPause removes the silence between the moment playback paused and the
// moment the player said so, so a pause leaves no hole in the file.
func (s *Session) trimPause() {
	r := s.ring
	if r == nil {
		return
	}
	floor := r.End() - s.ms(lateNotifyMs)
	if c := s.cur; c != nil {
		if c.drained > floor {
			floor = c.drained
		}
		if c.start > floor {
			floor = c.start
		}
	}
	if s.pend != nil && s.pend.lo > floor {
		floor = s.pend.lo
	}
	r.Truncate(r.SilenceStart(floor, s.silenceDB, s.block))
}

// endOfPlayback closes the current segment with everything captured: the
// player stopped, unloaded the item, or went away.
func (s *Session) endOfPlayback() {
	if s.ring == nil {
		return
	}
	if s.pend != nil {
		s.tryResolve(true)
	}
	if c := s.cur; c != nil {
		if c.provisional {
			s.expireProvisional(c)
		}
		end := s.ring.End()
		late := s.soundAt(c) + c.length
		if c.length > 0 && late+s.window < end {
			// Cut at the declared end so trailing audio of whatever comes
			// next is not appended.
			if q, db := s.ring.Quietest(c.start+c.length-s.window, late+s.window, s.frame, s.step); db <= s.silenceDB {
				end = q
			} else {
				end = c.start + c.length
			}
			if end < c.drained {
				end = c.drained
			}
		}
		s.write(c, c.drained, end)
		s.closeSegment(c, end)
		s.cur = nil
	}
	if (s.opts.EndOnStop && s.seen) || s.stop == stopGraceful {
		s.finish()
	}
	s.refreshState()
}

func (s *Session) handleStop(mode stopMode) {
	if s.done {
		return
	}
	c := s.cur
	if mode == stopNow || c == nil || c.ghost || c.reason != "" || c.provisional {
		if c != nil && !c.ghost && c.reason == "" && c.w != nil {
			s.dropWriter(c)
			if c.announced {
				s.deps.fin.skip(c.id, proto.SkipIncomplete)
			}
		}
		s.cur = nil
		s.pend = nil
		s.finish()
		return
	}
	minFrames := c.format.FramesFromMs(s.deps.values().Int("recorder.min_seconds") * 1000)
	if s.ring.End()-c.start < minFrames {
		s.dropWriter(c)
		s.deps.fin.skip(c.id, proto.SkipTooShort)
		s.cur = nil
		s.finish()
		return
	}
	s.stop = stopGraceful
	s.refreshState()
}

func (s *Session) finish() {
	if s.done {
		return
	}
	if c := s.cur; c != nil && !c.ghost && c.w != nil {
		s.write(c, c.drained, s.ring.End())
		s.closeSegment(c, s.ring.End())
	}
	s.cur = nil
	s.done = true
}

// formatChange makes sure a stream recreated in another format never lands
// inside one file.
func (s *Session) formatChange(f pcm.Format) {
	old := s.ring
	s.ring = NewRing(f, ringSeconds)
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
		c.format, c.start, c.drained, c.soundAt = f, 0, 0, -1
		c.length = f.FramesFromMs(c.info.LengthMs)
		c.awaitStart, c.startDeadline = true, s.ms(startTimeoutMs)
		if c.reason == "" {
			if w, err := pcm.CreateWAV(filepath.Join(s.tmpDir, c.id+".wav"), f); err == nil {
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
}

// expireProvisional keeps the stream after a format change as the rest of
// the same item, which is never recorded as a fragment.
func (s *Session) expireProvisional(c *segment) {
	c.provisional = false
	c.ghost = true
}

func pcmPow(db float64) float64 {
	if db <= pcm.Silence {
		return 0
	}
	return pow10(db / 20)
}
