package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spytify/spytify/internal/capture"
	"github.com/spytify/spytify/internal/meta"
	"github.com/spytify/spytify/internal/pcm"
	"github.com/spytify/spytify/pkg/proto"
)

func statusEvent(st meta.Status) meta.Event {
	return meta.Event{Kind: meta.StatusChanged, Status: st, PositionMs: -1}
}

// A capture that delivers nothing while the player plays is reported once,
// on the tick after captureStall, and again after it delivered and stopped
// once more; not while the player is paused or gone. The clock is moved by
// setting when the last chunk came.
func TestPinnedStallAndRecovery(t *testing.T) {
	r := bareRig(t, nil, endOnStop)
	s := r.s
	ctx := context.Background()
	long := time.Now().Add(-time.Hour)
	s.onMeta(ctx, statusEvent(meta.Playing))
	s.started = time.Now()
	s.onTick()
	s.started = long
	s.onTick()
	s.onTick()
	s.onChunk(capture.Chunk{Data: make([]byte, f44.FramesFromMs(10)*int64(f44.FrameBytes())), Format: f44})
	s.onTick()
	s.lastChunk = long
	s.onTick()
	s.onMeta(ctx, statusEvent(meta.Paused))
	s.stalled = false
	s.onTick()
	s.onMeta(ctx, statusEvent(meta.Playing))
	s.onMeta(ctx, meta.Event{Kind: meta.PlayerGone, PositionMs: -1})
	s.onTick()
	want := []string{
		"state waiting",
		"log error: no audio from the capture for 10s while the player plays",
		`error capture_stalled "no audio from the capture for 10s while the player plays"`,
		"log info: audio from the capture again",
		"log error: no audio from the capture for 10s while the player plays",
		`error capture_stalled "no audio from the capture for 10s while the player plays"`,
		"state paused",
		"state waiting",
		"log info: player interface gone",
	}
	if got := r.lines(); !slices.Equal(got, want) {
		t.Fatalf("trace:\n%q\nwant:\n%q", got, want)
	}
}

// A capture that delivers frames, all silent, while the player plays is
// reported once, on the tick after captureSilent, and withdrawn at the
// first sound, on every system: a player muted in its own volume or in the
// system's mixer gives silence, as macOS does when System Audio Recording
// was refused. Not while the player is paused, nor while the session mutes
// an ad. The clock is moved by setting when the last sound came.
func TestPinnedSilentCapture(t *testing.T) {
	silent := capture.Chunk{Data: make([]byte, f44.FramesFromMs(10)*int64(f44.FrameBytes())), Format: f44}
	loud := capture.Chunk{Data: slices.Repeat([]byte{0x00, 0x40}, len(silent.Data)/2), Format: f44}
	reported := []string{
		"log error: only silence from the capture for 30s while the player plays",
		`error capture_silent "only silence from the capture for 30s while the player plays"`,
		"log info: sound from the capture again",
		`error capture_silent ""`,
	}
	r := bareRig(t, map[string]any{"recorder.mute_ads": true}, endOnStop)
	s := r.s
	s.deps.watchSilence = true
	s.deps.mute = func(bool) error { return nil }
	var told []bool
	s.deps.silence = func(on bool) { told = append(told, on) }
	ctx := context.Background()
	long := time.Now().Add(-time.Hour)
	s.onMeta(ctx, statusEvent(meta.Playing))
	s.onChunk(silent)
	s.onTick()
	s.lastSound = long
	s.onTick()
	s.onChunk(silent)
	s.onTick()
	s.onChunk(loud)
	s.onMeta(ctx, statusEvent(meta.Paused))
	s.lastSound = long
	s.onTick()
	s.onMeta(ctx, statusEvent(meta.Playing))
	s.setState(proto.StateAd)
	s.lastSound = long
	s.onTick()
	s.setState(proto.StateRecording)
	s.onTick()
	s.onChunk(loud)
	// The player heard check is told of the silence, and of the
	// first sound; a later sound tells it nothing new.
	want := append(append([]string{"state waiting"}, reported...), "state paused", "state waiting", "state ad", "state recording")
	wantTold := []bool{true, false}
	if got := r.lines(); !slices.Equal(got, want) {
		t.Fatalf("trace:\n%q\nwant:\n%q", got, want)
	}
	if !slices.Equal(told, wantTold) {
		t.Fatalf("player heard check told %v, want %v", told, wantTold)
	}
}

