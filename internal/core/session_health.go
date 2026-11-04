package core

import (
	"fmt"
	"slices"
	"time"

	"github.com/spytify/spytify/internal/meta"
	"github.com/spytify/spytify/internal/pcm"
	"github.com/spytify/spytify/pkg/proto"
)

// captureStall is how long the player may play with no audio arriving
// before the session says so.
var captureStall = 10 * time.Second

// captureSilent is how long the player may play with only silence arriving
// before the session says so.
var captureSilent = 10 * time.Second

// diskFree reports the free space of a folder's disk; tests replace it.
var diskFree = freeBytes

// playerHeardCheck is the check that the capture did not hear only silence
// while the player played, until it heard sound again. The window words
// its failure as the only silence warning (model.CheckDetail).
func playerHeardCheck(silent bool) proto.Check {
	if silent {
		return proto.Check{Name: proto.CheckPlayerHeard, Detail: "only silence from the capture while the player played"}
	}
	return proto.Check{Name: proto.CheckPlayerHeard, OK: true, Detail: "no silence heard"}
}

func (s *Session) onTick() {
	last := s.lastChunk
	if last.IsZero() {
		last = s.started
	}
	if s.status == meta.Playing && !s.stalled && !last.IsZero() && time.Since(last) > captureStall {
		s.stalled = true
		msg := fmt.Sprintf("no audio from the capture for %s while the player plays", captureStall)
		s.deps.log.Errorf("%s", msg)
		s.deps.emit(proto.EventError, proto.ErrorEvent{Code: "capture_stalled", Msg: msg})
	}
	// An ad may be silent, muted by the session or not.
	if s.deps.watchSilence && s.status == meta.Playing && s.state() != proto.StateAd && !s.stalled && !s.silent && !s.lastChunk.IsZero() && time.Since(s.lastSound) > captureSilent {
		s.silent = true
		msg := fmt.Sprintf("only silence from the capture for %s while the player plays", captureSilent)
		s.deps.log.Errorf("%s", msg)
		s.deps.emit(proto.EventError, proto.ErrorEvent{Code: "capture_silent", Msg: msg})
		if s.deps.silence != nil {
			s.deps.silence(true)
		}
	}
	if s.opts.IdleExit > 0 && s.seen && (s.status != meta.Playing) && time.Since(s.lastSound) > s.opts.IdleExit {
		s.deps.log.Infof("player idle for %s, ending the session", s.opts.IdleExit)
		s.endOfPlayback()
		s.finish()
	}
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
		s.deps.emit(proto.EventError, proto.ErrorEvent{Code: "capture_silent"})
	}
	if s.deps.silence != nil {
		s.deps.silence(false)
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
		s.deps.emit(proto.EventError, proto.ErrorEvent{Code: "capture_low_quality", Msg: msg, Source: &proto.AudioSource{SampleRate: f.Rate, Channels: f.Channels}})
	case s.lowFormat:
		s.deps.log.Infof("the capture is %s, fit for music again", f)
		s.deps.emit(proto.EventError, proto.ErrorEvent{Code: "capture_low_quality"})
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
