package core

import (
	"fmt"
	"testing"

	"github.com/spytify/spytify/internal/fake"
	"github.com/spytify/spytify/internal/meta"
	"github.com/spytify/spytify/internal/pcm"
)

// pauseTimeline is a 4 s track paused once, then a 3 s one.
func pauseTimeline(silence bool, atMs, lengthMs, notifyMs, desyncMs int64) *fake.Timeline {
	tl := &fake.Timeline{Name: "pause", Rate: 44100, Channels: 2, Seed: 12, LeadInMs: 500, TailMs: 1500, PauseSilence: silence}
	tl.Items = []fake.Item{
		{ID: "/com/fake/track/1", Kind: "track", Artists: []string{"Artist"}, Title: "Track A", Album: "Album", LengthMs: 4000, GapMs: 500, Seed: 201,
			PauseAtMs: atMs, PauseMs: lengthMs, PauseNotifyMs: notifyMs, DesyncMs: desyncMs},
		{ID: "/com/fake/track/2", Kind: "track", Artists: []string{"Artist"}, Title: "Track B", Album: "Album", LengthMs: 3000, GapMs: 1000, Seed: 202},
	}
	return tl
}

// A pause at the start, in the middle and at the end of a track, shorter
// than the 20 ms of silence that make a pause and longer, its Paused and
// Playing notifications on time, early or late, with the stream carrying
// silence while paused or going quiet. With no data while paused and
// Paused 300 ms late, the fake player sends Playing before Paused. Today's
// behavior there looks odd: the session stays paused while it records B,
// and cuts the silence out of B and the gap before it.
func TestPinnedPauses(t *testing.T) {
	type pause struct {
		silence         bool
		at, length      int64
		notify, resumed int64
	}
	var cases []pause
	for _, at := range []int64{100, 2000, 3850} {
		for _, length := range []int64{15, 1500} {
			for _, nd := range [][2]int64{{0, 0}, {-150, 0}, {300, 0}, {0, -200}, {0, 250}} {
				cases = append(cases, pause{true, at, length, nd[0], nd[1]})
			}
		}
	}
	for _, nd := range [][2]int64{{0, 0}, {-150, 0}, {300, 0}, {0, -200}, {0, 250}} {
		cases = append(cases, pause{false, 2000, 1500, nd[0], nd[1]})
	}
	cases = append(cases, pause{false, 100, 1500, 0, 0}, pause{false, 3850, 1500, 0, 0})
	for _, c := range cases {
		stream := "silence"
		if !c.silence {
			stream = "nodata"
		}
		name := fmt.Sprintf("pauses/%s/at%d-for%d-notified%+d-resumed%+d", stream, c.at, c.length, c.notify, c.resumed)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := newRig(t, nil, SessionOptions{EndOnStop: true})
			r.playTimeline(pauseTimeline(c.silence, c.at, c.length, c.notify, c.resumed))
			r.check(name)
		})
	}
}

// fade plays the next ms of music seed fading out, or in, as a player does
// when it pauses and when it resumes.
func (r *rig) fade(seed, ms int64, out bool) {
	from := r.played[seed]
	r.played[seed] = from + ms
	data := pcm.Synth{Seed: seed, FadeInMs: 30}.Render(r.f, r.f.FramesFromMs(from+ms))
	way := "in"
	if out {
		way = "out"
	}
	r.play(fmt.Sprintf("music %d at %dms fading %s", seed, from, way), fading(r.f, data[r.f.FramesFromMs(from)*int64(r.f.FrameBytes()):], out))
}

