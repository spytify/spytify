package core

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/spytify/spytify/internal/capture"
	"github.com/spytify/spytify/internal/meta"
	"github.com/spytify/spytify/internal/pcm"
	"github.com/spytify/spytify/internal/settings"
	"github.com/spytify/spytify/internal/tag"
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
	provisionalMs  = 3000
	startTimeoutMs = 2000
	frameMs        = 20
	stepMs         = 5
	blockMs        = 5
	// A window this many dB under the median of the search window is a
	// boundary even when it is not silent.
	dipDB = 15
	// Silence this long while paused is the pause, not a quiet note.
	pauseRunMs = 20
	// After a skip, the next track starts where the peak level of 1 ms
	// windows drops this far under its median.
	skipDipDB = 20
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
	// EndOnStop ends the session when the player stops or quits after
	// having played (spytifyd --once). recorder.stop_with_player does the
	// same; it is read then, so turning it on mid-session applies.
	EndOnStop bool
	// IdleExit ends the session after this long without audio while the
	// player is paused or stopped; 0 disables it.
	IdleExit time.Duration
}

type segment struct {
	id     string
	info   meta.TrackInfo
	format pcm.Format
	start  int64
	// origin is where the item may have begun: the cut. A take that waits
	// for its first sound moves start there, so the file skips an advert's
	// tail, but the item's own quiet beginning counts in its length: its end
	// is searched from origin + length to first sound + length.
	origin  int64
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
	// cut is how the take's start was found, for track.started.
	cut *proto.TrackCut
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

// sessionDeps is what a session needs from the core.
type sessionDeps struct {
	values  func() settings.Values
	emit    func(method string, payload any)
	log     *Logger
	fin     *finisher
	control func(cmd string) error
	// mute silences the player (true) or gives it back its volume.
	mute func(on bool) error
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
	// While paused, every run of at least pauseRunMs of silent frames is cut
	// out at the sample, so a pause leaves neither a hole nor a seam.
	pausing  bool
	excising bool
	resumed  bool
	quietRun int64
	stop     stopMode
	done     bool
	n        int
	tmpDir   string
	// covers maps a cover URL the watcher published to the session's own
	// copy of it in tmpDir.
	covers map[string]string

	silenceDB float64
	window    int64
	frame     int64
	step      int64
	block     int64
	holdback  int64

	lastSound time.Time
	lastLevel time.Time
	// started and lastChunk find a capture that delivers nothing while
	// the player plays; stalled is set once that was reported.
	started   time.Time
	lastChunk time.Time
	stalled   bool
	// lowFormat is set while the capture's format is too poor for music.
	lowFormat bool
	peak, sum float64
	count     int64
	// diskFull is set when the output disk had no room for a track.
	diskFull  bool
	warnedLen bool
	adMuted   bool

	stopCh chan stopMode
	mu     sync.Mutex
	snap   proto.SessionStatusResult
}

func newSession(id string, deps sessionDeps, opts SessionOptions) *Session {
	s := &Session{id: id, deps: deps, opts: opts, status: meta.Stopped, stopCh: make(chan stopMode, 4), lastSound: time.Now()}
	s.snap = proto.SessionStatusResult{State: proto.StateIdle, SessionID: id}
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

// Stop asks the session to end: now ends the current track where it is,
// otherwise the current track is allowed to finish.
func (s *Session) Stop(now bool) {
	mode := stopGraceful
	if now {
		mode = stopNow
	}
	select {
	case s.stopCh <- mode:
	default:
		// Enough stop requests are queued; the session is ending.
	}
}

func (s *Session) setState(st proto.SessionState) {
	s.mu.Lock()
	changed := s.snap.State != st
	s.snap.State = st
	s.mu.Unlock()
	if changed {
		s.deps.emit(proto.EventSessionState, proto.SessionStateEvent{State: st})
		s.muteAds(st)
	}
}

// muteAds silences the player while an ad plays (recorder.mute_ads), and
// gives the volume back when anything else plays or the session ends.
func (s *Session) muteAds(st proto.SessionState) {
	if s.deps.mute == nil {
		return
	}
	want := st == proto.StateAd && s.deps.values().Bool(settings.RecorderMuteAds)
	if want == s.adMuted {
		return
	}
	s.adMuted = want
	if err := s.deps.mute(want); err != nil {
		s.deps.log.Infof("mute ads: %v", err)
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
	s.silenceDB = v.Float(settings.RecorderSilenceDb)
	s.window = f.FramesFromMs(v.Int(settings.RecorderBoundaryWindowMs))
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
	s.tmpDir = filepath.Join(v.Path(settings.OutputDir), ".spytify")
	if err := os.MkdirAll(s.tmpDir, 0o755); err != nil {
		s.deps.emit(proto.EventError, proto.ErrorEvent{Code: "output_dir", Msg: err.Error(), Fatal: true})
		s.done = true
	}
	// Clients learn that a session runs from this first transition.
	s.setState(proto.StateWaiting)
	s.started = time.Now()
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
			s.onMeta(ctx, ev)
		case <-tick.C:
			s.onTick()
		}
	}
	s.setState(proto.StateStopping)
	s.deps.fin.wait()
	for _, p := range s.covers {
		_ = os.Remove(p)
	}
	s.covers = nil
	s.setState(proto.StateIdle)
}

// captureStall is how long the player may play with no audio arriving
// before the session says so.
var captureStall = 10 * time.Second

// diskFree reports the free space of a folder's disk; tests replace it.
var diskFree = freeBytes

// stallHint is what to look at when no audio arrives.
func stallHint(goos string) string {
	switch goos {
	case "darwin":
		return "No audio from the player for 10 s. If macOS asks to let Spytify record system audio, allow it; it is in System Settings > Privacy & Security > Screen & System Audio Recording."
	case "windows":
		return "No audio from the player for 10 s. Check that the Windows Audio service runs and that the player plays on an output device."
	}
	return "No audio from the player for 10 s. Check that PipeWire runs: systemctl --user restart pipewire."
}

// lowQualityHint is what to change when the capture is too poor for
// music, as from a Bluetooth headset in a call.
func lowQualityHint(goos string, f pcm.Format) string {
	what := fmt.Sprintf("%g kHz", float64(f.Rate)/1000)
	if f.Channels < 2 {
		what += " mono"
	}
	lead := "The player's output device runs at " + what + ", not fit for recording: tracks will sound like a phone call. "
	switch goos {
	case "darwin":
		return lead + "A Bluetooth headset switches to call mode when an app opens its microphone. In System Settings > Sound > Input, choose another microphone, such as the built-in one, or play to another output."
	case "windows":
		return lead + "A Bluetooth headset does this while an app uses its microphone. Choose another output in Settings > System > Sound, or end what uses the headset's microphone."
	}
	return lead + "A Bluetooth headset does this while an app uses its microphone. Choose another output device, or end what uses the headset's microphone."
}

func (s *Session) onTick() {
	last := s.lastChunk
	if last.IsZero() {
		last = s.started
	}
	if s.status == meta.Playing && !s.stalled && !last.IsZero() && time.Since(last) > captureStall {
		s.stalled = true
		s.deps.log.Errorf("no audio from the capture for %s while the player plays", captureStall)
		s.deps.emit(proto.EventError, proto.ErrorEvent{Code: "capture_stalled", Msg: stallHint(runtime.GOOS)})
	}
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
	peak, rms := ch.Format.Level(ch.Data)
	if peak > s.silenceDB {
		s.lastSound = time.Now()
	}
	data := ch.Data
	if s.pausing {
		if data = s.excisePause(ch.Format, data); len(data) == 0 {
			s.progress()
			return
		}
	}
	s.makeRoom(int64(len(data) / ch.Format.FrameBytes()))
	if lost := s.ring.Append(data); lost > 0 {
		s.deps.fin.addOverruns(lost)
		s.deps.emit(proto.EventError, proto.ErrorEvent{Code: "ring_overflow", Msg: fmt.Sprintf("%d frames lost", lost)})
		if s.cur != nil && !s.cur.ghost {
			s.cur.partial = true
		}
	}
	s.meter(peak, rms, len(ch.Data)/ch.Format.Sample.Bytes())
	s.progress()
}

// poorFormat is a format too poor for music, under 44.1 kHz or in mono:
// what is recorded in it sounds like a phone call.
func poorFormat(f pcm.Format) bool {
	return f.Rate < 44100 || f.Channels < 2
}

// checkQuality runs on the capture's first format and on each change: a
// format too poor for music is reported each time, and the report is
// withdrawn, the same code with no message, once the format is fit again.
func (s *Session) checkQuality(f pcm.Format) {
	low := poorFormat(f)
	switch {
	case low:
		s.deps.log.Errorf("the capture is %s: tracks will sound like a phone call", f)
		s.deps.emit(proto.EventError, proto.ErrorEvent{Code: "capture_low_quality", Msg: lowQualityHint(runtime.GOOS, f)})
	case s.lowFormat:
		s.deps.log.Infof("the capture is %s, fit for music again", f)
		s.deps.emit(proto.EventError, proto.ErrorEvent{Code: "capture_low_quality"})
	}
	s.lowFormat = low
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
		if e := c.origin + c.length - s.window; limit > e {
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
			s.dropWriter(c)
			c.reason = proto.SkipIncomplete
			if !s.outputLost(err) {
				s.deps.emit(proto.EventError, proto.ErrorEvent{Code: "write", Msg: err.Error()})
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

func (s *Session) onMeta(ctx context.Context, ev meta.Event) {
	if s.ring == nil {
		s.formatChange(s.defaultFormat())
	}
	at := s.ring.End()
	switch ev.Kind {
	case meta.TrackChanged:
		s.claimCover(ctx, ev.Track.ArtURL)
		if !s.gotStatus {
			info := ev.Track
			s.initial, s.initPos = &info, ev.PositionMs
			return
		}
		if ev.Restart {
			s.deps.log.Debugf("%q started over: the player is at %d ms", ev.Track.Title, ev.PositionMs)
		}
		s.onTrack(ev.Track, at, ev.Restart)
	case meta.StatusChanged:
		first := !s.gotStatus
		s.gotStatus = true
		s.onStatus(ev.Status, ev.PositionMs, at, first)
	case meta.PlayerGone:
		s.deps.log.Infof("player interface gone")
		// A player that is gone plays nothing, whatever it said last.
		s.status = meta.Stopped
		s.endOfPlayback()
	case meta.Seeked:
		s.deps.log.Debugf("%q: the player jumped to %d ms", ev.Track.Title, ev.PositionMs)
		s.onSeek(ev.PositionMs, at)
	}
	s.progress()
}

// onSeek follows a jump in the current item. The take no longer matches the
// published track, so it is dropped; the item's end moves with the position
// so that the next track is still cut at its own start.
func (s *Session) onSeek(posMs, at int64) {
	c := s.cur
	if c == nil || posMs < 0 {
		return
	}
	if c.length > 0 && c.info.LengthMs > 0 {
		played := at - c.start
		if played < 0 {
			played = 0
		}
		c.length = played + c.format.FramesFromMs(max(0, c.info.LengthMs-posMs))
	}
	if c.ghost || c.provisional || c.reason != "" {
		return
	}
	// The take goes on to its end all the same: it is not saved, but its
	// audio is held for track.keep.
	s.deps.log.Infof("%q: playback jumped to %d ms, this take is not saved", c.info.Title, posMs)
	c.reason = proto.SkipSeeked
	if c.announced {
		s.deps.fin.skip(c.id, c.reason)
	}
}

func (s *Session) defaultFormat() pcm.Format {
	return pcm.Format{Rate: 44100, Channels: 2, Sample: pcm.S16LE}
}

// claimCover copies a cover the watcher published as a file into the
// session's temp dir. On Windows the poller replaces its temporary cover
// file when the next track's art arrives — before the finisher reads it
// at the track's end — so a file cover must be claimed while it exists.
// The track's ArtURL keeps naming the watcher's file for everyone else;
// the copy's URL replaces it only in the track info given to the job.
func (s *Session) claimCover(ctx context.Context, u string) {
	if !strings.HasPrefix(u, "file://") || s.tmpDir == "" {
		return
	}
	if s.covers == nil {
		s.covers = map[string]string{}
	}
	if _, ok := s.covers[u]; ok {
		return
	}
	data, mime, err := tag.FetchCover(ctx, nil, u)
	if err != nil {
		return
	}
	ext := ".jpg"
	if mime == "image/png" {
		ext = ".png"
	}
	dst := filepath.Join(s.tmpDir, fmt.Sprintf("cover-%d%s", len(s.covers), ext))
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		return
	}
	s.covers[u] = dst
}

// coverURL is the file URL of a local path: file:///C:/x on Windows.
func coverURL(p string) string {
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(p)}
	if !strings.HasPrefix(u.Path, "/") {
		u.Path = "/" + u.Path
	}
	return u.String()
}

func sameItem(a, b meta.TrackInfo) bool {
	if a.ID != "" || b.ID != "" {
		return a.ID == b.ID
	}
	return a.Title == b.Title && strings.Join(a.Artists, ",") == strings.Join(b.Artists, ",")
}

// onTrack handles a TrackChanged. The same item again is a metadata
// refresh, unless restart says it started over: then it is a new track,
// cut like a skip.
func (s *Session) onTrack(info meta.TrackInfo, at int64, restart bool) {
	if info.Empty() {
		s.endOfPlayback()
		return
	}
	if !restart {
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
	}
	if c := s.cur; c != nil && c.provisional {
		s.adopt(c, info)
		return
	}
	if s.pend != nil {
		s.tryResolve(true)
	}
	s.pend = s.plan(info, at, restart)
}

// plan places the search window for a track change noticed at sample at.
func (s *Session) plan(info meta.TrackInfo, at int64, restart bool) *change {
	r := s.ring
	c := s.cur
	ch := &change{info: info, at: at, restart: restart}
	switch {
	case c == nil:
		ch.kind = changeFirst
		ch.lo = at - s.ms(lateNotifyMs)
		ch.expected = at
	case c.length > 0 && at >= c.origin+c.length-s.window-s.ms(earlySlackMs):
		// At its end, the same item again is a new take (repeat), and the
		// one before it is complete.
		ch.restart = false
		e := c.origin + c.length
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
	var by string
	switch p.kind {
	case changeFirst:
		at, ok := s.firstSound(p.lo)
		by = "first_sound"
		if !ok {
			by = "notice"
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
		refine := func(c int64) int64 { return r.RefineCut(c, s.frame, f1ms(r)) }
		var edge int64
		edgeOK := false
		if p.kind == changeInterrupt {
			at, peak, median := r.QuietestPeak(p.lo, hi, f1ms(r))
			edge, edgeOK = at, peak <= median-skipDipDB
		}
		switch {
		case db <= s.silenceDB:
			cut, by = refine(q), "silence"
		case db <= r.MedianRMS(p.lo, hi, s.frame)-dipDB:
			// No silence, but a clear dip: two tracks faded into each
			// other.
			cut, by = refine(q), "dip"
		case edgeOK:
			// A skip cuts a track mid-note; the next one starts where the
			// peak level drops for a millisecond, at its first sample.
			cut, by = edge, "edge"
		case p.kind == changeBoundary && s.cur != nil:
			// Gapless: trust the declared length, counted from the
			// track's first sound.
			cut, by = s.soundAt(s.cur)+s.cur.length, "length"
		default:
			cut, by = p.expected, "notice"
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
	how := &proto.TrackCut{Kind: p.kind.String(), By: by, OffsetMs: f.MsFromFrames(cut) - f.MsFromFrames(p.at)}
	s.cutAt(cut, p.info, p.kind == changeFirst, p.restart, how)
	return true
}

func f1ms(r *Ring) int64 { return r.Format().FramesFromMs(1) }

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

// stopIfDiskFull ends the session at once when the output disk is full:
// nothing more can be saved, and a graceful stop would wait for the end of
// a track the player, paused for the full disk, may never play.
func (s *Session) stopIfDiskFull() {
	if s.diskFull {
		s.handleStop(stopNow)
	}
}

// firstSound is where a track starts when it starts at its first sound:
// the first block above silence_db, moved back over the quiet beginning of
// its fade-in, no earlier than from.
func (s *Session) firstSound(from int64) (int64, bool) {
	at, ok := s.ring.FirstAbove(from, s.silenceDB, s.block)
	if !ok {
		return 0, false
	}
	f := s.ring.Format()
	return s.ring.ExtendBack(at, s.silenceDB-12, f.FramesFromMs(1), f.FramesFromMs(100), from), true
}

func (s *Session) tryStart(c *segment, force bool) {
	at, ok := s.firstSound(c.start)
	if !ok {
		if !force && s.ring.End() < c.startDeadline {
			return
		}
		at = c.start
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
		origin:  start,
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
	short := v.Int(settings.RecorderSkipShortUntitled)
	if seg.info.IsAdvert() || (short > 0 && seg.info.LengthMs > 0 && seg.info.LengthMs < short*1000 && seg.info.Album == "") {
		seg.reason = proto.SkipAd
	}
	if seg.reason == "" && v.String(settings.RecorderDuplicates) == "skip" && s.deps.fin.recorded(seg.info, v) {
		seg.reason = proto.SkipDuplicate
		if v.Bool(settings.RecorderSkipDuplicatesForward) {
			go func() {
				if err := s.deps.control(proto.CmdNext); err != nil {
					s.deps.log.Infof("skip to next: %v", err)
				}
			}()
		}
	}
	if seg.reason == "" && !seg.ghost && !seg.provisional {
		if free, ok := diskFree(v.Path(settings.OutputDir)); ok && free < minFreeBytes {
			s.deps.emit(proto.EventError, proto.ErrorEvent{Code: "disk_full", Msg: fmt.Sprintf("only %d MB free in the output folder", free>>20)})
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
				s.deps.emit(proto.EventError, proto.ErrorEvent{Code: "temp_file", Msg: err.Error()})
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
	s.deps.emit(proto.EventError, proto.ErrorEvent{Code: "output_dir", Msg: "the output folder cannot be reached, so the recording stopped: " + err.Error()})
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
	case proto.SkipAd, proto.SkipDuplicate, proto.SkipSeeked:
		s.deps.fin.skip(seg.id, seg.reason)
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
	info := c.info
	if p := s.covers[info.ArtURL]; p != "" {
		info.ArtURL = coverURL(p)
	}
	s.deps.fin.enqueue(finishJob{
		id:       c.id,
		info:     info,
		tmp:      path,
		format:   c.format,
		played:   played,
		length:   c.length,
		partial:  c.partial,
		rejected: c.reason,
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
		s.lastSound = time.Now()
		if s.pausing {
			// The audio may already be flowing, or start a moment later;
			// in that case the silence before it is still cut.
			s.resumed = true
			if !s.excising {
				s.pausing = false
			}
		}
	case meta.Paused:
		s.pausing, s.resumed = true, false
		s.trimPause()
	case meta.Stopped:
		s.pausing, s.excising = false, false
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

// pauseFloor is the earliest frame a pause may still cut out of the ring.
func (s *Session) pauseFloor() int64 {
	r := s.ring
	floor := r.End() - s.ms(lateNotifyMs)
	if c := s.cur; c != nil {
		floor = maxInt(floor, maxInt(c.drained, c.start))
	}
	if s.pend != nil {
		floor = maxInt(floor, s.pend.lo)
	}
	return maxInt(floor, r.Start())
}

func quietFrame(f pcm.Format, data []byte, i int, thr float64) bool {
	for c := 0; c < f.Channels; c++ {
		if v := f.SampleAt(data, i*f.Channels+c); v > thr || v < -thr {
			return false
		}
	}
	return true
}

// trimPause handles a Paused notification that came after the audio
// stopped: the silence already captured is cut back to its first frame.
func (s *Session) trimPause() {
	r := s.ring
	if r == nil {
		return
	}
	f := r.Format()
	floor := s.pauseFloor()
	tail := r.Bytes(floor, r.End())
	thr := pow10(s.silenceDB / 20)
	n := len(tail) / f.FrameBytes()
	run := 0
	for i := n - 1; i >= 0 && quietFrame(f, tail, i, thr); i-- {
		run++
	}
	s.quietRun, s.excising = int64(run), false
	if int64(run) >= f.FramesFromMs(pauseRunMs) {
		r.Truncate(r.End() - int64(run))
		s.quietRun, s.excising = 0, true
	}
}

// excisePause filters one chunk while paused: frames of a silent run of at
// least pauseRunMs are dropped, including the start of the run that earlier
// chunks left in the ring; everything else is kept.
func (s *Session) excisePause(f pcm.Format, data []byte) []byte {
	fb := f.FrameBytes()
	n := len(data) / fb
	thr := pow10(s.silenceDB / 20)
	minRun := f.FramesFromMs(pauseRunMs)
	out := make([]byte, 0, len(data))
	for i := 0; i < n; i++ {
		quiet := quietFrame(f, data, i, thr)
		if s.excising {
			if quiet {
				continue
			}
			s.excising = false
			if s.resumed {
				s.pausing, s.quietRun = false, 0
				return append(out, data[i*fb:]...)
			}
		}
		out = append(out, data[i*fb:(i+1)*fb]...)
		if !quiet {
			s.quietRun = 0
			continue
		}
		s.quietRun++
		if s.quietRun >= minRun {
			inChunk := s.quietRun
			if max := int64(len(out) / fb); inChunk > max {
				inChunk = max
			}
			out = out[:len(out)-int(inChunk)*fb]
			if earlier := s.quietRun - inChunk; earlier > 0 {
				s.ring.Truncate(maxInt(s.ring.End()-earlier, s.pauseFloor()))
			}
			s.quietRun, s.excising = 0, true
		}
	}
	return out
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
			if q, db := s.ring.Quietest(c.origin+c.length-s.window, late+s.window, s.frame, s.step); db <= s.silenceDB {
				end = q
			} else {
				end = c.origin + c.length
			}
			if end < c.drained {
				end = c.drained
			}
		}
		s.write(c, c.drained, end)
		s.closeSegment(c, end)
		s.cur = nil
	}
	if (s.seen && (s.opts.EndOnStop || s.deps.values().Bool("recorder.stop_with_player"))) || s.stop == stopGraceful {
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
		if c != nil && !c.ghost && !c.provisional {
			s.reject(c)
		}
		s.cur = nil
		s.pend = nil
		s.finish()
		return
	}
	s.stop = stopGraceful
	s.refreshState()
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
		c.format, c.start, c.origin, c.drained, c.soundAt = f, 0, 0, 0, -1
		c.length = f.FramesFromMs(c.info.LengthMs)
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

func pcmPow(db float64) float64 {
	if db <= pcm.Silence {
		return 0
	}
	return pow10(db / 20)
}
