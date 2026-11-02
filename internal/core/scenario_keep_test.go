package core

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spytify/spytify/internal/encode"
	"github.com/spytify/spytify/internal/testkit"
	"github.com/spytify/spytify/pkg/proto"
)

// A take ended by Stop now is not saved, but it can be after the session:
// track.keep saves the part that played as "<title> (partial)".
func TestStopNowTakeCanBeKept(t *testing.T) {
	tl := loadTimeline(t, "basic")
	s := newScenario(t, tl, nil)
	// Paper Lanterns starts at 6.5 s: stop 2 s into it.
	s.player.At(8500, func() { _, _ = s.core.StopSession(true) })
	runWith(t, s, SessionOptions{EndOnStop: true})
	_, skipped := outcome(t, s)
	if skipped["Paper Lanterns"] != proto.SkipIncomplete || !slices.Equal(keepable(s, "Paper Lanterns"), []bool{true}) {
		t.Fatalf("skipped %v, keepable %v", skipped, keepable(s, "Paper Lanterns"))
	}
	if err := s.core.Keep("another"); err == nil {
		t.Fatal("kept a take that is not held")
	}
	id := idOf(s, "Paper Lanterns")
	if err := s.core.Keep(id); err != nil {
		t.Fatal(err)
	}
	f := finished(t, s, id)
	if !f.Partial || !strings.HasSuffix(f.Path, "Paper Lanterns (partial).wav") || f.DurationMs < 1000 || f.DurationMs > 3000 {
		t.Fatalf("kept %+v", f)
	}
	if !slices.Equal(keepable(s, "Paper Lanterns"), []bool{true, false}) {
		t.Fatalf("keepable %v", keepable(s, "Paper Lanterns"))
	}
	if err := s.core.Keep(id); err == nil {
		t.Fatal("kept the same take twice")
	}
}

// A take kept once its session ended still gets its cover, though the
// session removed its copies of the player's cover files as it ended.
func TestATakeKeptAfterTheSessionHasItsCover(t *testing.T) {
	ff := testkit.Need(t, "ffmpeg", encode.Find)
	tl := loadTimeline(t, "basic")
	s := newScenario(t, tl, map[string]any{"encode.format": "flac", "tags.cover": true})
	// Paper Lanterns starts at 6.5 s: stop 2 s into it.
	s.player.At(8500, func() { _, _ = s.core.StopSession(true) })
	runWith(t, s, SessionOptions{EndOnStop: true})
	id := idOf(s, "Paper Lanterns")
	if err := s.core.Keep(id); err != nil {
		t.Fatal(err)
	}
	f := finished(t, s, id)
	want, err := os.ReadFile(tl.Items[1].Art)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if art, err := ff.Cover(ctx, f.Path); !bytes.Equal(art, want) {
		t.Fatalf("%s: cover of %d bytes, want its own of %d: %v\n%s", filepath.Base(f.Path), len(art), len(want), err, s.logs)
	}
	if left, _ := filepath.Glob(filepath.Join(s.out, ".spytify", "cover-*")); len(left) > 0 {
		t.Fatalf("covers left behind: %v", left)
	}
}

// Only the last take cut short can be kept, and only until the next track
// is saved: its audio is gone then.
func TestKeepableTakeGoesOnceTheNextTrackIsSaved(t *testing.T) {
	tl := loadTimeline(t, "control")
	s := newScenario(t, tl, nil)
	s.run(t, tl)
	if got := keepable(s, "Fast Forward"); !slices.Equal(got, []bool{true, false}) {
		t.Fatalf("keepable %v", got)
	}
	if err := s.core.Keep(idOf(s, "Fast Forward")); err == nil {
		t.Fatal("kept a take that was let go")
	}
	if held, _ := filepath.Glob(filepath.Join(s.out, ".spytify", "*.wav")); len(held) > 0 {
		t.Fatalf("audio left behind: %v", held)
	}
}

// A take moved along its track bar is recorded to its end all the same, so
// keeping it saves all of it, jump included.
func TestSeekedTakeIsKeptWhole(t *testing.T) {
	tl := loadTimeline(t, "seek")
	s := newScenario(t, tl, nil)
	// Back Again plays 8.5 s, jumping back 2.5 s at 4 s. It is kept as soon
	// as it is held: Chapter Four, saved next, lets it go.
	var kept atomic.Bool
	s.events.onEvent = func(ev proto.Event) {
		var k proto.TrackKeepableEvent
		if ev.Method != proto.EventTrackKeepable || ev.Decode(&k) != nil || !k.Keepable || k.ID != idOf(s, "Back Again") || !kept.CompareAndSwap(false, true) {
			return
		}
		if err := s.core.Keep(k.ID); err != nil {
			t.Errorf("keep: %v", err)
		}
	}
	s.run(t, tl)
	f := finished(t, s, idOf(s, "Back Again"))
	if !f.Partial || f.DurationMs < 7500 {
		t.Fatalf("kept %+v, want the 8.5 s played", f)
	}
	if got := keepable(s, "Skip Ahead"); !slices.Equal(got, []bool{true, false}) {
		t.Fatalf("Skip Ahead keepable %v: the next take held should replace it", got)
	}
}
