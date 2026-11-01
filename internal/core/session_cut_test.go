package core

import (
	"fmt"
	"math"
	"path/filepath"
	"testing"

	"github.com/spytify/spytify/internal/fake"
	"github.com/spytify/spytify/internal/meta"
	"github.com/spytify/spytify/internal/pcm"
)

// junction is what the audio does where one track gives way to the next.
type junction struct {
	name  string
	gapMs int64
	// fadeMs is each track's fade in and out; nil keeps the timeline's 30 ms.
	fadeMs *int64
	// tail, when set, is audio after each track's declared end, inside its
	// item: a quiet passage or a hum the player plays before the next.
	tail func(f pcm.Format) []byte
}

func ms(v int64) *int64 { return &v }

var junctions = []junction{
	{name: "gap1000", gapMs: 1000},
	{name: "gap300", gapMs: 300},
	{name: "gap20", gapMs: 20},
	{name: "touching", gapMs: 0},
	{name: "gapless", gapMs: 0, fadeMs: ms(0)},
	{name: "longfades", gapMs: 0, fadeMs: ms(1500)},
	// The last 400 ms of each track are 26 dB quieter, with no silence:
	// a dip, as where two tracks fade into each other.
	{name: "dip", fadeMs: ms(0), tail: func(f pcm.Format) []byte {
		return pcm.Synth{Seed: 99, LevelDB: -38}.Render(f, f.FramesFromMs(400))
	}},
	// A hum at -44 dBFS for a second between tracks: quiet, not silent.
	{name: "hum", tail: func(f pcm.Format) []byte {
		return pcm.Synth{Seed: 98, LevelDB: -44}.Render(f, f.FramesFromMs(1000))
	}},
}

// junctionTimeline is three tracks of 3, 2.5 and 2 s meeting at j, each
// notified offsetMs from its first sample.
func junctionTimeline(t *testing.T, j junction, offsetMs int64) *fake.Timeline {
	t.Helper()
	tl := &fake.Timeline{Name: "junction", Rate: 44100, Channels: 2, Seed: 11, LeadInMs: 500, TailMs: 1500}
	for i, length := range []int64{3000, 2500, 2000} {
		it := fake.Item{
			ID:             fmt.Sprintf("/com/fake/track/%d", i+1),
			Kind:           "track",
			Artists:        []string{"Artist"},
			Title:          fmt.Sprintf("Track %c", 'A'+i),
			Album:          "Album",
			LengthMs:       length,
			GapMs:          j.gapMs,
			NotifyOffsetMs: offsetMs,
			FadeMs:         j.fadeMs,
			Seed:           int64(100 + i),
		}
		if j.tail != nil && i < 2 {
			f := tl.Format()
			fade := int64(30)
			if j.fadeMs != nil {
				fade = *j.fadeMs
			}
			audio := pcm.Synth{Seed: it.Seed, FadeInMs: fade, FadeOut: fade}.Render(f, f.FramesFromMs(length))
			if j.name == "dip" {
				// The quiet passage is the end of the track's own length.
				audio = audio[:f.FramesFromMs(length-400)*int64(f.FrameBytes())]
			}
			it.WAV = writeWAV(t, f, append(audio, j.tail(f)...))
		}
		tl.Items = append(tl.Items, it)
	}
	return tl
}

// square is a wave whose every 1 ms window peaks at amp, the level a skip's
// edge is measured against.
func square(f pcm.Format, frames int64, amp float64) []byte {
	b := make([]byte, frames*int64(f.FrameBytes()))
	for i := int64(0); i < frames; i++ {
		v := amp
		if i%2 == 1 {
			v = -amp
		}
		for c := 0; c < f.Channels; c++ {
			f.PutSample(b, int(i)*f.Channels+c, v)
		}
	}
	return b
}

func writeWAV(t *testing.T, f pcm.Format, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "item.wav")
	w, err := pcm.CreateWAV(path, f)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// Where each track starts and ends, for every kind of junction and every
