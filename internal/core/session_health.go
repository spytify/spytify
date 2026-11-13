package core

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"time"

	"github.com/spytify/spytify/internal/capture"
	"github.com/spytify/spytify/internal/meta"
	"github.com/spytify/spytify/internal/pcm"
	"github.com/spytify/spytify/pkg/proto"
)

// captureQuiet is how long the player may play with no sound from the
// capture before the session says so: no audio arriving, or, for a track,
// only silence since it started. One delay for both: the same player,
// muted, gives one or the other by system, Windows' loopback delivering
// zeros where macOS's tap can deliver nothing.
var captureQuiet = 10 * time.Second

// unmuteGrace is how long after an ad's mute is undone a volume of 0 may
// still be the mute's: the player is polled.
const unmuteGrace = time.Second

// diskFree reports the free space of a folder's disk; tests replace it.
var diskFree = freeBytes

// playerHeardCheck is the check that the capture did not hear only silence
// while the player played, until it heard sound again; silent is what was
// known then, nil while it hears sound. The window words its failure as
// the only silence warning (model.CheckDetail).
func playerHeardCheck(silent map[string]string) proto.Check {
	if silent != nil {
		return proto.Check{ID: proto.CheckPlayerHeard, Name: "player heard", Detail: "only silence from the capture while the player played", Facts: silent}
	}
	return proto.Check{ID: proto.CheckPlayerHeard, Name: "player heard", OK: true, Detail: "no silence heard"}
}

func (s *Session) onTick() {
	last := s.lastChunk
	if last.IsZero() {
		last = s.started
	}
	if s.status == meta.Playing && !s.stalled && !last.IsZero() && time.Since(last) > captureQuiet {
		s.stalled = true
		msg := fmt.Sprintf("no audio from the capture for %s while the player plays", captureQuiet)
		s.deps.log.Errorf("%s", msg)
		s.deps.emit(proto.EventError, proto.ErrorEvent{Code: proto.ErrorCaptureStalled, Msg: msg, Facts: s.stallFacts(last)})
	}
	// An ad may be silent, muted by the session or not. A player at 0% is
	// silent too, and its volume warning says so.
	if s.deps.watchSilence && !(s.volumeLow && s.volumePct == 0) && !s.trackHeard && s.status == meta.Playing && s.state() != proto.StateAd && !s.stalled && !s.silent && !s.lastChunk.IsZero() && time.Since(s.lastSound) > captureQuiet && time.Since(s.trackStarted) > captureQuiet {
		s.silent = true
		msg := fmt.Sprintf("only silence from the capture for %s while the player plays", captureQuiet)
		s.deps.log.Errorf("%s", msg)
		facts := s.silenceFacts()
		s.deps.emit(proto.EventError, proto.ErrorEvent{Code: proto.ErrorCaptureSilent, Msg: msg, Facts: facts})
		if s.deps.silence != nil {
			s.deps.silence(facts)
		}
	}
	if s.opts.IdleExit > 0 && s.seen && (s.status != meta.Playing) && time.Since(s.lastSound) > s.opts.IdleExit {
		s.deps.log.Infof("player idle for %s, ending the session", s.opts.IdleExit)
		s.endOfPlayback()
		s.finish()
	}
}

// stallFacts is what is known of a stalled capture, for the bug tracker:
// the session's side, since last, and the capture's own.
func (s *Session) stallFacts(last time.Time) map[string]string {
	facts := map[string]string{
		"stall.player_status":  string(s.status),
		"stall.since_chunk_ms": strconv.FormatInt(time.Since(last).Milliseconds(), 10),
		"stall.any_chunk":      yesNo(!s.lastChunk.IsZero()),
		"stall.session_age_s":  strconv.FormatInt(int64(time.Since(s.started).Seconds()), 10),
	}
	if s.deps.health != nil {
		maps.Copy(facts, s.deps.health())
	}
	return facts
}

// silenceFacts is what is known of a capture that hears only silence, for
// the bug tracker: the session's side, how long ago the computer woke, as
// outputs may not be back then, and the capture's own.
func (s *Session) silenceFacts() map[string]string {
	facts := map[string]string{
		"silent.player_status": string(s.status),
		"silent.any_sound":     yesNo(s.heard),
		"silent.track_age_s":   strconv.FormatInt(int64(time.Since(s.trackStarted).Seconds()), 10),
		"silent.session_age_s": strconv.FormatInt(int64(time.Since(s.started).Seconds()), 10),
		"silent.since_wake_s":  "none",
	}
	if s.volumeLow {
		facts["silent.player_volume_pct"] = strconv.Itoa(s.volumePct)
	}
	if s.deps.sinceWake != nil {
		if d, ok := s.deps.sinceWake(); ok {
			facts["silent.since_wake_s"] = strconv.FormatInt(int64(d.Seconds()), 10)
		}
	}
	if s.deps.health != nil {
		maps.Copy(facts, s.deps.health())
	}
	return facts
}

