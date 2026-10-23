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

// A restart of the same track mid-way (player.control restart, or repeat
// one) discards the partial take and records the track from its start.
func TestRestartRecordsTheTrackAgain(t *testing.T) {
	tl := loadTimeline(t, "basic")
	tl.Items = tl.Items[:1]
	tl.Items[0].LengthMs = 8000
	tl.Expect = nil
	s := newScenario(t, tl, nil)
	s.player.At(4000, func() {
		if err := s.core.Control(proto.CmdRestart); err != nil {
			t.Errorf("restart: %v", err)
		}
	})
	runWith(t, s, SessionOptions{EndOnStop: true})
	files := fake.AudioFiles(s.out)
	if len(files) != 1 {
		t.Fatalf("files %v\n%s", files, s.logs)
	}
	var skipped []proto.SkipReason
	for _, ev := range s.events.all() {
		var sk proto.TrackSkippedEvent
		if ev.Method == proto.EventTrackSkipped && ev.Decode(&sk) == nil {
			skipped = append(skipped, sk.Reason)
		}
	}
	if len(skipped) != 1 || skipped[0] != proto.SkipIncomplete {
		t.Fatalf("skipped %v, want the partial take as incomplete", skipped)
	}
	gotF, got, err := pcm.ReadWAV(filepath.Join(s.out, files[0]))
	if err != nil {
		t.Fatal(err)
	}
	wantF, want, _ := tl.ItemAudio(0)
	if c := pcm.Compare(wantF, want, gotF, got, fake.TrimDB, fake.SearchMs); !c.Matches(fake.MaxLagMs, fake.MaxErrorDB) {
		t.Fatalf("restarted take does not match the track: %+v\n%s", c, s.logs)
	}
}

func TestTheSameTrackAgainAtItsEndKeepsTheFullTake(t *testing.T) {
	tl := loadTimeline(t, "basic")
	tl.Items = tl.Items[:1]
	tl.Items[0].LengthMs = 8000
	tl.Expect = nil
	s := newScenario(t, tl, nil)
	// Repeat one: the item starts over as it ends.
	s.player.At(500+8000, func() {
		if err := s.core.Control(proto.CmdRestart); err != nil {
			t.Errorf("restart: %v", err)
		}
	})
	runWith(t, s, SessionOptions{EndOnStop: true})
	var skipped []proto.SkipReason
	for _, ev := range s.events.all() {
		var sk proto.TrackSkippedEvent
		if ev.Method == proto.EventTrackSkipped && ev.Decode(&sk) == nil {
			skipped = append(skipped, sk.Reason)
		}
	}
	files := fake.AudioFiles(s.out)
	if len(files) != 1 || len(skipped) != 1 || skipped[0] != proto.SkipDuplicate {
		t.Fatalf("files %v, skipped %v; want the full take kept and the replay a duplicate\n%s", files, skipped, s.logs)
	}
	gotF, got, err := pcm.ReadWAV(filepath.Join(s.out, files[0]))
	if err != nil {
		t.Fatal(err)
	}
	wantF, want, _ := tl.ItemAudio(0)
	if c := pcm.Compare(wantF, want, gotF, got, fake.TrimDB, fake.SearchMs); !c.Matches(fake.MaxLagMs, fake.MaxErrorDB) {
		t.Fatalf("the kept take does not match the track: %+v\n%s", c, s.logs)
	}
}

func TestMuteAdsSilencesThePlayerDuringTheAd(t *testing.T) {
	tl := loadTimeline(t, "ads")
	tl.Items[1].LengthMs = 8000
	tl.Expect = nil
	s := newScenario(t, tl, map[string]any{"recorder.mute_ads": true})
	if err := s.player.SetVolume(0.6); err != nil {
		t.Fatal(err)
	}
	volumeAt := map[string]float64{}
	probe := func(name string) func() {
		return func() { volumeAt[name], _ = s.player.Volume() }
	}
	// Item 0 ends at 6.5 s, the ad at 15 s, the last item at 20.3 s.
	s.player.At(3000, probe("song"))
	s.player.At(11000, probe("ad"))
	s.player.At(18000, probe("after"))
	runWith(t, s, SessionOptions{EndOnStop: true})
	if volumeAt["ad"] != 0 {
		t.Errorf("volume during the ad = %v, want 0", volumeAt["ad"])
	}
	// The session pins the player at full volume and restores 60% at the end.
	if volumeAt["song"] != 1 || volumeAt["after"] != 1 {
		t.Errorf("volume around the ad = %v", volumeAt)
	}
	if v, _ := s.player.Volume(); v != 0.6 {
		t.Errorf("volume after the session = %v, want 0.6", v)
	}
}

// directSession drives a Session by hand, for states a timeline cannot
// reach, such as a session that starts in the middle of a track.
func directSession(t *testing.T) (*Session, *recorder, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := settings.Open(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	overrides := map[string]any{"output.dir": filepath.Join(dir, "out")}
	for k, v := range baseSettings {
		overrides[k] = v
	}
	store.Override(overrides)
	log, _ := NewLogger("", testWriter{t})
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
	fin := newFinisher(store.Values, ff, emit, log)
	s := newSession("test", sessionDeps{values: store.Values, emit: emit, log: log, fin: fin, control: func(string) error { return nil }}, SessionOptions{EndOnStop: true})
	return s, rec, filepath.Join(dir, "out")
}

func TestNoAudioWhileThePlayerPlaysIsReported(t *testing.T) {
	defer func(d time.Duration) { captureStall = d }(captureStall)
	captureStall = 300 * time.Millisecond
	s, rec, _ := directSession(t)
	frames := make(chan capture.Chunk)
	events := make(chan meta.Event)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx, frames, events)
	events <- meta.Event{Kind: meta.TrackChanged, Track: meta.TrackInfo{ID: "/x/track/a", Title: "A", LengthMs: 60000}, PositionMs: 0}
	events <- meta.Event{Kind: meta.StatusChanged, Status: meta.Playing, PositionMs: 0}
	deadline := time.Now().Add(5 * time.Second)
	for {
		for _, ev := range rec.all() {
			var e proto.ErrorEvent
			if ev.Method == proto.EventError && ev.Decode(&e) == nil && e.Code == "capture_stalled" {
				if e.Msg == "" {
					t.Fatal("the warning says nothing")
				}
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("a capture that delivers nothing was not reported")
		}
		time.Sleep(20 * time.Millisecond)
	}
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