// An ad brings no silence warning, muted by the session or not: it may be
// silent, as when the player mutes it. The 30 s start again when it ends.
func TestPinnedNoSilenceWarningOverAnAd(t *testing.T) {
	silent := capture.Chunk{Data: make([]byte, f44.FramesFromMs(10)*int64(f44.FrameBytes())), Format: f44}
	for _, muted := range []bool{false, true} {
		t.Run(fmt.Sprintf("mute_ads=%v", muted), func(t *testing.T) {
			r := bareRig(t, map[string]any{"recorder.mute_ads": muted}, endOnStop)
			s := r.s
			s.deps.watchSilence = true
			s.deps.mute = func(bool) error { return nil }
			long := time.Now().Add(-time.Hour)
			s.onMeta(context.Background(), statusEvent(meta.Playing))
			s.setState(proto.StateAd)
			s.onChunk(silent)
			s.lastSound = long
			s.onTick()
			s.setState(proto.StateRecording)
			s.onChunk(silent)
			s.onTick()
			afterAd := []string{"state waiting", "state ad", "state recording"}
			if got := r.lines(); !slices.Equal(got, afterAd) {
				t.Fatalf("over the ad and just after:\n%q\nwant:\n%q", got, afterAd)
			}
			s.lastSound = long
			s.onTick()
			want := []string{"state waiting", "state ad", "state recording",
				"log error: only silence from the capture for 30s while the player plays",
				`error capture_silent "only silence from the capture for 30s while the player plays"`}
			if got := r.lines(); !slices.Equal(got, want) {
				t.Fatalf("trace:\n%q\nwant:\n%q", got, want)
			}
		})
	}
}

// Only silence is watched until the session's first track ends: after it,
// a quiet stretch, such as the end of a quiet song, brings no warning, and
// a take with no sound is skipped as too quiet instead. An ad first is not
// that first track.
func TestNoSilenceWarningAfterTheFirstTrack(t *testing.T) {
	for _, c := range []struct {
		name  string
		first meta.TrackInfo
		warns bool
	}{
		{"after a track", trackA, false},
		{"after an ad", advert, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := bareRig(t, nil, endOnStop)
			s := r.s
			s.tmpDir = t.TempDir()
			s.deps.watchSilence = true
			ctx := context.Background()
			play := func(data []byte) {
				step := int(f44.FramesFromMs(10)) * f44.FrameBytes()
				for i := 0; i < len(data); i += step {
					s.onChunk(capture.Chunk{Data: data[i:min(i+step, len(data))], Format: f44})
				}
			}
			sound := func(seed int64) []byte { return music(seed).Render(f44, f44.FramesFromMs(3000)) }
			s.onMeta(ctx, statusEvent(meta.Playing))
			s.onMeta(ctx, meta.Event{Kind: meta.TrackChanged, Track: c.first, PositionMs: 0})
			play(sound(1))
			s.onMeta(ctx, meta.Event{Kind: meta.TrackChanged, Track: trackB, PositionMs: 0})
			play(sound(2))
			if !slices.ContainsFunc(r.lines(), func(l string) bool { return strings.HasPrefix(l, `started t-002 "B"`) }) {
				t.Fatalf("B never started:\n%q", r.lines())
			}
			s.lastSound = time.Now().Add(-time.Hour)
			s.onTick()
			warned := slices.ContainsFunc(r.lines(), func(l string) bool { return strings.HasPrefix(l, "error capture_silent") })
			if warned != c.warns {
				t.Fatalf("silence warned %v, want %v:\n%q", warned, c.warns, r.lines())
			}
		})
	}
}

// The player heard check fails while the capture hears only silence; the
// window words why, as the silence warning (model.CheckDetail).
func TestPlayerHeardCheck(t *testing.T) {
	same := func(a, b proto.Check) bool { return a.Name == b.Name && a.OK == b.OK && a.Detail == b.Detail }
	if got, want := playerHeardCheck(false), (proto.Check{Name: proto.CheckPlayerHeard, OK: true, Detail: "no silence heard"}); !same(got, want) {
		t.Errorf("with sound: %+v, want %+v", got, want)
	}
	if got, want := playerHeardCheck(true), (proto.Check{Name: proto.CheckPlayerHeard, Detail: "only silence from the capture while the player played"}); !same(got, want) {
		t.Errorf("with only silence: %+v, want %+v", got, want)
	}
}