// lateness of the player's notification: early enough to be taken for a
// skip (-1500 ms), early, on time, late, and later than the drain holds
// audio back (+1500 ms). Today's behavior at -1500 ms looks odd: B is cut
// where the player named it, and C then at B's declared end counted from
// there.
func TestPinnedCutsAtEachJunction(t *testing.T) {
	for _, j := range junctions {
		for _, off := range []int64{-1500, -800, -300, 0, 300, 900, 1500} {
			name := fmt.Sprintf("cuts/%s/notify%+d", j.name, off)
			t.Run(fmt.Sprintf("%s/notify%+d", j.name, off), func(t *testing.T) {
				t.Parallel()
				r := newRig(t, nil, SessionOptions{EndOnStop: true})
				r.playTimeline(junctionTimeline(t, j, off))
				r.check(name)
			})
		}
	}
}

// The boundary search window, recorder.boundary_window_ms, from its
// smallest to its largest, at a gap and where tracks run gapless.
func TestPinnedCutsForEachWindow(t *testing.T) {
	for _, window := range []int64{20, 100, 300, 1000, 2000} {
		for _, c := range []struct {
			j   junction
			off int64
		}{{junctions[1], 300}, {junctions[4], 500}, {junctions[4], -800}} {
			name := fmt.Sprintf("windows/%d/%s-notify%+d", window, c.j.name, c.off)
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				r := newRig(t, map[string]any{"recorder.boundary_window_ms": window}, SessionOptions{EndOnStop: true})
				r.playTimeline(junctionTimeline(t, c.j, c.off))
				r.check(name)
			})
		}
	}
}

// A track shorter than the 300 ms search window, between two others.
// Today's behavior, which looks odd: between two gaps, the cut after it
// lands at the start of the gap before it, so its take is empty and its
// audio goes into the next one's.
func TestPinnedCutsAroundATrackShorterThanTheWindow(t *testing.T) {
	for _, gap := range []int64{0, 300, 1000} {
		for _, off := range []int64{0, 300} {
			name := fmt.Sprintf("short/gap%d-notify%+d", gap, off)
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				tl := junctionTimeline(t, junction{gapMs: gap}, off)
				tl.Items[1].LengthMs = 200
				r := newRig(t, nil, SessionOptions{EndOnStop: true})
				r.playTimeline(tl)
				r.check(name)
			})
		}
	}
}

// The timelines of the scenario tests, cut by cut.
func TestPinnedCutsOfTheScenarioTimelines(t *testing.T) {
	for _, name := range []string{"basic", "jitter", "gapless", "ads", "format-change", "pause-resume", "duplicate", "control", "seek", "quiet-start", "settings", "artists"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tl := loadTimeline(t, name)
			r := newRig(t, tl.Settings, SessionOptions{EndOnStop: true})
			r.playTimeline(tl)
			r.check("timelines/" + name)
		})
	}
}

// Two changes back to back: the second comes before the first could be
// cut, so the first is cut at once with what the ring holds. Today's
// behavior, which looks odd: the take in between can be empty, and is
// announced and handed over all the same.
func TestPinnedBackToBackChanges(t *testing.T) {
	a, b, c := info("/x/a", "A", 3000), info("/x/b", "B", 3000), info("/x/c", "C", 3000)
	scripts := map[string]func(r *rig){
		// B then C 50 ms apart, as the player names a track it skips at
		// once.
		"two-tracks-50ms-apart": func(r *rig) {
			r.status(meta.Playing)
			r.track(a, 0)
			r.quiet(500)
			r.sound(1, 3000)
			r.quiet(500)
			r.track(b, 0)
			r.quiet(50)
			r.track(c, 0)
			r.sound(3, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		},
		// B named twice before A ends, its album known the second time:
		// the same change, its metadata refreshed.
		"same-track-twice": func(r *rig) {
			r.status(meta.Playing)
			r.track(a, 0)
			r.quiet(500)
			r.sound(1, 2700)
			r.track(info("/x/b", "B", 0), 0)
			r.sound(1, 100)
			r.track(b, 0)
			r.sound(1, 200)
			r.quiet(500)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		},
		// Two skips 200 ms apart in the middle of A.
		"two-skips": func(r *rig) {
			r.status(meta.Playing)
			r.track(a, 0)
			r.quiet(500)
			r.sound(1, 1500)
			r.track(b, 0)
			r.sound(2, 200)
			r.track(c, 0)
			r.sound(3, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		},
		// Three names within 20 ms before any audio.
		"before-any-audio": func(r *rig) {
			r.status(meta.Playing)
			r.track(a, 0)
			r.track(b, 0)
			r.track(c, 0)
			r.quiet(500)
			r.sound(3, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		},
	}
	for _, name := range sortedKeys(scripts) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := newRig(t, nil, SessionOptions{EndOnStop: true})
			r.describe("A, B and C are 3000 ms each")
			scripts[name](r)
			r.check("backtoback/" + name)
		})
	}
}

