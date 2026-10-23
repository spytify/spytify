package core

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spytify/spytify/internal/capture"
	"github.com/spytify/spytify/internal/encode"
	"github.com/spytify/spytify/internal/fake"
	"github.com/spytify/spytify/internal/meta"
	"github.com/spytify/spytify/internal/pcm"
	"github.com/spytify/spytify/internal/settings"
	"github.com/spytify/spytify/internal/target"
	"github.com/spytify/spytify/pkg/proto"
)

// onTrackStart runs fn the first time a track with title starts.
func onTrackStart(r *recorder, title string, fn func()) {
	var once atomic.Bool
	r.onEvent = func(ev proto.Event) {
		var t proto.TrackStartedEvent
		if ev.Method == proto.EventTrackStarted && ev.Decode(&t) == nil && t.Title == title && once.CompareAndSwap(false, true) {
			fn()
		}
	}
}

func outcome(t *testing.T, s *scenario) (saved []string, skipped map[string]proto.SkipReason) {
	t.Helper()
	titles := map[string]string{}
	skipped = map[string]proto.SkipReason{}
	for _, ev := range s.events.all() {
		switch ev.Method {
		case proto.EventTrackStarted:
			var tr proto.TrackStartedEvent
			_ = ev.Decode(&tr)
			titles[tr.ID] = tr.Title
		case proto.EventTrackFinished:
			var f proto.TrackFinishedEvent
			_ = ev.Decode(&f)
			saved = append(saved, titles[f.ID])
		case proto.EventTrackSkipped:
			var sk proto.TrackSkippedEvent
			_ = ev.Decode(&sk)
			skipped[titles[sk.ID]] = sk.Reason
		}
	}
	return saved, skipped
}

func runWith(t *testing.T, s *scenario, opts SessionOptions) {
	t.Helper()
	if _, err := s.core.StartSession(opts); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { s.core.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Minute):
		t.Fatalf("session did not end; log:\n%s", s.logs)
	}
}

func states(s *scenario) []proto.SessionState {
	var out []proto.SessionState
	for _, ev := range s.events.all() {
		var st proto.SessionStateEvent
		if ev.Method == proto.EventSessionState && ev.Decode(&st) == nil {
			out = append(out, st.State)
		}
	}
	return out
}

func TestGracefulStopFinishesCurrentTrack(t *testing.T) {
	tl := loadTimeline(t, "basic")
	// With no minimum length the stop, sent as the track starts, always
	// takes the "let it finish" path.
	s := newScenario(t, tl, map[string]any{"recorder.min_seconds": 0})
	onTrackStart(s.events, "Basic Two", func() { s.core.StopSession(false) })
	runWith(t, s, SessionOptions{EndOnStop: true})
	saved, skipped := outcome(t, s)
	if len(saved) != 2 || saved[0] != "Basic One" || saved[1] != "Basic Two" || len(skipped) != 0 {
		t.Fatalf("saved %v skipped %v\n%s", saved, skipped, s.logs)
	}
	sawStopping := false
	for _, st := range states(s) {
		sawStopping = sawStopping || st == proto.StateStopping
	}
	if !sawStopping {
		t.Fatalf("no stopping state: %v", states(s))
	}
	if problems := fake.Verify(tl, []fake.Expect{{Item: 0, Saved: true}, {Item: 1, Saved: true}}, 2, s.out, s.events.all()); len(problems) > 0 {
		t.Fatal(problems)
	}
}

func TestStopNowDiscardsCurrentTrack(t *testing.T) {
	tl := loadTimeline(t, "basic")
	s := newScenario(t, tl, nil)
	onTrackStart(s.events, "Basic Two", func() { s.core.StopSession(true) })
	runWith(t, s, SessionOptions{EndOnStop: true})
	saved, skipped := outcome(t, s)
	if len(saved) != 1 || saved[0] != "Basic One" || skipped["Basic Two"] != proto.SkipIncomplete {
		t.Fatalf("saved %v skipped %v", saved, skipped)
	}
	if files := fake.AudioFiles(s.out); len(files) != 1 {
		t.Fatalf("files %v", files)
	}
}

func TestSingleTrack(t *testing.T) {
	tl := loadTimeline(t, "basic")
	s := newScenario(t, tl, nil)
	runWith(t, s, SessionOptions{EndOnStop: true, SingleTrack: true})
	saved, _ := outcome(t, s)
	if len(saved) != 1 || saved[0] != "Basic One" {
		t.Fatalf("saved %v", saved)
	}
}

type countingBackends struct {
	fakeBackends
	controls *atomic.Int32
}

type countingNP struct {
	*fake.Backend
	controls *atomic.Int32
}

func (c countingNP) Control(cmd string) error {
	if cmd == proto.CmdNext {
		c.controls.Add(1)
	}
	return c.Backend.Control(cmd)
}