// The level meter reports the highest peak and the RMS of every sample
// since its last report, at most every 100 ms, and only while recording,
// the track a stop waits for included. The clock is moved by setting when
// the last report went.
func TestPinnedMeter(t *testing.T) {
	r := bareRig(t, nil, endOnStop)
	s := r.s
	mono := func(peak, rms float64, frames int) { s.meter([]float64{peak}, []float64{rms}, frames) }
	later := time.Now().Add(time.Hour)
	s.setState(proto.StateRecording)
	mono(-6, -10, 100)
	s.lastLevel = later
	mono(-20, -30, 100)
	mono(-3, -12, 300)
	s.lastLevel = time.Time{}
	mono(-40, pcm.Silence, 100)
	// No samples, no report.
	s.lastLevel = time.Time{}
	mono(-50, -50, 0)
	s.lastLevel = time.Time{}
	mono(pcm.Silence, pcm.Silence, 10)
	s.setState(proto.StatePaused)
	s.lastLevel = time.Time{}
	mono(-1, -1, 100)
	s.setState(proto.StateRecording)
	s.lastLevel = time.Time{}
	mono(-30, -30, 100)
	// Stereo: each channel's, left then right, and the whole's.
	s.lastLevel = time.Time{}
	s.meter([]float64{-6, -20}, []float64{-12, -30}, 100)
	// Stop pressed: the track plays to its end, and is recorded to it.
	s.setState(proto.StateStopping)
	s.lastLevel = time.Time{}
	mono(-25, -25, 100)
	one := func(peak, rms float64) proto.LevelEvent {
		return proto.LevelEvent{PeakDBFS: peak, RMSDBFS: rms, Channels: []proto.ChannelLevel{{PeakDBFS: peak, RMSDBFS: rms}}}
	}
	want := []proto.LevelEvent{
		one(-6, -10),
		one(-3, -14.2),
		one(-120, -120),
		one(-30, -30),
		{PeakDBFS: -6, RMSDBFS: -14.9, Channels: []proto.ChannelLevel{{PeakDBFS: -6, RMSDBFS: -12}, {PeakDBFS: -20, RMSDBFS: -30}}},
		one(-25, -25),
	}
	r.mu.Lock()
	got := append([]proto.LevelEvent(nil), r.levels...)
	r.mu.Unlock()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("levels %+v, want %+v", got, want)
	}
}

// IdleExit ends a session the player left paused or stopped, with no sound
// for that long, once it has played; not while it plays.
func TestPinnedIdleExit(t *testing.T) {
	r := bareRig(t, nil, SessionOptions{EndOnStop: true, IdleExit: time.Minute})
	s := r.s
	ctx := context.Background()
	long := time.Now().Add(-time.Hour)
	s.started = time.Now()
	s.lastSound = long
	s.onMeta(ctx, statusEvent(meta.Paused))
	s.onTick()
	if s.done {
		t.Fatal("a player that never played ended the session")
	}
	s.onMeta(ctx, statusEvent(meta.Playing))
	s.lastSound = long
	s.lastChunk = time.Now()
	s.onTick()
	if s.done {
		t.Fatal("a playing player ended the session")
	}
	s.onMeta(ctx, statusEvent(meta.Paused))
	s.lastSound = time.Now()
	s.onTick()
	if s.done {
		t.Fatal("a player paused a moment ago ended the session")
	}
	s.lastSound = long
	s.onTick()
	if !s.done {
		t.Fatal("an idle player did not end the session")
	}
	want := []string{"state paused", "state waiting", "state paused", "log info: player idle for 1m0s, ending the session"}
	if got := r.lines(); !slices.Equal(got, want) {
		t.Fatalf("trace:\n%q\nwant:\n%q", got, want)
	}
}