// The first sound found frame by frame across the 5 ms blocks it is
// searched in, and the same audio in chunks of every size, from 1 ms to
// more than the drain holds back.
func TestPinnedCutsAtBlockEdgesAndChunkSizes(t *testing.T) {
	for _, lead := range []int64{0, 1, 220, 221, 222, 441} {
		for _, chunkMs := range []int64{1, 7, 10, 45, 400, 2000} {
			name := fmt.Sprintf("blocks/lead%d-chunk%dms", lead, chunkMs)
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				r := newRig(t, nil, SessionOptions{EndOnStop: true})
				r.chunk = r.f.FramesFromMs(chunkMs)
				r.describe("A starts %d frames after 500 ms of silence; chunks of %d ms", lead, chunkMs)
				r.status(meta.Playing)
				r.track(info("/x/a", "A", 1500), 0)
				r.play("silence", make([]byte, (r.f.FramesFromMs(500)+lead)*int64(r.f.FrameBytes())))
				r.sound(1, 1500)
				r.quiet(300)
				r.track(info("/x/b", "B", 1500), 0)
				r.sound(2, 1500)
				r.quiet(1500)
				r.status(meta.Stopped)
				r.check(name)
			})
		}
	}
}

// A track with no declared length is drained up to the hold-back, 1.3 s
// behind the capture; the next one is noticed from on time to later than
// that.
func TestPinnedCutsAfterATrackWithNoLength(t *testing.T) {
	for _, late := range []int64{0, 1000, 1200, 1300, 1400, 2000} {
		name := fmt.Sprintf("nolength/notify+%d", late)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := newRig(t, nil, SessionOptions{EndOnStop: true})
			r.describe("A declares no length; B is noticed %d ms after it starts", late)
			r.status(meta.Playing)
			r.track(info("/x/a", "A", 0), 0)
			r.quiet(500)
			r.sound(1, 3000)
			r.quiet(500)
			if late > 0 {
				r.sound(2, late)
			}
			r.track(info("/x/b", "B", 3000), 0)
			r.sound(2, 3000-late)
			r.quiet(1500)
			r.status(meta.Stopped)
			r.check(name)
		})
	}
}

