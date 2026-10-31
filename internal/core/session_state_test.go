package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spytify/spytify/internal/meta"
	"github.com/spytify/spytify/pkg/proto"
)

// stateScript is one walk through the session's states.
type stateScript struct {
	opts     SessionOptions
	settings map[string]any
	run      func(r *rig)
}

var (
	trackA = info("/x/a", "A", 3000)
	trackB = info("/x/b", "B", 3000)
	advert = meta.TrackInfo{ID: "/x/ad/1", Title: "Sponsored", Artists: []string{"Sponsor"}, LengthMs: 3000}
)

// playA starts a session on A, from its first sound, 1 s into it.
func playA(r *rig) {
	r.status(meta.Playing)
	r.track(trackA, 0)
	r.quiet(500)
	r.sound(1, 1000)
}

// thenB ends A and plays B to the end of the session.
func thenB(r *rig) {
	r.sound(1, 2000)
	r.quiet(500)
	r.track(trackB, 0)
	r.sound(2, 3000)
	r.quiet(1500)
	r.status(meta.Stopped)
}

var endOnStop = SessionOptions{EndOnStop: true}

// Every state the session reports, every way into it and out of it, and a
// stop and a stop now in each.
func TestPinnedStates(t *testing.T) {
	scripts := map[string]stateScript{
		"stop-while-waiting": {endOnStop, nil, func(r *rig) {
			r.stop(false)
		}},
		"stop-now-while-waiting": {endOnStop, nil, func(r *rig) {
			r.stop(true)
		}},
		"stop-while-waiting-for-the-next-track": {endOnStop, nil, func(r *rig) {
			r.track(trackA, 2500)
			r.status(meta.Playing)
			r.sound(1, 300)
			r.stop(false)
		}},
		"stop-while-awaiting-the-first-sound": {endOnStop, nil, func(r *rig) {
			r.track(trackA, 100)
			r.status(meta.Playing)
			r.quiet(300)
			r.stop(false)
			r.quiet(300)
			r.sound(1, 3000)
			r.quiet(500)
			r.track(trackB, 0)
			r.sound(2, 1000)
		}},
		// Today's behavior, which looks odd: the take is held for
		// track.keep with nothing played but the silence it waited in.
		"stop-now-while-awaiting-the-first-sound": {endOnStop, nil, func(r *rig) {
			r.track(trackA, 100)
			r.status(meta.Playing)
			r.quiet(300)
			r.stop(true)
		}},
		"stop-while-recording": {endOnStop, nil, func(r *rig) {
			playA(r)
			r.stop(false)
			thenB(r)
		}},
		"stop-now-while-recording": {endOnStop, nil, func(r *rig) {
			playA(r)
			r.stop(true)
		}},
		"stop-twice-then-now": {endOnStop, nil, func(r *rig) {
			playA(r)
			r.stop(false)
			r.sound(1, 500)
			r.stop(false)
			r.sound(1, 500)
			r.stop(true)
		}},
		"stop-then-the-player-stops": {endOnStop, nil, func(r *rig) {
			playA(r)
			r.stop(false)
			r.sound(1, 500)
			r.status(meta.Stopped)
		}},
		"stop-then-the-player-stops-without-end-on-stop": {SessionOptions{}, nil, func(r *rig) {
			playA(r)
			r.stop(false)
			r.sound(1, 500)
			r.status(meta.Stopped)
		}},
		"stop-while-paused": {endOnStop, nil, func(r *rig) {
			playA(r)
			r.status(meta.Paused)
			r.quiet(300)
			r.stop(false)
			r.quiet(300)
			r.status(meta.Playing)
			thenB(r)
		}},
		"stop-now-while-paused": {endOnStop, nil, func(r *rig) {
			playA(r)
			r.status(meta.Paused)
			r.quiet(300)
			r.stop(true)
		}},
		"stop-during-an-ad": {endOnStop, nil, func(r *rig) {
			playA(r)
			r.sound(1, 2000)
			r.quiet(500)
			r.track(advert, 0)
			r.sound(9, 1000)
			r.stop(false)
		}},
		"stop-now-during-an-ad": {endOnStop, nil, func(r *rig) {
			playA(r)
			r.sound(1, 2000)
			r.quiet(500)
			r.track(advert, 0)
			r.sound(9, 1000)
			r.stop(true)
		}},
		// Today's behavior, which looks odd: the duplicate, recorded in
		// name only, reports the recording state.
		"stop-on-a-duplicate": {endOnStop, nil, func(r *rig) {
			r.alreadyRecorded("Artist", "B")
			playA(r)
			r.sound(1, 2000)
			r.quiet(500)
			r.track(trackB, 0)
			r.sound(2, 1000)
			r.stop(false)
		}},
		"stop-after-a-seek": {endOnStop, nil, func(r *rig) {
			playA(r)
			r.send(meta.Event{Kind: meta.Seeked, Track: trackA, PositionMs: 2500})
			r.sound(1, 300)
			r.stop(false)
		}},
		"context-canceled-while-recording": {endOnStop, nil, func(r *rig) {
			playA(r)
			r.cancelRun()
		}},
		"capture-ends-while-recording": {endOnStop, nil, func(r *rig) {
			playA(r)
			r.endCapture()
		}},
		"player-events-end-while-recording": {endOnStop, nil, func(r *rig) {
			playA(r)
			r.endEvents()
			r.sound(1, 2500)
			r.quiet(500)
		}},
		"session-starts-mid-track": {endOnStop, nil, func(r *rig) {
			r.track(trackA, 2500)
			r.status(meta.Playing)
			r.sound(1, 500)
			r.quiet(500)
			r.track(trackB, 0)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		}},
		// Today's behavior, which looks odd: A's track.started says no cut.
		"session-starts-at-300ms": {endOnStop, nil, func(r *rig) {
			r.track(trackA, 300)
			r.status(meta.Playing)
			r.sound(1, 2700)
			r.quiet(500)
			r.track(trackB, 0)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		}},
		"session-starts-at-301ms": {endOnStop, nil, func(r *rig) {
			r.track(trackA, 301)
			r.status(meta.Playing)
			r.sound(1, 2700)
			r.quiet(500)
			r.track(trackB, 0)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		}},
		"session-starts-with-no-position": {endOnStop, nil, func(r *rig) {
			r.track(trackA, -1)
			r.status(meta.Playing)
			r.sound(1, 3000)
			r.quiet(500)
			r.track(trackB, 0)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		}},
		// Today's behavior, which looks odd: Play names no track, so A is
		// not recorded, and B, named with no take open, starts at the
		// first sound in the second before its notice: A's last 500 ms.
		"session-starts-on-a-stopped-player-with-a-track": {endOnStop, nil, func(r *rig) {
			r.track(trackA, 0)
			r.status(meta.Stopped)
			r.quiet(300)
			r.status(meta.Playing)
			r.sound(1, 3000)
			r.quiet(500)
			r.track(trackB, 0)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		}},
		"status-before-any-track": {endOnStop, nil, func(r *rig) {
			r.status(meta.Playing)
			r.quiet(300)
			r.track(trackA, 0)
			r.quiet(200)
			r.sound(1, 3000)
			r.quiet(500)
			r.status(meta.Stopped)
		}},
		"player-stops-before-it-played": {endOnStop, nil, func(r *rig) {
			r.status(meta.Stopped)
			r.quiet(500)
			r.status(meta.Playing)
			r.track(trackA, 0)
			r.quiet(500)
			r.sound(1, 3000)
			r.quiet(500)
			r.status(meta.Stopped)
		}},
		// EndOnStop waits for the player to have played: a pause then a
		// stop, or the player going away, before that do not end it.
		"player-pauses-and-stops-before-it-played": {endOnStop, nil, func(r *rig) {
			r.status(meta.Paused)
			r.quiet(300)
			r.status(meta.Stopped)
			r.quiet(300)
			r.send(meta.Event{Kind: meta.PlayerGone, PositionMs: -1})
			r.quiet(300)
			r.status(meta.Playing)
			r.track(trackA, 0)
			r.sound(1, 3000)
			r.quiet(500)
			r.status(meta.Stopped)
		}},
		"player-stops-and-plays-again": {SessionOptions{}, nil, func(r *rig) {
			playA(r)
			r.sound(1, 2000)
			r.quiet(500)
			r.status(meta.Stopped)
			r.track(meta.TrackInfo{}, -1)
			r.quiet(1000)
			r.status(meta.Playing)
			r.track(trackB, 0)
			r.quiet(200)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		}},
		"player-quits-and-comes-back": {SessionOptions{}, nil, func(r *rig) {
			playA(r)
			r.sound(1, 1000)
			r.send(meta.Event{Kind: meta.PlayerGone, PositionMs: -1})
			r.quiet(1000)
			r.send(meta.Event{Kind: meta.PlayerAppeared, PositionMs: -1})
			r.track(trackB, 0)
			r.status(meta.Playing)
			r.quiet(200)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		}},
		// recorder.stop_with_player does what EndOnStop does: a pause does
		// not end the session, the player stopping or quitting does.
		"player-pauses-and-stops-with-stop-with-player": {SessionOptions{}, map[string]any{"recorder.stop_with_player": true}, func(r *rig) {
			playA(r)
			r.status(meta.Paused)
			r.quiet(500)
			r.status(meta.Playing)
			thenB(r)
		}},
		"player-quits-with-stop-with-player": {SessionOptions{}, map[string]any{"recorder.stop_with_player": true}, func(r *rig) {
			playA(r)
			r.sound(1, 1000)
			r.send(meta.Event{Kind: meta.PlayerGone, PositionMs: -1})
			r.quiet(500)
		}},
		"player-quits-with-end-on-stop": {endOnStop, nil, func(r *rig) {
			playA(r)
			r.sound(1, 1000)
			r.send(meta.Event{Kind: meta.PlayerGone, PositionMs: -1})
			r.quiet(500)
		}},
		// Today's behavior, which looks odd: B, named with no take open,
		// starts at the first sound in the second before its notice, which
		// is A's; its end is counted from there, so it loses its last
		// second.
		"track-unloaded-mid-track": {SessionOptions{}, nil, func(r *rig) {
			playA(r)
			r.track(meta.TrackInfo{}, -1)
			r.quiet(500)
			r.track(trackB, 0)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		}},
		"skip-mid-track": {endOnStop, nil, func(r *rig) {
			playA(r)
			r.track(trackB, 0)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		}},
		"restart-mid-track": {endOnStop, nil, func(r *rig) {
			playA(r)
			r.send(meta.Event{Kind: meta.TrackChanged, Track: trackA, PositionMs: 0, Restart: true})
			r.rewind(1)
			r.sound(1, 3000)
			r.quiet(500)
			r.track(trackB, 0)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		}},
		"restart-at-the-end": {endOnStop, nil, func(r *rig) {
			playA(r)
			r.sound(1, 2000)
			r.send(meta.Event{Kind: meta.TrackChanged, Track: trackA, PositionMs: 0, Restart: true})
			r.rewind(1)
			r.sound(1, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		}},
		"metadata-refreshed": {endOnStop, nil, func(r *rig) {
			r.status(meta.Playing)
			r.track(info("/x/a", "A", 0), 0)
			r.quiet(500)
			r.sound(1, 1000)
			refreshed := trackA
			refreshed.Album = "Album (Deluxe)"
			r.track(refreshed, -1)
			r.sound(1, 2000)
			r.quiet(500)
			r.track(trackB, 0)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		}},
		"same-title-with-no-id": {endOnStop, nil, func(r *rig) {
			a := trackA
			a.ID = ""
			r.status(meta.Playing)
			r.track(a, 0)
			r.quiet(500)
			r.sound(1, 1000)
			a.LengthMs = 3100
			r.track(a, -1)
			r.sound(1, 2000)
			r.quiet(500)
			b := trackB
			b.ID = ""
			r.track(b, 0)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		}},
		"single-track": {SessionOptions{EndOnStop: true, SingleTrack: true}, nil, func(r *rig) {
			playA(r)
			thenB(r)
		}},
		"ad-between-tracks": {endOnStop, map[string]any{"recorder.mute_ads": true}, func(r *rig) {
			playA(r)
			r.sound(1, 2000)
			r.quiet(300)
			r.track(advert, 0)
			r.sound(9, 3000)
			r.track(trackB, 0)
			r.sound(9, 200)
			r.quiet(300)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		}},
		"short-item-with-no-album-is-an-ad": {endOnStop, nil, func(r *rig) {
			playA(r)
			r.sound(1, 2000)
			r.quiet(300)
			r.track(meta.TrackInfo{ID: "/x/clip", Title: "Clip", Artists: []string{"Someone"}, LengthMs: 3000}, 0)
			r.sound(3, 3000)
			r.quiet(300)
			r.track(trackB, 0)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		}},
		"duplicate-skipped": {endOnStop, map[string]any{"recorder.duplicates": "skip"}, func(r *rig) {
			playA(r)
			r.sound(1, 2000)
			r.quiet(500)
			r.track(info("/x/a2", "A", 3000), 0)
			r.rewind(1)
			r.sound(1, 3000)
			r.quiet(500)
			r.track(trackB, 0)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		}},
		"duplicate-kept-with-a-suffix": {endOnStop, map[string]any{"recorder.duplicates": "suffix"}, func(r *rig) {
			playA(r)
			r.sound(1, 2000)
			r.quiet(500)
			r.track(info("/x/a2", "A", 3000), 0)
			r.rewind(1)
			r.sound(1, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		}},
		"seek-forward-and-back": {endOnStop, nil, func(r *rig) {
			playA(r)
			r.send(meta.Event{Kind: meta.Seeked, Track: trackA, PositionMs: 2000})
			r.sound(1, 1000)
			r.send(meta.Event{Kind: meta.Seeked, Track: trackA, PositionMs: 500})
			r.sound(1, 2500)
			r.quiet(500)
			r.track(trackB, 0)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		}},
		"seek-with-no-position-or-no-track": {endOnStop, nil, func(r *rig) {
			r.send(meta.Event{Kind: meta.Seeked, Track: trackA, PositionMs: 1000})
			playA(r)
			r.send(meta.Event{Kind: meta.Seeked, Track: trackA, PositionMs: -1})
			thenB(r)
		}},
		"seek-while-waiting-for-the-next-track": {endOnStop, nil, func(r *rig) {
			r.track(trackA, 2000)
			r.status(meta.Playing)
			r.sound(1, 300)
			r.send(meta.Event{Kind: meta.Seeked, Track: trackA, PositionMs: 500})
			r.sound(1, 2500)
			r.quiet(500)
			r.track(trackB, 0)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		}},
	}
	for _, name := range sortedKeys(scripts) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			sc := scripts[name]
			r := newRig(t, sc.settings, sc.opts)
			r.describe("A and B are 3000 ms each")
			sc.run(r)
			r.check("states/" + name)
		})
	}
}