func (c countingBackends) New(target.Target) (capture.Capture, meta.NowPlaying, error) {
	return c.b, countingNP{c.b, c.controls}, nil
}

func TestSkipDuplicateForward(t *testing.T) {
	tl := loadTimeline(t, "duplicate")
	s := newScenario(t, tl, map[string]any{"recorder.duplicates": "skip", "recorder.skip_duplicates_forward": true})
	var controls atomic.Int32
	s.core.backends = countingBackends{fakeBackends: s.core.backends.(fakeBackends), controls: &controls}
	runWith(t, s, SessionOptions{EndOnStop: true})
	saved, skipped := outcome(t, s)
	if len(saved) != 1 || skipped["Same Song"] != proto.SkipDuplicate {
		t.Fatalf("saved %v skipped %v", saved, skipped)
	}
	if controls.Load() != 1 {
		t.Fatalf("player.control next sent %d times", controls.Load())
	}
}

type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(string(p))
	return len(p), nil
}

// directSession drives a Session by hand, for states a timeline cannot
// reach, such as a session that starts in the middle of a track.
func directSession(t *testing.T) (*Session, *recorder, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := settings.Open(filepath.Join(dir, "config.toml"), settings.NewMemorySecrets())
	if err != nil {
		t.Fatal(err)
	}
	overrides := map[string]any{"output.dir": filepath.Join(dir, "out")}
	for k, v := range baseSettings {
		overrides[k] = v
	}
	store.Override(overrides)
	log, _ := NewLogger("", testWriter{t})
	log.SetLevel("debug")
	rec := &recorder{}
	ff, ffErr := encode.Find("ffmpeg")
	if ffErr != nil {
		t.Skip(ffErr)
	}
	// Core.emit hands the broadcaster marshalled payloads; do the same.
	emit := func(method string, payload any) {
		raw, _ := json.Marshal(payload)
		rec.add(method, json.RawMessage(raw))
	}
	fin := newFinisher(store.Values, ff, &tagService{values: store.Values, log: log}, emit, log)
	s := newSession("test", sessionDeps{values: store.Values, emit: emit, log: log, fin: fin, control: func(string) error { return nil }}, SessionOptions{EndOnStop: true})
	return s, rec, filepath.Join(dir, "out")
}

func TestSessionStartedMidTrackWaitsForNextTrack(t *testing.T) {
	s, rec, out := directSession(t)
	f := pcm.Format{Rate: 8000, Channels: 1, Sample: pcm.S16LE}
	frames := make(chan capture.Chunk)
	events := make(chan meta.Event)
	go s.Run(context.Background(), frames, events)

	a := meta.TrackInfo{ID: "/x/track/a", Title: "Already Playing", Artists: []string{"A"}, Album: "X", LengthMs: 4000}
	b := meta.TrackInfo{ID: "/x/track/b", Title: "Next One", Artists: []string{"B"}, Album: "X", LengthMs: 3000}
	events <- meta.Event{Kind: meta.TrackChanged, Track: a, PositionMs: 2500}
	events <- meta.Event{Kind: meta.StatusChanged, Status: meta.Playing, PositionMs: 2500}
	send := func(synth pcm.Synth, ms int64) {
		data := synth.Render(f, f.FramesFromMs(ms))
		step := f.FramesFromMs(10) * int64(f.FrameBytes())
		for i := int64(0); i < int64(len(data)); i += step {
			frames <- capture.Chunk{Data: data[i:min(i+step, int64(len(data)))], Format: f}
		}
	}
	send(pcm.Synth{Seed: 1, FadeOut: 20}, 1500) // the rest of "Already Playing"
	send(pcm.Synth{}, 0)
	frames <- capture.Chunk{Data: make([]byte, f.FramesFromMs(300)*2), Format: f}
	events <- meta.Event{Kind: meta.TrackChanged, Track: b, PositionMs: 0}
	send(pcm.Synth{Seed: 2, FadeInMs: 20, FadeOut: 20}, 3000)
	frames <- capture.Chunk{Data: make([]byte, f.FramesFromMs(800)*2), Format: f}
	events <- meta.Event{Kind: meta.StatusChanged, Status: meta.Stopped, PositionMs: -1}
	close(frames)

	deadline := time.Now().Add(30 * time.Second)
	for s.state() != proto.StateIdle && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	var started []string
	for _, ev := range rec.all() {
		var tr proto.TrackStartedEvent
		if ev.Method == proto.EventTrackStarted && ev.Decode(&tr) == nil {
			started = append(started, tr.Title)
		}
	}
	if len(started) != 1 || started[0] != "Next One" {
		t.Fatalf("started %v", started)
	}
	if files := fake.AudioFiles(out); len(files) != 1 || files[0] != "B - Next One.wav" {
		t.Fatalf("files %v", files)
	}
}
