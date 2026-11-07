package core

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spytify/spytify/internal/meta"
	"github.com/spytify/spytify/internal/tag"
	"github.com/spytify/spytify/pkg/proto"
)

// A track whose position is under this when the session starts is
// recorded from its first sound; otherwise the session waits for the
// next one.
const freshStartMs = 300

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
		// Named once the player said its status, a track replaces the one
		// a stopped player showed.
		s.initial = nil
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
		s.initial = nil
		s.endOfPlayback()
	case meta.Seeked:
		s.deps.log.Debugf("%q: the player jumped to %d ms", ev.Track.Title, ev.PositionMs)
		s.onSeek(ev.PositionMs, at)
	case meta.VolumeChanged:
		s.onVolume(ev.Volume)
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
	// A change still pending starts this item: started over again before
	// it is cut, as a paused player's track bar dragged back to 0 twice,
	// the item still starts there, and no empty take comes between.
	if s.pend != nil && sameItem(s.pend.info, info) {
		s.pend.info = info
		return
	}
	// A change still pending that no sound followed never played, as the
	// first of two Next pressed while paused: it leaves no take, and this
	// change is planned from the take before it. Back to that take's own
	// item by way of another, the player starts it over.
	if s.pend != nil && at <= s.pend.at {
		s.pend = nil
		if c := s.cur; c != nil && sameItem(c.info, info) {
			restart = true
		}
	}
	if !restart {
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
	// Another track before the one paused at its start ever played: that
	// one leaves no take, and this one starts as the session's first.
	if c := s.cur; c != nil && !c.announced && !c.ghost {
		s.dropWriter(c)
		s.cur = nil
	}
	if s.pend != nil {
		s.tryResolve(true)
	}
	// Silence is watched from the start of each track: the quiet end of
	// the last one is not this one's.
	s.trackStarted, s.trackHeard = time.Now(), false
	s.pend = s.plan(s.ring, s.cur, info, at, restart, s.status == meta.Paused)
}

func (s *Session) onStatus(st meta.Status, positionMs, at int64, first bool) {
	s.status = st
	if s.initial != nil {
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
	if info == nil || info.Empty() {
		return
	}
	pos := s.initPos
	if st == meta.Stopped {
		// A stopped player plays the track it shows from its start, and
		// Play names no track: the track waits for Play, unless another
		// is named first.
		if pos <= freshStartMs {
			s.initial, s.initPos = info, 0
		}
		return
	}
	f := s.ring.Format()
	if (st == meta.Playing || st == meta.Paused) && pos >= 0 && pos <= freshStartMs {
		seg := s.newSegment(*info, at)
		seg.awaitStart = true
		seg.startDeadline = at + s.ms(startTimeoutMs)
		s.cur = seg
		// Paused at its start, the track is announced once it plays: a
		// track changed or a session ended before then leaves no take.
		if st == meta.Playing {
			s.announce(seg)
		}
		return
	}
	ghost := &segment{id: s.id + "-pre", info: *info, format: f, ghost: true, start: at, drained: at, soundAt: -1}
	if pos >= 0 && info.LengthMs > pos {
		ghost.length = f.FramesFromMs(info.LengthMs - pos)
	}
	s.cur = ghost
	s.deps.log.Infof("%q is already playing; recording starts with the next track", info.Title)
}
