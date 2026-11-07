package core

import (
	"context"
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

const ringSeconds = 4.0

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
	// recorded is where the file of a track skipped as already recorded
	// is.
	recorded recordedFile
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
	// pauseFade is a take a pause was cut out of, where the player's
	// fades meet: its file is marked " (pause fade)".
	pauseFade bool
	// lowVolume is a take recorded while the player's own volume was
	// under 100%: its file is marked " (low volume)".
	lowVolume bool
	// cut is how the take's start was found, for track.started.
	cut *proto.TrackCut
}

// sessionDeps is what a session needs from the core.
type sessionDeps struct {
	values  func() settings.Values
	emit    func(method string, payload any)
	log     *Logger
	fin     *finisher
	control func(cmd proto.PlayerCmd) error
	// mute silences the player (true) or gives it back its volume.
	mute func(on bool) error
	// watchSilence makes the session warn of a capture that delivers only
	// silence while the player plays; tests leave it off.
	watchSilence bool
	// silence tells that the capture hears only silence while the player
	// plays (true), or sound (false).
	silence func(on bool)
	// playlist is the session's playlist file as it ends, "" for none.
	playlist func() string
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
	// The pause excision's flags are set here as the player's status
	// changes; it cuts the pauses out of the stream.
	pauseExcision
	stop   stopMode
	done   bool
	n      int
	tmpDir string
	// covers maps a cover URL the watcher published to the session's own
	// copy of it in tmpDir.
	covers map[string]string

	// The cut finder's sizes are set by tune; the drain and the end of
	// playback use them too.
	cutFinder

	lastSound time.Time
	lastLevel time.Time
	// started and lastChunk find a capture that delivers nothing while
	// the player plays; stalled is set once that was reported.
	started   time.Time
	lastChunk time.Time
	stalled   bool
	// silent is set once a capture that delivers only silence while the
	// player plays was reported; heard once the capture brought sound.
	silent, heard bool
	// trackStarted is when the player's track started, or started over,
	// and trackHeard is set once it brought sound. Only a track silent
	// from its start is reported: the quiet end of a song had sound
	// before it.
	trackStarted time.Time
	trackHeard   bool
	// lowFormat is set while the capture's format is too poor for music.
	lowFormat bool
	// peak and sum are each channel's since the last level reported, over
	// count frames.
	peak, sum []float64
	count     int64
	// diskFull is set when the output disk had no room for a track.
	diskFull  bool
	warnedLen bool
	adMuted   bool
	// unmutedAt is when the session last gave the player its volume back
	// after an ad.
	unmutedAt time.Time
	// volumeLow is set while the player's own volume is reported under
	// 100%, at volumePct.
	volumeLow bool
	volumePct int

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
	prev := s.snap.State
	changed := prev != st
	s.snap.State = st
	s.mu.Unlock()
	if changed {
		if prev == proto.StateAd {
			// The player's sound is due from now, not from before the ad.
			s.lastSound = time.Now()
		}
		ev := proto.SessionStateEvent{State: st}
		if st == proto.StateIdle && s.deps.playlist != nil {
			ev.Playlist = s.deps.playlist()
		}
		s.deps.emit(proto.EventSessionState, ev)
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
	if !want {
		s.unmutedAt = time.Now()
	}
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
		s.deps.emit(proto.EventError, proto.ErrorEvent{Code: proto.ErrorOutputDir, Msg: err.Error(), Fatal: true})
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
		late := s.soundAt(s.ring, c) + c.length
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
	if (s.seen && (s.opts.EndOnStop || s.deps.values().Bool(settings.RecorderStopWithPlayer))) || s.stop == stopGraceful {
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