// The capture's format, fit for music or too poor (a headset in call
// mode), changing between and within tracks.
func TestPinnedFormats(t *testing.T) {
	call := pcm.Format{Rate: 16000, Channels: 1, Sample: pcm.S16LE}
	f48 := pcm.Format{Rate: 48000, Channels: 2, Sample: pcm.S16LE}
	a, b := info("/x/a", "A", 3000), info("/x/b", "B", 3000)
	scripts := map[string]func(r *rig){
		// A call takes the headset 1 s into A, and ends 1 s into B. Each
		// plays on in the new stream for what is left of its length: its
		// file misses the second in the other format, and B is cut at A's
		// end.
		"call-during-a-track": func(r *rig) {
			r.status(meta.Playing)
			r.track(a, 0)
			r.quiet(500)
			r.sound(1, 1000)
			r.setFormat(call)
			r.sound(1, 2000)
			r.quiet(500)
			r.track(b, 0)
			r.sound(2, 1000)
			r.setFormat(f44)
			r.sound(2, 2000)
			r.quiet(1500)
			r.status(meta.Stopped)
		},
		// The call comes 2.5 s into a 6 s A: more than the 2 s a take may
		// miss, so A is cut short. B is still cut at A's end.
		"call-2.5s-into-a-track": func(r *rig) {
			r.status(meta.Playing)
			r.track(info("/x/a", "A", 6000), 0)
			r.quiet(500)
			r.sound(1, 2500)
			r.setFormat(call)
			r.sound(1, 3500)
			r.quiet(500)
			r.track(b, 0)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		},
		// The session starts in a call.
		"starts-in-a-call": func(r *rig) {
			r.setFormat(call)
			r.status(meta.Playing)
			r.track(a, 0)
			r.quiet(500)
			r.sound(1, 3000)
			r.quiet(500)
			r.track(b, 0)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		},
		// The stream is made again at 48 kHz 4 s into an 8 s track, with
		// no track change: the rest of it is not recorded.
		"format-change-mid-track": func(r *rig) {
			r.status(meta.Playing)
			r.track(info("/x/a", "A", 8000), 0)
			r.quiet(500)
			r.sound(1, 4000)
			r.setFormat(f48)
			r.sound(1, 4000)
			r.quiet(500)
			r.track(b, 0)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		},
		// The stream is made again 500 ms after B was cut: B is in the new
		// stream.
		"format-change-just-after-a-cut": func(r *rig) {
			r.status(meta.Playing)
			r.track(a, 0)
			r.quiet(500)
			r.sound(1, 3000)
			r.quiet(500)
			r.track(b, 0)
			r.quiet(500)
			r.setFormat(f48)
			r.quiet(200)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		},
		// The stream is made again while B's cut is still searched. Today's
		// behavior, which looks odd: B's track.started names the old
		// format as its source.
		"format-change-with-a-change-pending": func(r *rig) {
			r.status(meta.Playing)
			r.track(a, 0)
			r.quiet(500)
			r.sound(1, 3000)
			r.quiet(100)
			r.track(b, 0)
			r.quiet(100)
			r.setFormat(f48)
			r.quiet(200)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		},
		// The player is heard of before any audio, which then comes at
		// 48 kHz.
		"metadata-before-audio-in-another-format": func(r *rig) {
			r.setFormat(f48)
			r.status(meta.Playing)
			r.track(a, 0)
			r.quiet(500)
			r.sound(1, 3000)
			r.quiet(500)
			r.track(b, 0)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		},
		// The stream is made again twice before the next track is named.
		"two-format-changes-before-a-track": func(r *rig) {
			r.status(meta.Playing)
			r.track(info("/x/a", "A", 5000), 0)
			r.quiet(500)
			r.sound(1, 5000)
			r.setFormat(f48)
			r.quiet(300)
			r.setFormat(f44)
			r.quiet(300)
			r.track(b, 0)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		},
		// After a format change mid-track, a chunk larger than the ring
		// comes before the next track is named.
		"big-chunk-after-a-format-change": func(r *rig) {
			r.status(meta.Playing)
			r.track(info("/x/a", "A", 9000), 0)
			r.quiet(500)
			r.sound(1, 4000)
			r.setFormat(f48)
			r.sound(1, 500)
			r.chunk = r.f.FramesFromMs(4500)
			r.sound(1, 4500)
			r.chunk = r.f.FramesFromMs(10)
			r.quiet(500)
			r.track(b, 0)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		},
		// The player names A again after the stream was made again in the
		// middle of it.
		"same-track-named-after-a-format-change": func(r *rig) {
			r.status(meta.Playing)
			r.track(info("/x/a", "A", 8000), 0)
			r.quiet(500)
			r.sound(1, 4000)
			r.setFormat(f48)
			r.sound(1, 500)
			r.track(info("/x/a", "A", 8000), -1)
			r.sound(1, 3500)
			r.quiet(500)
			r.track(b, 0)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		},
		// The stream is made again before any track is named.
		"format-change-while-waiting": func(r *rig) {
			r.status(meta.Playing)
			r.quiet(300)
			r.setFormat(f48)
			r.quiet(300)
			r.track(b, 0)
			r.quiet(200)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		},
		// The session starts at the very start of A, before any audio, which
		// then comes at 48 kHz.
		"fresh-start-before-audio-in-another-format": func(r *rig) {
			r.setFormat(f48)
			r.track(a, 0)
			r.status(meta.Playing)
			r.quiet(300)
			r.sound(1, 3000)
			r.quiet(500)
			r.track(b, 0)
			r.sound(2, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		},
		// A format change and the end of playback in the 3 s after it.
		"stopped-after-a-format-change": func(r *rig) {
			r.status(meta.Playing)
			r.track(info("/x/a", "A", 8000), 0)
			r.quiet(500)
			r.sound(1, 4000)
			r.setFormat(f48)
			r.sound(1, 1000)
			r.status(meta.Stopped)
		},
	}
	for _, name := range sortedKeys(scripts) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := newRig(t, nil, endOnStop)
			scripts[name](r)
			r.check("formats/" + name)
		})
	}
}