// The ends of a session that never recorded.
func TestPinnedSessionsThatRecordNothing(t *testing.T) {
	t.Run("output-folder-cannot-be-made", func(t *testing.T) {
		t.Parallel()
		file := filepath.Join(t.TempDir(), "out")
		if err := os.WriteFile(file, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		r := newRig(t, map[string]any{"output.dir": file}, endOnStop)
		r.describe("the output folder is a file")
		playA(r)
		r.check("states/output-folder-cannot-be-made")
	})
	t.Run("capture-ends-before-anything", func(t *testing.T) {
		t.Parallel()
		r := newRig(t, nil, endOnStop)
		r.endCapture()
		r.check("states/capture-ends-before-anything")
	})
	t.Run("audio-before-any-event", func(t *testing.T) {
		t.Parallel()
		r := newRig(t, nil, endOnStop)
		r.sound(1, 500)
		r.status(meta.Playing)
		r.track(trackA, 500)
		r.sound(1, 2500)
		r.quiet(500)
		r.track(trackB, 0)
		r.sound(2, 3000)
		r.quiet(1500)
		r.status(meta.Stopped)
		r.check("states/audio-before-any-event")
	})
}

// Covers given as files are copied for the take: once per file, a PNG as
// .png, and a file that cannot be read is left out.
func TestPinnedCovers(t *testing.T) {
	dir := t.TempDir()
	jpg := filepath.Join(dir, "a.jpg")
	png := filepath.Join(dir, "c.png")
	src, err := os.ReadFile(filepath.Join("..", "..", "testdata", "covers", "cover-01.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jpg, src, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(png, []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"), 0o644); err != nil {
		t.Fatal(err)
	}
	a, b, c := trackA, trackB, info("/x/c", "C", 3000)
	a.ArtURL = coverURL(jpg)
	b.ArtURL = coverURL(filepath.Join(dir, "missing.jpg"))
	c.ArtURL = coverURL(png)
	r := newRig(t, nil, endOnStop)
	r.describe("A's cover is a JPEG file, B's a file that is not there, C's a PNG file")
	r.status(meta.Playing)
	r.track(a, 0)
	r.quiet(500)
	r.sound(1, 1000)
	r.track(a, -1)
	r.sound(1, 2000)
	r.quiet(500)
	r.track(b, 0)
	r.sound(2, 3000)
	r.quiet(500)
	r.track(c, 0)
	r.sound(3, 3000)
	r.quiet(1500)
	r.status(meta.Stopped)
	r.check("states/covers")
}

// With recorder.skip_duplicates_forward, a duplicate makes the session
// send player.control next; the rig's player only writes it down.
func TestPinnedDuplicateSkipsForward(t *testing.T) {
	r := newRig(t, map[string]any{"recorder.duplicates": "skip", "recorder.skip_duplicates_forward": true}, endOnStop)
	r.alreadyRecorded("Artist", "B")
	playA(r)
	thenB(r)
	r.check("states/duplicate-skips-forward")
	waitFor(t, func() bool { return len(r.sent()) == 1 && r.sent()[0] == proto.CmdNext }, "player.control next")
}
