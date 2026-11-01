package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spytify/spytify/internal/capture"
	"github.com/spytify/spytify/internal/encode"
	"github.com/spytify/spytify/internal/fake"
	"github.com/spytify/spytify/internal/meta"
	"github.com/spytify/spytify/internal/settings"
	"github.com/spytify/spytify/internal/target"
	"github.com/spytify/spytify/pkg/proto"
)

// baseSettings make scenario output comparable sample by sample: WAV, no
// loudness change, no network.
var baseSettings = map[string]any{
	"encode.format":      "wav",
	"recorder.normalize": false,
	"tags.cover":         false,
	"output.group_by":    "none",
}

type fakeBackends struct{ b *fake.Backend }

func (f fakeBackends) New(target.Target) (capture.Capture, meta.NowPlaying, error) {
	return f.b, f.b, nil
}
func (f fakeBackends) Probe() []proto.Check { return nil }

type recorder struct {
	mu      sync.Mutex
	events  []proto.Event
	onEvent func(proto.Event)
}

func (r *recorder) add(method string, payload any) {
	raw, _ := payload.(json.RawMessage)
	ev := proto.Event{Method: method, Params: raw}
	r.mu.Lock()
	r.events = append(r.events, ev)
	hook := r.onEvent
	r.mu.Unlock()
	if hook != nil {
		hook(ev)
	}
}

func (r *recorder) all() []proto.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]proto.Event(nil), r.events...)
}

type scenario struct {
	core   *Core
	player *fake.Player
	out    string
	events *recorder
	logs   *bytes.Buffer
}

func newScenario(t *testing.T, tl *fake.Timeline, extra map[string]any) *scenario {
	t.Helper()
	return newScenarioAt(t, tl, extra, false)
}

