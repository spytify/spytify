package core

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spytify/spytify/internal/capture"
	"github.com/spytify/spytify/internal/meta"
	"github.com/spytify/spytify/pkg/proto"
)

// The player's own volume lowered during a session: the warning says it at
// each new value and goes once it is back at 100%. The take it was lowered
// in is marked, and so is a take that starts while it is still low; a take
// played at 100% throughout is not.
func TestPinnedVolumeCases(t *testing.T) {
	a, b, c, d := info("/x/a", "A", 3000), info("/x/b", "B", 3000), info("/x/c", "C", 3000), info("/x/d", "D", 3000)
	scripts := map[string]func(r *rig){
		// Lowered 1 s into A, lowered more, then back at 100% before B.
		"lowered-during-a-track": func(r *rig) {
			r.status(meta.Playing)
			r.volume(1)
			r.track(a, 0)
			r.quiet(500)
			r.sound(1, 1000)
			r.volume(0.4)
			r.sound(1, 500)
			r.volume(0.4)
			r.volume(0.2)
			r.sound(1, 500)
			r.volume(1)
			r.sound(1, 1000)
			r.quiet(500)
			r.track(b, 0)
			r.sound(2, 3000)
			r.quiet(500)
			r.track(c, 0)
			r.sound(3, 1000)
			r.volume(0.5)
			r.sound(3, 2000)
			r.quiet(500)
			r.track(d, 0)
			r.sound(4, 3000)
			r.quiet(1500)
			r.status(meta.Stopped)
		},
	}
	for name, script := range scripts {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := newRig(t, nil, SessionOptions{EndOnStop: true})
			script(r)
			r.check("volume/" + name)
		})
	}
}

// The volume the session sets itself is not the user's: the 0 of an ad it
// mutes brings no warning, nor a 0 read just after it gave the volume back,
// from a poll taken before.
func TestTheAdMuteIsNotALowVolume(t *testing.T) {
	r := bareRig(t, map[string]any{"recorder.mute_ads": true}, endOnStop)
	s := r.s
	s.deps.mute = func(bool) error { return nil }
	ctx := context.Background()
	s.onMeta(ctx, statusEvent(meta.Playing))
	s.setState(proto.StateAd)
	s.onMeta(ctx, meta.Event{Kind: meta.VolumeChanged, Volume: 0, PositionMs: -1})
	s.setState(proto.StateRecording)
	s.onMeta(ctx, meta.Event{Kind: meta.VolumeChanged, Volume: 0, PositionMs: -1})
	s.onMeta(ctx, meta.Event{Kind: meta.VolumeChanged, Volume: 1, PositionMs: -1})
	want := []string{"state waiting", "state ad", "state recording"}
	if got := r.lines(); !slices.Equal(got, want) {
		t.Fatalf("trace:\n%q\nwant:\n%q", got, want)
	}
	// Past the grace, a 0 is the user's.
	s.unmutedAt = time.Now().Add(-2 * unmuteGrace)
	s.onMeta(ctx, meta.Event{Kind: meta.VolumeChanged, Volume: 0, PositionMs: -1})
	want = append(want, `log info: the player's volume is at 0%: tracks recorded now are quieter`, `error player_volume_low "the player's volume is at 0%: tracks recorded now are quieter" volume=0`)
	if got := r.lines(); !slices.Equal(got, want) {
		t.Fatalf("trace:\n%q\nwant:\n%q", got, want)
	}
}

// A player at 0% is silent, and its volume warning already says why: the
// silence warning does not come on top of it. It comes again once the
// volume is back and the track is still silent.
func TestAPlayerAt0BringsNoSilenceWarning(t *testing.T) {
	silent := capture.Chunk{Data: make([]byte, f44.FramesFromMs(10)*int64(f44.FrameBytes())), Format: f44}
	r := bareRig(t, nil, endOnStop)
	s := r.s
	s.deps.watchSilence = true
	ctx := context.Background()
	long := time.Now().Add(-time.Hour)
	s.onMeta(ctx, statusEvent(meta.Playing))
	s.onMeta(ctx, meta.Event{Kind: meta.VolumeChanged, Volume: 0, PositionMs: -1})
	s.onChunk(silent)
	s.lastSound, s.trackStarted = long, long
	s.onTick()
	if got := strings.Join(r.lines(), "\n"); strings.Contains(got, "capture_silent") {
		t.Fatalf("a silence warning at 0%%:\n%s", got)
	}
	s.onMeta(ctx, meta.Event{Kind: meta.VolumeChanged, Volume: 1, PositionMs: -1})
	s.onTick()
	if got := strings.Join(r.lines(), "\n"); !strings.Contains(got, `error player_volume_low ""`) || !strings.Contains(got, "error capture_silent") {
		t.Fatalf("back at 100%%, still silent:\n%s", got)
	}
}
