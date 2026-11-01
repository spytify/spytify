package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
		`error capture_stalled "No audio from the player for 10 s. …"`,
		"log info: audio from the capture again",
		"log error: no audio from the capture for 10s while the player plays",
		`error capture_stalled "No audio from the player for 10 s. …"`,
		"state paused",
		"state waiting",
		"log info: player interface gone",
	}
	if got := r.lines(); !slices.Equal(got, want) {
		t.Fatalf("trace:\n%q\nwant:\n%q", got, want)
	}
}

// The level meter reports the highest peak and the RMS of every sample
// since its last report, at most every 100 ms, and only while recording.
// The clock is moved by setting when the last report went.
func TestPinnedMeter(t *testing.T) {
	r := bareRig(t, nil, endOnStop)
	s := r.s
	later := time.Now().Add(time.Hour)
	s.setState(proto.StateRecording)
	s.meter(-6, -10, 100)
	s.lastLevel = later
	s.meter(-20, -30, 100)
	s.meter(-3, -12, 300)
	s.lastLevel = time.Time{}
	s.meter(-40, pcm.Silence, 100)
	// No samples, no report.
	s.lastLevel = time.Time{}
	s.meter(-50, -50, 0)
	s.lastLevel = time.Time{}
	s.meter(pcm.Silence, pcm.Silence, 10)
	s.setState(proto.StatePaused)
	s.lastLevel = time.Time{}
	s.meter(-1, -1, 100)
	s.setState(proto.StateRecording)
	s.lastLevel = time.Time{}
	s.meter(-30, -30, 100)
	want := []proto.LevelEvent{
		{PeakDBFS: -6, RMSDBFS: -10},
		{PeakDBFS: -3, RMSDBFS: -14.2},
		{PeakDBFS: -120, RMSDBFS: -120},
		{PeakDBFS: -30, RMSDBFS: -30},
	}
	r.mu.Lock()
	got := append([]proto.LevelEvent(nil), r.levels...)
	r.mu.Unlock()
	if !slices.Equal(got, want) {
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
		// A call takes the headset during A, and ends during B. Today's
		// behavior, which looks odd: 1 s after A's cut, the new stream is
		// taken for A's start; A loses its first second, and its length,
		// counted from there, puts B's cut 500 ms late.
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

// What the session says when no audio comes, or the capture is too poor
// for music, on each system.
func TestPinnedHints(t *testing.T) {
	call := pcm.Format{Rate: 16000, Channels: 1, Sample: pcm.S16LE}
	f8 := pcm.Format{Rate: 8000, Channels: 2, Sample: pcm.S16LE}
	var b strings.Builder
	for _, goos := range []string{"darwin", "windows", "linux"} {
		fmt.Fprintf(&b, "%s stall: %s\n", goos, stallHint(goos))
		fmt.Fprintf(&b, "%s call: %s\n", goos, lowQualityHint(goos, call))
		fmt.Fprintf(&b, "%s 8 kHz: %s\n", goos, lowQualityHint(goos, f8))
	}
	checkGolden(t, "health/hints", b.String())
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
	r.s.classify(&segment{info: trackA, format: f44})
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