// captureHealth is what a capture says of itself (capture.Healther), nil
// for one that says nothing.
func captureHealth(c capture.Capture) func() map[string]string {
	if h, ok := c.(capture.Healther); ok {
		return h.Health
	}
	return nil
}

// onVolume follows the player's own volume. Under 100%, the take is
// quieter than the track: the session says so, again for each new value,
// and marks the take; back at 100%, the warning is withdrawn. The volume
// the session sets itself, muting an ad, is not the user's.
func (s *Session) onVolume(v float64) {
	if s.adMuted || (v == 0 && time.Since(s.unmutedAt) < unmuteGrace) {
		return
	}
	pct := int(math.Round(v * 100))
	if pct >= 100 {
		if s.volumeLow {
			s.volumeLow, s.volumePct = false, 0
			s.deps.log.Infof("the player's volume is back at 100%%")
			s.deps.emit(proto.EventError, proto.ErrorEvent{Code: proto.ErrorPlayerVolumeLow})
		}
		return
	}
	s.markLowVolume(s.cur)
	if s.volumeLow && pct == s.volumePct {
		return
	}
	s.volumeLow, s.volumePct = true, pct
	msg := fmt.Sprintf("the player's volume is at %d%%: tracks recorded now are quieter", pct)
	s.deps.log.Infof("%s", msg)
	s.deps.emit(proto.EventError, proto.ErrorEvent{Code: proto.ErrorPlayerVolumeLow, Msg: msg, Volume: &pct})
}

// markLowVolume marks a take recorded while the player's volume is under
// 100%; an ad, or audio the session does not record, has no file to mark.
func (s *Session) markLowVolume(c *segment) {
	if c == nil || c.ghost || c.provisional || c.reason == proto.SkipAd || c.lowVolume {
		return
	}
	c.lowVolume = true
	s.deps.log.Debugf("%q: recorded while the player's volume is under 100%%; the file is marked (low volume)", c.info.Title)
}

// hear notes sound from the capture, the session's first or the first
// after a silence was reported, which it withdraws.
func (s *Session) hear() {
	if s.heard && !s.silent {
		return
	}
	s.heard = true
	if s.silent {
		s.silent = false
		s.deps.log.Infof("sound from the capture again")
		s.deps.emit(proto.EventError, proto.ErrorEvent{Code: proto.ErrorCaptureSilent})
	}
	if s.deps.silence != nil {
		s.deps.silence(nil)
	}
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
		msg := fmt.Sprintf("the capture is %s: tracks will sound like a phone call", f)
		s.deps.log.Errorf("%s", msg)
		s.deps.emit(proto.EventError, proto.ErrorEvent{Code: proto.ErrorCaptureLowQuality, Msg: msg, Source: &proto.AudioSource{SampleRate: f.Rate, Channels: f.Channels}})
	case s.lowFormat:
		s.deps.log.Infof("the capture is %s, fit for music again", f)
		s.deps.emit(proto.EventError, proto.ErrorEvent{Code: proto.ErrorCaptureLowQuality})
	}
	s.lowFormat = low
}

func (s *Session) meter(peaks, rms []float64, frames int) {
	if s.count == 0 || len(peaks) != len(s.peak) {
		s.peak, s.sum, s.count = slices.Clone(peaks), make([]float64, len(peaks)), 0
	}
	for c := range peaks {
		s.peak[c] = max(s.peak[c], peaks[c])
		lin := pcmPow(rms[c])
		s.sum[c] += lin * lin * float64(frames)
	}
	s.count += int64(frames)
	if time.Since(s.lastLevel) < 100*time.Millisecond {
		return
	}
	// A stop that waits for the track's end still records it.
	if st := s.state(); (st == proto.StateRecording || st == proto.StateStopping) && s.count > 0 {
		e := proto.LevelEvent{PeakDBFS: pcm.Silence}
		var sum float64
		for c := range s.peak {
			e.Channels = append(e.Channels, proto.ChannelLevel{PeakDBFS: round1(s.peak[c]), RMSDBFS: round1(pcm.DBFS(sqrt(s.sum[c] / float64(s.count))))})
			e.PeakDBFS = max(e.PeakDBFS, round1(s.peak[c]))
			sum += s.sum[c]
		}
		e.RMSDBFS = round1(pcm.DBFS(sqrt(sum / float64(s.count*int64(len(s.peak))))))
		s.deps.emit(proto.EventLevel, e)
	}
	s.lastLevel = time.Now()
	s.count = 0
}

// stopIfDiskFull ends the session at once when the output disk is full:
// nothing more can be saved, and a graceful stop would wait for the end of
// a track the player, paused for the full disk, may never play.
func (s *Session) stopIfDiskFull() {
	if s.diskFull {
		s.handleStop(stopNow)
	}
}

func pcmPow(db float64) float64 {
	if db <= pcm.Silence {
		return 0
	}
	return pow10(db / 20)
}