// Single cases of the cut search.
func TestPinnedCutCases(t *testing.T) {
	a, b := info("/x/a", "A", 3000), info("/x/b", "B", 3000)
	scripts := map[string]func(r *rig){
		// The first track's notification comes before any sound, and the
		// sound never comes within the 2 s the session waits for it.
		"first-track-silent": func(r *rig) {
			r.status(meta.Playing)
			r.track(a, 0)
			r.quiet(3000)
			r.sound(1, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		},
		// A plays 2 s past its declared end with no notification: the ring
		// fills, and A records on with no declared length.
		"track-outlives-its-length": func(r *rig) {
			r.status(meta.Playing)
			r.track(info("/x/a", "A", 2000), 0)
			r.quiet(500)
			r.sound(1, 8000)
			r.quiet(500)
			r.track(b, 0)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		},
		// After A outlived its length, B is named with none and gets one
		// from a refresh. Today's behavior, which looks odd: the warning
		// about A is once per session, and so is its effect: B keeps no
		// length, and its end is searched as a skip's.
		"length-from-a-refresh-after-a-track-outlived-its-own": func(r *rig) {
			r.status(meta.Playing)
			r.track(info("/x/a", "A", 2000), 0)
			r.quiet(500)
			r.sound(1, 8000)
			r.quiet(500)
			r.track(info("/x/b", "B", 0), 0)
			r.sound(2, 1000)
			r.track(b, -1)
			r.sound(2, 2000)
			r.quiet(500)
			r.track(info("/x/c", "C", 3000), 0)
			r.sound(3, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		},
		// A chunk larger than the ring.
		"chunk-larger-than-the-ring": func(r *rig) {
			r.status(meta.Playing)
			r.track(a, 0)
			r.quiet(500)
			r.sound(1, 1000)
			r.chunk = r.f.FramesFromMs(5000)
			r.sound(2, 5000)
			r.chunk = r.f.FramesFromMs(10)
			r.quiet(1500)
			r.status(meta.Stopped)
		},
		// The player stops 1 s after A's declared end, which carries the
		// start of whatever came next: A ends at its declared length.
		"stopped-late-with-audio-after-the-end": func(r *rig) {
			r.status(meta.Playing)
			r.track(a, 0)
			r.quiet(500)
			r.sound(1, 3000)
			r.sound(2, 1000)
			r.status(meta.Stopped)
		},
		// Same, with a silence at A's end to cut in.
		"stopped-late-with-a-gap-after-the-end": func(r *rig) {
			r.status(meta.Playing)
			r.track(a, 0)
			r.quiet(500)
			r.sound(1, 3000)
			r.quiet(200)
			r.sound(2, 1000)
			r.status(meta.Stopped)
		},
		// A skip into a track that starts loud, with no fade-in: no quiet
		// millisecond to cut at.
		"skip-into-a-loud-start": func(r *rig) {
			r.status(meta.Playing)
			r.track(a, 0)
			r.quiet(500)
			r.sound(1, 1500)
			r.track(b, 0)
			r.play("music 2 with no fade", pcm.Synth{Seed: 2}.Render(r.f, r.f.FramesFromMs(3000)))
			r.quiet(1500)
			r.status(meta.Stopped)
		},
		// A take waiting for its first sound gets a chunk larger than the
		// ring can hold.
		"big-chunk-while-awaiting-the-first-sound": func(r *rig) {
			r.track(a, 100)
			r.status(meta.Playing)
			r.quiet(300)
			r.chunk = r.f.FramesFromMs(4500)
			r.play("silence then music 1", append(make([]byte, r.f.FramesFromMs(1500)*int64(r.f.FrameBytes())), music(1).Render(r.f, r.f.FramesFromMs(3000))...))
			r.chunk = r.f.FramesFromMs(10)
			r.quiet(500)
			r.track(b, 0)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		},
		// A take that starts at its first sound hears none for 2.5 s: it
		// starts where it was cut.
		"no-sound-for-the-start-timeout": func(r *rig) {
			r.track(a, 100)
			r.status(meta.Playing)
			r.quiet(2500)
			r.sound(1, 3000)
			r.quiet(500)
			r.track(b, 0)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		},
		// A is silent from start to end.
		"silent-track": func(r *rig) {
			r.status(meta.Playing)
			r.track(a, 0)
			r.quiet(500)
			r.sound(1, 200)
			r.quiet(3300)
			r.track(b, 0)
			r.sound(2, 3000)
			r.quiet(1500)
			r.track(info("/x/c", "C", 3000), 0)
			r.quiet(4000)
			r.status(meta.Stopped)
		},
		// After an advert, B is heard 2 s after its cut; it is moved along to
		// its end, then the player stops.
		"seek-to-the-end-after-a-late-start": func(r *rig) {
			r.status(meta.Playing)
			r.track(a, 0)
			r.quiet(500)
			r.sound(1, 3000)
			r.quiet(300)
			r.track(meta.TrackInfo{ID: "/x/ad/1", Title: "Sponsored", Artists: []string{"Sponsor"}, LengthMs: 2000}, 0)
			r.sound(9, 2000)
			r.track(b, 0)
			r.quiet(1900)
			r.sound(2, 2000)
			r.send(meta.Event{Kind: meta.Seeked, Track: b, PositionMs: 3000})
			r.sound(2, 1500)
			r.status(meta.Stopped)
		},
		// A skip 100 ms before the player says so, where the peak of 1 ms
		// drops 19.5 dB: just short of the 20 dB of an edge.
		"skip-at-a-19.5-dB-drop": func(r *rig) {
			r.status(meta.Playing)
			r.track(a, 0)
			r.quiet(500)
			r.play("square at -12 dBFS", square(r.f, r.f.FramesFromMs(1400), 0.25))
			r.play("square 19.5 dB lower", square(r.f, 50, 0.25*math.Pow(10, -19.5/20)))
			r.play("square at -12 dBFS", square(r.f, r.f.FramesFromMs(100)-50, 0.25))
			r.track(b, 0)
			r.play("square at -12 dBFS", square(r.f, r.f.FramesFromMs(3000), 0.25))
			r.quiet(1500)
			r.status(meta.Stopped)
		},
		// Same, 20.5 dB lower: an edge.
		"skip-at-a-20.5-dB-drop": func(r *rig) {
			r.status(meta.Playing)
			r.track(a, 0)
			r.quiet(500)
			r.play("square at -12 dBFS", square(r.f, r.f.FramesFromMs(1400), 0.25))
			r.play("square 20.5 dB lower", square(r.f, 50, 0.25*math.Pow(10, -20.5/20)))
			r.play("square at -12 dBFS", square(r.f, r.f.FramesFromMs(100)-50, 0.25))
			r.track(b, 0)
			r.play("square at -12 dBFS", square(r.f, r.f.FramesFromMs(3000), 0.25))
			r.quiet(1500)
			r.status(meta.Stopped)
		},
		// A skip at a 60 ms passage 25 dB quieter: both a dip and an edge;
		// the dip wins.
		"skip-at-a-quiet-passage": func(r *rig) {
			r.status(meta.Playing)
			r.track(a, 0)
			r.quiet(500)
			r.play("square at -12 dBFS", square(r.f, r.f.FramesFromMs(1400), 0.25))
			r.play("square 25 dB lower", square(r.f, r.f.FramesFromMs(60), 0.25*math.Pow(10, -25.0/20)))
			r.play("square at -12 dBFS", square(r.f, r.f.FramesFromMs(40), 0.25))
			r.track(b, 0)
			r.play("square at -12 dBFS", square(r.f, r.f.FramesFromMs(3000), 0.25))
			r.quiet(1500)
			r.status(meta.Stopped)
		},
		// The audio after B's notification comes one frame at a time, so
		// the trace shows the very frame at which each cut is made.
		"one-frame-chunks": func(r *rig) {
			r.status(meta.Playing)
			r.track(a, 0)
			r.chunk = 1
			r.quiet(30)
			r.sound(1, 30)
			r.chunk = r.f.FramesFromMs(10)
			r.sound(1, 2940)
			r.quiet(200)
			r.track(b, 0)
			r.chunk = 1
			r.sound(2, 400)
			r.chunk = r.f.FramesFromMs(10)
			r.sound(2, 2600)
			r.quiet(1500)
			r.status(meta.Stopped)
		},
		// The player names the next track while the ring holds less audio
		// than the search window needs, then stops at once.
		"stopped-before-the-window-filled": func(r *rig) {
			r.status(meta.Playing)
			r.track(a, 0)
			r.quiet(500)
			r.sound(1, 3000)
			r.track(b, 0)
			r.sound(2, 100)
			r.status(meta.Stopped)
		},
	}
	for _, name := range sortedKeys(scripts) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := newRig(t, nil, SessionOptions{EndOnStop: true})
			scripts[name](r)
			r.check("cases/" + name)
		})
	}
}