// Pauses the timelines cannot play.
func TestPinnedPauseCases(t *testing.T) {
	a, b := info("/x/a", "A", 3000), info("/x/b", "B", 3000)
	scripts := map[string]func(r *rig){
		// The player fades out over 150 ms when paused and in over 60 ms
		// when resumed: the fades meet where the pause is cut out, a dip
		// that is heard, so A is not saved.
		"fades-around-a-pause": func(r *rig) {
			r.status(meta.Playing)
			r.track(a, 0)
			r.quiet(500)
			r.sound(1, 1000)
			r.status(meta.Paused)
			r.fade(1, 150, true)
			r.quiet(500)
			r.status(meta.Playing)
			r.fade(1, 60, false)
			r.sound(1, 1790)
			r.quiet(500)
			r.track(b, 0)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		},
		// The same, Playing noticed 200 ms after the sound came back, as
		// from a player that is polled.
		"fades-around-a-pause-resume-noticed-late": func(r *rig) {
			r.status(meta.Playing)
			r.track(a, 0)
			r.quiet(500)
			r.sound(1, 1000)
			r.status(meta.Paused)
			r.fade(1, 150, true)
			r.quiet(500)
			r.fade(1, 60, false)
			r.sound(1, 140)
			r.status(meta.Playing)
			r.sound(1, 1650)
			r.quiet(500)
			r.track(b, 0)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		},
		// Next pressed while paused: B is named during the pause. Today's
		// behavior, which looks odd: 1 s before A's declared end, the skip
		// is taken for A's natural end, and with no silence in the window
		// the cut lands at that end: A's file takes B's first second.
		"track-change-while-paused": func(r *rig) {
			r.status(meta.Playing)
			r.track(a, 0)
			r.quiet(500)
			r.sound(1, 2000)
			r.status(meta.Paused)
			r.quiet(500)
			r.track(b, 0)
			r.quiet(500)
			r.status(meta.Playing)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		},
		// The session starts on a paused track.
		"session-starts-paused-mid-track": func(r *rig) {
			r.track(a, 1000)
			r.status(meta.Paused)
			r.quiet(500)
			r.status(meta.Playing)
			r.sound(1, 2000)
			r.quiet(500)
			r.track(b, 0)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		},
		// Today's behavior, which looks odd: a track at 0 but paused is
		// taken for one already playing, and is not recorded.
		"session-starts-paused-at-zero": func(r *rig) {
			r.track(a, 0)
			r.status(meta.Paused)
			r.quiet(500)
			r.status(meta.Playing)
			r.sound(1, 3000)
			r.quiet(500)
			r.track(b, 0)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		},
		// Paused before the first track's first sound, while its start is
		// still searched.
		"paused-before-the-first-sound": func(r *rig) {
			r.status(meta.Playing)
			r.track(a, 0)
			r.quiet(300)
			r.status(meta.Paused)
			r.quiet(700)
			r.status(meta.Playing)
			r.quiet(200)
			r.sound(1, 3000)
			r.quiet(500)
			r.track(b, 0)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		},
		// Paused before A was heard, B named during the pause: A's start is
		// placed where the player named it, which the pause cut away.
		"track-change-while-paused-before-any-sound": func(r *rig) {
			r.status(meta.Playing)
			r.quiet(500)
			r.track(a, 0)
			r.quiet(500)
			r.status(meta.Paused)
			r.quiet(500)
			r.track(b, 0)
			r.quiet(300)
			r.status(meta.Playing)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		},
		// A's audio ends 500 ms before its declared end, B is named 300 ms
		// later, and the player pauses while B's cut is still searched: the
		// pause cuts back no earlier than where B's search starts. Today's
		// behavior, which looks odd: the silence the search needed is cut
		// with the pause, and B's first 300 ms go to A.
		"paused-while-the-next-cut-is-searched": func(r *rig) {
			r.status(meta.Playing)
			r.track(a, 0)
			r.quiet(500)
			r.sound(1, 2500)
			r.quiet(300)
			r.track(b, 0)
			r.quiet(100)
			r.status(meta.Paused)
			r.quiet(500)
			r.status(meta.Playing)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		},
		// The player stops during a pause.
		"stopped-while-paused": func(r *rig) {
			r.status(meta.Playing)
			r.track(a, 0)
			r.quiet(500)
			r.sound(1, 2000)
			r.status(meta.Paused)
			r.quiet(500)
			r.status(meta.Stopped)
		},
		// Paused twice in one track, the second time with the notification
		// 300 ms late.
		"paused-twice": func(r *rig) {
			r.status(meta.Playing)
			r.track(info("/x/a", "A", 4000), 0)
			r.quiet(500)
			r.sound(1, 1000)
			r.status(meta.Paused)
			r.quiet(400)
			r.status(meta.Playing)
			r.sound(1, 1500)
			r.quiet(300)
			r.status(meta.Paused)
			r.quiet(400)
			r.status(meta.Playing)
			r.sound(1, 1500)
			r.quiet(500)
			r.track(b, 0)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		},
		// Paused 300 ms after the audio stopped, and resumed 10 ms later:
		// the silence before the notification is cut back, and the 10 ms
		// after it go with it.
		"paused-late-resumed-at-once": func(r *rig) {
			r.status(meta.Playing)
			r.track(info("/x/a", "A", 3500), 0)
			r.quiet(500)
			r.sound(1, 1500)
			r.quiet(300)
			r.status(meta.Paused)
			r.quiet(10)
			r.status(meta.Playing)
			r.sound(1, 2000)
			r.quiet(500)
			r.track(b, 0)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		},
		// While paused, the stream carries notes at -30 dBFS with 22 ms and
		// 18 ms of silence between them: only a silence of 20 ms is a pause.
		"notes-while-paused": func(r *rig) {
			note := pcm.Synth{Seed: 8, LevelDB: -30}.Render(r.f, r.f.FramesFromMs(10))
			r.status(meta.Playing)
			r.track(a, 0)
			r.quiet(500)
			r.sound(1, 1500)
			r.status(meta.Paused)
			r.play("note at -30 dBFS", note)
			r.quiet(22)
			r.play("note at -30 dBFS", note)
			r.quiet(18)
			r.play("note at -30 dBFS", note)
			r.quiet(500)
			r.status(meta.Playing)
			r.sound(1, 1500)
			r.quiet(500)
			r.track(b, 0)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		},
		// While paused, the stream carries a note under the silence
		// threshold for 10 ms, shorter than a pause, then silence.
		"quiet-note-while-paused": func(r *rig) {
			r.status(meta.Playing)
			r.track(a, 0)
			r.quiet(500)
			r.sound(1, 1500)
			r.status(meta.Paused)
			r.play("music at -60 dBFS", pcm.Synth{Seed: 7, LevelDB: -60}.Render(r.f, r.f.FramesFromMs(10)))
			r.quiet(15)
			r.play("music at -60 dBFS", pcm.Synth{Seed: 7, LevelDB: -60}.Render(r.f, r.f.FramesFromMs(10)))
			r.quiet(500)
			r.status(meta.Playing)
			r.sound(1, 1500)
			r.quiet(500)
			r.track(b, 0)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		},
		// The stream goes on playing for 200 ms after Paused, as a player
		// that notifies before its buffer runs out.
		"audio-after-paused": func(r *rig) {
			r.status(meta.Playing)
			r.track(a, 0)
			r.quiet(500)
			r.sound(1, 1500)
			r.status(meta.Paused)
			r.sound(1, 200)
			r.quiet(500)
			r.status(meta.Playing)
			r.sound(1, 1300)
			r.quiet(500)
			r.track(b, 0)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		},
	}
	for _, name := range sortedKeys(scripts) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := newRig(t, nil, SessionOptions{EndOnStop: true})
			scripts[name](r)
			r.check("pausecases/" + name)
		})
	}
}