// With the output disk full, the take is skipped and the session stops at
// once, pausing the player when recorder.pause_on_disk_full asks for it.
func TestPinnedDiskFull(t *testing.T) {
	old := diskFree
	defer func() { diskFree = old }()
	for _, c := range []struct {
		name  string
		pause bool
		// fullFrom is the take from which the disk is full.
		fullFrom int32
	}{
		{"first-take", true, 1},
		{"first-take-no-pause", false, 1},
		{"second-take", true, 2},
	} {
		t.Run(c.name, func(t *testing.T) {
			var takes atomic.Int32
			diskFree = func(string) (int64, bool) {
				if takes.Add(1) >= c.fullFrom {
					return 10 << 20, true
				}
				return 10 << 30, true
			}
			r := newRig(t, map[string]any{"recorder.pause_on_disk_full": c.pause}, endOnStop)
			playA(r)
			thenB(r)
			r.check("diskfull/" + c.name)
			if c.pause {
				waitFor(t, func() bool { return slices.Contains(r.sent(), proto.CmdPause) }, "player.control pause")
			}
		})
	}
}

// A player whose volume cannot be set is written down in the log, and the
// session goes on.
func TestPinnedMuteFailure(t *testing.T) {
	r := bareRig(t, map[string]any{"recorder.mute_ads": true}, endOnStop)
	r.s.deps.mute = func(bool) error { return errors.New("no volume control") }
	r.s.setState(proto.StateAd)
	r.s.setState(proto.StateRecording)
	want := []string{"state ad", "log info: mute ads: no volume control", "state recording", "log info: mute ads: no volume control"}
	if got := r.lines(); !slices.Equal(got, want) {
		t.Fatalf("trace:\n%q\nwant:\n%q", got, want)
	}
}

// A skip to the next track the player refuses is written down in the log.
func TestPinnedSkipForwardFailure(t *testing.T) {
	r := bareRig(t, map[string]any{"recorder.duplicates": "skip", "recorder.skip_duplicates_forward": true}, endOnStop)
	r.alreadyRecorded("Artist", "A")
	r.s.deps.control = func(string) error { return errors.New("not now") }
	seg := &segment{info: trackA, format: f44}
	r.s.classify(seg)
	r.s.announce(seg)
	waitFor(t, func() bool { return slices.Contains(r.lines(), "log info: skip to next: not now") }, "log of the refused skip")
}

// A temp file that cannot be made, its name taken by a folder, skips its
// take with an error; the output folder is still there, so the session goes
// on. Today's behavior, which looks odd: the take reports the recording
// state, and is said skipped only at its end.
func TestPinnedTempFileCannotBeMade(t *testing.T) {
	r := newRig(t, nil, endOnStop)
	if err := os.MkdirAll(filepath.Join(r.out, ".spytify", "t-002.wav"), 0o755); err != nil {
		t.Fatal(err)
	}
	r.describe("a folder is in the way of t-002's temp file")
	playA(r)
	r.sound(1, 2000)
	r.quiet(500)
	r.track(trackB, 0)
	r.sound(2, 3000)
	r.quiet(500)
	r.track(info("/x/c", "C", 3000), 0)
	r.sound(3, 3000)
	r.quiet(1500)
	r.status(meta.Stopped)
	r.check("health/temp-file-cannot-be-made")
}