// newScenarioAt plays the timeline in real time when realtime is set, for
// what depends on the clock, such as the session timer.
func newScenarioAt(t *testing.T, tl *fake.Timeline, extra map[string]any, realtime bool) *scenario {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	store, err := settings.Open(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	overrides := map[string]any{"output.dir": out}
	for _, m := range []map[string]any{baseSettings, tl.Settings, extra} {
		for k, v := range m {
			overrides[k] = v
		}
	}
	if rej := store.Override(overrides); len(rej) > 0 {
		t.Fatalf("settings rejected: %v", rej)
	}
	player, err := fake.NewPlayer(tl, realtime)
	if err != nil {
		t.Fatal(err)
	}
	logs := &bytes.Buffer{}
	log, _ := NewLogger("", &syncWriter{w: logs})
	c := New(Options{
		Env:         target.Env{Target: target.Target{ProcessNames: []string{"fakeplayer"}}, FFmpeg: "ffmpeg", StateDir: filepath.Join(dir, "state")},
		Store:       store,
		Log:         log,
		Backends:    fakeBackends{fake.NewBackend(player)},
		FindProcess: func(target.Target) (target.Process, bool) { return target.Process{PID: 4242, Name: "fakeplayer"}, true },
	})
	rec := &recorder{}
	c.SetBroadcaster(rec.add)
	return &scenario{core: c, player: player, out: out, events: rec, logs: logs}
}

type syncWriter struct {
	mu sync.Mutex
	w  *bytes.Buffer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

func (s *scenario) run(t *testing.T, tl *fake.Timeline) {
	t.Helper()
	for _, a := range tl.Actions {
		cmd := a.Cmd
		s.player.At(a.AtMs, func() {
			if err := s.core.Control(cmd); err != nil {
				t.Errorf("player.control %s: %v", cmd, err)
			}
		})
	}
	if _, err := s.core.StartSession(SessionOptions{EndOnStop: true}); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { s.core.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Minute):
		t.Fatalf("session did not end; log:\n%s", s.logs)
	}
}

func loadTimeline(t *testing.T, name string) *fake.Timeline {
	t.Helper()
	tl, err := fake.Load(filepath.Join("..", "..", "testdata", "timelines", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return tl
}

func TestScenarios(t *testing.T) {
	for _, name := range []string{"basic", "jitter", "gapless", "ads", "format-change", "pause-resume", "duplicate", "control", "seek", "quiet-start"} {
		t.Run(name, func(t *testing.T) {
			tl := loadTimeline(t, name)
			variants := tl.Variants
			if len(variants) == 0 {
				variants = []fake.Variant{{Name: "default", Expect: tl.Expect}}
			}
			for _, v := range variants {
				t.Run(v.Name, func(t *testing.T) {
					tl := loadTimeline(t, name)
					s := newScenario(t, tl, v.Settings)
					s.run(t, tl)
					files := -1
					if v.Files != nil {
						files = *v.Files
					}
					if keep := os.Getenv("SPYTIFY_KEEP_OUTPUT"); keep != "" {
						dst := filepath.Join(keep, name, v.Name)
						_ = os.RemoveAll(dst)
						if err := os.CopyFS(dst, os.DirFS(s.out)); err != nil {
							t.Log(err)
						}
					}
					if problems := fake.Verify(tl, v.Expect, files, s.out, s.events.all()); len(problems) > 0 {
						for _, p := range problems {
							t.Error(p)
						}
						t.Logf("log:\n%s", s.logs)
					}
				})
			}
		})
	}
}

// A player turned down to 5 % records at 100 %, and is turned back down
// once the session ends.
func TestRecordingRaisesThePlayerVolumeAndPutsItBack(t *testing.T) {
	tl := loadTimeline(t, "basic")
	low := 0.05
	tl.Volume = &low
	s := newScenario(t, tl, nil)
	during := make(chan float64, 1)
	s.player.At(3000, func() {
		v, _ := s.player.Volume()
		during <- v
	})
	s.run(t, tl)
	select {
	case v := <-during:
		if v != 1 {
			t.Errorf("volume while recording %.2f, want 1", v)
		}
	default:
		t.Error("the player never reached 3 s")
	}
	if v, _ := s.player.Volume(); v != low {
		t.Errorf("volume after the session %.2f, want %.2f", v, low)
	}
}

// The fake player shows a cover for each track, as a player does: it is
// in track.started and embedded in the saved file, whose track.finished
// says how long each step took.
func TestTheCoverGoesIntoTheFile(t *testing.T) {
	ff, err := encode.Find("ffmpeg")
	if err != nil {
		t.Skip(err)
	}
	tl := loadTimeline(t, "basic")
	s := newScenario(t, tl, map[string]any{"encode.format": "flac", "tags.cover": true})
	s.run(t, tl)
	saved := 0
	for _, ev := range s.events.all() {
		switch ev.Method {
		case proto.EventTrackStarted:
			var tr proto.TrackStartedEvent
			if ev.Decode(&tr) == nil && !strings.Contains(tr.CoverURL, "testdata/covers/cover-") {
				t.Errorf("%q started with cover %q", tr.Title, tr.CoverURL)
			}
		case proto.EventTrackFinished:
			var f proto.TrackFinishedEvent
			if ev.Decode(&f) != nil {
				continue
			}
			saved++
			// The app times saving a file from these.
			if tm := f.Timings; tm == nil || tm.Format != "flac" || tm.EncodeMs <= 0 || tm.TotalMs < tm.TrimMs+tm.CoverMs+tm.EncodeMs+tm.TagMs {
				t.Errorf("%s timings %+v", filepath.Base(f.Path), f.Timings)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			art, err := ff.Cover(ctx, f.Path)
			cancel()
			if err != nil || len(art) == 0 {
				t.Errorf("%s has no cover: %v", filepath.Base(f.Path), err)
			}
		}
	}
	if saved != len(tl.Items) {
		t.Fatalf("%d files saved, want %d", saved, len(tl.Items))
	}
}

// track.started says how the recorder found each start: the first track at
// its first sound, the next ones in the silence between them, near where
// the player said they changed.
func TestTrackStartedSaysHowItWasCut(t *testing.T) {
	tl := loadTimeline(t, "basic")
	s := newScenario(t, tl, nil)
	s.run(t, tl)
	var cuts []proto.TrackCut
	for _, ev := range s.events.all() {
		var tr proto.TrackStartedEvent
		if ev.Method != proto.EventTrackStarted || ev.Decode(&tr) != nil {
			continue
		}
		if tr.Cut == nil {
			t.Fatalf("%q started with no cut", tr.Title)
		}
		cuts = append(cuts, *tr.Cut)
	}
	if len(cuts) != len(tl.Items) {
		t.Fatalf("cuts %+v, want one per track", cuts)
	}
	for i, c := range cuts {
		kind, by := "boundary", "silence"
		if i == 0 {
			kind, by = "first", "first_sound"
		}
		if c.Kind != kind || c.By != by || c.OffsetMs < -2000 || c.OffsetMs > 2000 {
			t.Errorf("track %d cut %+v, want %s by %s within 2 s of the notice", i, c, kind, by)
		}
	}
}

// Each saved file says its counter number, the one in its name with the
// counter prefix: from the start value, one more per file.
func TestEachFileSaysItsCounter(t *testing.T) {
	tl := loadTimeline(t, "basic")
	s := newScenario(t, tl, map[string]any{"output.prefix": "counter", "output.counter_next": 5})
	s.run(t, tl)
	var got []int64
	for _, ev := range s.events.all() {
		var f proto.TrackFinishedEvent
		if ev.Method != proto.EventTrackFinished || ev.Decode(&f) != nil {
			continue
		}
		got = append(got, f.Counter)
		if want := fmt.Sprintf("%03d ", f.Counter); !strings.HasPrefix(filepath.Base(f.Path), want) {
			t.Errorf("%s does not start with its counter %q", filepath.Base(f.Path), want)
		}
	}
	if len(got) != 3 || got[0] != 5 || got[1] != 6 || got[2] != 7 {
		t.Fatalf("counters %v, want 5 6 7", got)
	}
	// The next recording goes on from there.
	if next := s.core.store.Values().Int(settings.OutputCounterNext); next != 8 {
		t.Errorf("next counter number %d, want 8", next)
	}
}

// Past the last number its digits allow, the counter saves nothing more
// and the recording stops, as v1's did.
func TestTheRecordingStopsWhenTheCounterIsFull(t *testing.T) {
	tl := loadTimeline(t, "basic")
	s := newScenario(t, tl, map[string]any{"output.prefix": "counter", "output.counter_next": 999, "output.counter_width": 3})
	s.run(t, tl)
	var saved []string
	full := false
	for _, ev := range s.events.all() {
		switch ev.Method {
		case proto.EventTrackFinished:
			var f proto.TrackFinishedEvent
			if ev.Decode(&f) == nil {
				saved = append(saved, filepath.Base(f.Path))
			}
		case proto.EventError:
			var e proto.ErrorEvent
			if ev.Decode(&e) == nil && e.Code == "counter_full" {
				full = true
			}
		}
	}
	if len(saved) != 1 || !strings.HasPrefix(saved[0], "999 ") || !full {
		t.Fatalf("saved %v, counter_full %v; want the 999 file alone and the counter full\n%s", saved, full, s.logs)
	}
}

// A cover the watcher deletes when the next track's comes out still goes
// into its own track's file.
func TestACoverFileGoneAtTheNextTrackStillGoesIntoItsFile(t *testing.T) {
	ff, err := encode.Find("ffmpeg")
	if err != nil {
		t.Skip(err)
	}
	tl := loadTimeline(t, "basic")
	dir := t.TempDir()
	covers := map[string][]byte{}
	for i := range tl.Items {
		b, err := os.ReadFile(tl.Items[i].Art)
		if err != nil {
			t.Fatal(err)
		}
		f, err := os.CreateTemp(dir, "cover-*.jpg")
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.Write(b)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		tl.Items[i].Art = f.Name()
		covers[filepath.Base(f.Name())] = b
	}
	s := newScenario(t, tl, map[string]any{"encode.format": "flac", "tags.cover": true})
	// As the Windows watcher does with its temporary files, a track's cover
	// file goes as the next track starts, before the player says so.
	start := tl.LeadInMs
	for i := range len(tl.Items) - 1 {
		start += tl.Items[i].LengthMs + tl.Items[i].GapMs
		art := tl.Items[i].Art
		s.player.At(start, func() { _ = os.Remove(art) })
	}
	s.run(t, tl)
	want := map[string][]byte{}
	saved := 0
	for _, ev := range s.events.all() {
		switch ev.Method {
		case proto.EventTrackStarted:
			var tr proto.TrackStartedEvent
			if ev.Decode(&tr) == nil {
				want[tr.ID] = covers[filepath.Base(tr.CoverURL)]
			}
		case proto.EventTrackFinished:
			var f proto.TrackFinishedEvent
			if ev.Decode(&f) != nil {
				continue
			}
			saved++
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			art, err := ff.Cover(ctx, f.Path)
			cancel()
			if w := want[f.ID]; len(w) == 0 || !bytes.Equal(art, w) {
				t.Errorf("%s: cover of %d bytes, want its own of %d: %v", filepath.Base(f.Path), len(art), len(w), err)
			}
		}
	}
	if saved != len(tl.Items) {
		t.Fatalf("%d files saved, want %d; log:\n%s", saved, len(tl.Items), s.logs)
	}
}
