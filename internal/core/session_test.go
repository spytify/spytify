package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
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
	"github.com/spytify/spytify/internal/testkit"
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
	// The stop, sent as the track starts, lets it finish.
	s := newScenario(t, tl, nil)
	onTrackStart(s.events, "Paper Lanterns", func() { s.core.StopSession(false) })
	runWith(t, s, SessionOptions{EndOnStop: true})
	saved, skipped := outcome(t, s)
	if len(saved) != 2 || saved[0] != "Midnight Ferry" || saved[1] != "Paper Lanterns" || len(skipped) != 0 {
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
	onTrackStart(s.events, "Paper Lanterns", func() { s.core.StopSession(true) })
	runWith(t, s, SessionOptions{EndOnStop: true})
	saved, skipped := outcome(t, s)
	if len(saved) != 1 || saved[0] != "Midnight Ferry" || skipped["Paper Lanterns"] != proto.SkipIncomplete {
		t.Fatalf("saved %v skipped %v", saved, skipped)
	}
	if files := fake.AudioFiles(s.out); len(files) != 1 {
		t.Fatalf("files %v", files)
	}
}

// onWaiting runs fn when the session first waits, its temp folder made.
func onWaiting(r *recorder, fn func()) {
	var once atomic.Bool
	r.onEvent = func(ev proto.Event) {
		var st proto.SessionStateEvent
		if ev.Method == proto.EventSessionState && ev.Decode(&st) == nil && st.State == proto.StateWaiting && once.CompareAndSwap(false, true) {
			fn()
		}
	}
}

func errorsOf(s *scenario) []proto.ErrorEvent {
	var out []proto.ErrorEvent
	for _, ev := range s.events.all() {
		var e proto.ErrorEvent
		if ev.Method == proto.EventError && ev.Decode(&e) == nil {
			out = append(out, e)
		}
	}
	return out
}

// The temp folder deleted while a session runs, by hand with the files
// around it, is made again for the next track.
func TestTempFolderDeletedIsMadeAgain(t *testing.T) {
	tl := loadTimeline(t, "basic")
	s := newScenario(t, tl, nil)
	onWaiting(s.events, func() {
		if err := os.RemoveAll(filepath.Join(s.out, ".spytify")); err != nil {
			t.Error(err)
		}
	})
	runWith(t, s, SessionOptions{EndOnStop: true})
	saved, skipped := outcome(t, s)
	if len(saved) != 3 || len(skipped) != 0 || len(errorsOf(s)) != 0 {
		t.Fatalf("saved %v skipped %v errors %v\n%s", saved, skipped, errorsOf(s), s.logs)
	}
}

// An output folder out of reach, its USB drive unplugged, stops the
// recording at once with an error that says so: nothing more can be
// recorded, this track included.
func TestOutputFolderGoneStopsTheRecording(t *testing.T) {
	tl := loadTimeline(t, "basic")
	s := newScenario(t, tl, nil)
	onWaiting(s.events, func() {
		// A file where the folder was: it cannot be made again.
		if err := os.RemoveAll(s.out); err != nil {
			t.Error(err)
		}
		if err := os.WriteFile(s.out, nil, 0o644); err != nil {
			t.Error(err)
		}
	})
	// Midnight Ferry ends at 5.5 s.
	var reached atomic.Bool
	s.player.At(5000, func() { reached.Store(true) })
	runWith(t, s, SessionOptions{EndOnStop: true})
	saved, skipped := outcome(t, s)
	if len(saved) != 0 || idOf(s, "Paper Lanterns") != "" {
		t.Fatalf("saved %v skipped %v\n%s", saved, skipped, s.logs)
	}
	// A track seen to start is said to be skipped, not left recording.
	if idOf(s, "Midnight Ferry") != "" && skipped["Midnight Ferry"] != proto.SkipIncomplete {
		t.Errorf("skipped %v", skipped)
	}
	if reached.Load() {
		t.Error("the recording waited for the track to end")
	}
	errs := errorsOf(s)
	if len(errs) != 1 || errs[0].Code != "output_dir" || !strings.HasPrefix(errs[0].Msg, "the output folder cannot be reached") {
		t.Fatalf("errors %+v", errs)
	}
}

// Without an output folder to write to, no recording starts, and the
// diagnostics say why.
func TestNoRecordingStartsWithoutAnOutputFolder(t *testing.T) {
	tl := loadTimeline(t, "basic")
	s := newScenario(t, tl, nil)
	if err := os.WriteFile(s.out, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := s.core.StartSession(SessionOptions{EndOnStop: true})
	var perr *proto.Error
	if !errors.As(err, &perr) || perr.Code != proto.CodeOutputDir {
		t.Fatalf("start: %v, want the output folder's error", err)
	}
	for _, c := range s.core.Info().Checks {
		if c.Name == proto.CheckOutputWritable && (c.OK || c.Detail == s.out) {
			t.Errorf("check %+v, want it failing with the reason", c)
		}
	}
}

// A track that cannot be saved because the output folder went, its USB
// drive unplugged after the track ended, stops the recording too.
func TestASaveWithTheOutputFolderGoneStopsTheRecording(t *testing.T) {
	dir := t.TempDir()
	// A file where the drive's folder was: the output folder cannot be
	// reached, nor made again.
	drive := filepath.Join(dir, "drive")
	if err := os.WriteFile(drive, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := settings.Open(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	overrides := map[string]any{"output.dir": filepath.Join(drive, "Music")}
	for k, v := range baseSettings {
		overrides[k] = v
	}
	if rej := store.Override(overrides); len(rej) > 0 {
		t.Fatalf("settings rejected: %v", rej)
	}
	ff := testkit.Need(t, "ffmpeg", encode.Find)
	log, _ := NewLogger("", testWriter{t})
	rec := &recorder{}
	emit := func(method string, payload any) {
		raw, _ := json.Marshal(payload)
		rec.add(method, json.RawMessage(raw))
	}
	fin := newFinisher(store.Values, ff, emit, log)
	var lost atomic.Bool
	fin.lost = func() { lost.Store(true) }

	f := pcm.Format{Rate: 44100, Channels: 2, Sample: pcm.S16LE}
	frames := f.FramesFromMs(3000)
	take := testkit.WriteWAV(t, filepath.Join(dir, "take.wav"), f, pcm.Synth{Seed: 1}.Render(f, frames))
	fin.enqueue(finishJob{id: "s-1", info: meta.TrackInfo{Title: "Midnight Ferry", Artists: []string{"Neon Harbor"}, Album: "Harbor Lights", LengthMs: 3000},
		tmp: take, format: f, played: frames, length: frames})
	fin.close()

	var errs []proto.ErrorEvent
	skipped := false
	for _, ev := range rec.all() {
		var e proto.ErrorEvent
		if ev.Method == proto.EventError && ev.Decode(&e) == nil {
			errs = append(errs, e)
		}
		var sk proto.TrackSkippedEvent
		skipped = skipped || (ev.Method == proto.EventTrackSkipped && ev.Decode(&sk) == nil && sk.ID == "s-1" && sk.Reason == proto.SkipIncomplete)
	}
	if !lost.Load() || !skipped || len(errs) != 1 || errs[0].Code != "output_dir" || !strings.HasPrefix(errs[0].Msg, "the output folder cannot be reached, so the recording stopped") {
		t.Fatalf("lost %v, skipped %v, errors %+v", lost.Load(), skipped, errs)
	}
}

// idOf is the id of the take of title.
func idOf(s *scenario, title string) string {
	for _, ev := range s.events.all() {
		var tr proto.TrackStartedEvent
		if ev.Method == proto.EventTrackStarted && ev.Decode(&tr) == nil && tr.Title == title {
			return tr.ID
		}
	}
	return ""
}

// keepable is every track.keepable the take of title got, in order.
func keepable(s *scenario, title string) []bool {
	id := idOf(s, title)
	var out []bool
	for _, ev := range s.events.all() {
		var k proto.TrackKeepableEvent
		if ev.Method == proto.EventTrackKeepable && ev.Decode(&k) == nil && k.ID == id {
			out = append(out, k.Keepable)
		}
	}
	return out
}

// finished waits for the file of take id.
func finished(t *testing.T, s *scenario, id string) proto.TrackFinishedEvent {
	t.Helper()
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		for _, ev := range s.events.all() {
			var f proto.TrackFinishedEvent
			if ev.Method == proto.EventTrackFinished && ev.Decode(&f) == nil && f.ID == id {
				return f
			}
		}
	}
	t.Fatalf("no file for %s; log:\n%s", id, s.logs)
	return proto.TrackFinishedEvent{}
}

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

// With recorder.stop_with_player, a session started as the app and the CLI
// start one ends when the player stops, not at its pauses; turned on
// mid-session, it applies to that session. Off, the session outlives the
// stop: here, until the fake player's stream ends.
func TestTheSessionEndsWhenThePlayerStops(t *testing.T) {
	for _, c := range []struct {
		name       string
		on, midway bool
	}{{"on", true, false}, {"off", false, false}, {"turned on mid-session", false, true}} {
		t.Run(c.name, func(t *testing.T) {
			tl := loadTimeline(t, "pause-resume")
			s := newScenario(t, tl, map[string]any{"recorder.stop_with_player": c.on})
			if c.midway {
				s.player.At(2000, func() {
					if _, err := s.core.SetSettings(map[string]any{"recorder.stop_with_player": true}); err != nil {
						t.Error(err)
					}
				})
			}
			if _, err := s.core.Handle(context.Background(), proto.MethodSessionStart, nil); err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			go func() { s.core.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(time.Minute):
				t.Fatalf("session did not end; log:\n%s", s.logs)
			}
			if saved, skipped := outcome(t, s); len(saved) != len(tl.Items) {
				t.Errorf("saved %v skipped %v, want every track", saved, skipped)
			}
			ends := c.on || c.midway
			if outlived := strings.Contains(s.logs.String(), "capture stream ended"); outlived == ends {
				t.Errorf("the session outlived the stop: %v, want %v\n%s", outlived, !ends, s.logs)
			}
		})
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
	if len(saved) != 1 || skipped["Hollow Moon"] != proto.SkipDuplicate {
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

// directSession is a Session a test runs by hand, without Core, with a
// finisher that saves files.
func directSession(t *testing.T) (*Session, *recorder) {
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
	ff := testkit.Need(t, "ffmpeg", encode.Find)
	// Core.emit hands the broadcaster marshalled payloads; do the same.
	emit := func(method string, payload any) {
		raw, _ := json.Marshal(payload)
		rec.add(method, json.RawMessage(raw))
	}
	fin := newFinisher(store.Values, ff, emit, log)
	s := newSession("test", sessionDeps{values: store.Values, emit: emit, log: log, fin: fin, control: func(string) error { return nil }}, SessionOptions{EndOnStop: true})
	return s, rec
}

func TestNoAudioWhileThePlayerPlaysIsReported(t *testing.T) {
	old := captureStall
	captureStall = 300 * time.Millisecond
	s, rec := directSession(t)
	frames := make(chan capture.Chunk)
	events := make(chan meta.Event)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Run(ctx, frames, events)
	}()
	// The session reads captureStall until Run returns.
	defer func() {
		cancel()
		<-done
		captureStall = old
	}()
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

// lowQuality is every capture_low_quality event, in order.
func lowQuality(events []proto.Event) []proto.ErrorEvent {
	var out []proto.ErrorEvent
	for _, ev := range events {
		var e proto.ErrorEvent
		if ev.Method == proto.EventError && ev.Decode(&e) == nil && e.Code == "capture_low_quality" {
			out = append(out, e)
		}
	}
	return out
}

// A capture under 44.1 kHz or in mono, as from a Bluetooth headset in a
// call, is reported once a session, not once a chunk or a track; one fit
// for music is not reported.
func TestALowQualityCaptureIsReportedOnce(t *testing.T) {
	for _, c := range []struct {
		rate, channels int
		warns          bool
	}{
		{16000, 2, true},
		{44100, 1, true},
		{44100, 2, false},
		{48000, 2, false},
	} {
		tl := loadTimeline(t, "basic")
		tl.Rate, tl.Channels = c.rate, c.channels
		s := newScenario(t, tl, nil)
		s.run(t, tl)
		got := lowQuality(s.events.all())
		if !c.warns {
			if len(got) != 0 {
				t.Errorf("%d Hz, %d channels: warned %+v", c.rate, c.channels, got)
			}
			continue
		}
		want := proto.AudioSource{SampleRate: c.rate, Channels: c.channels}
		if len(got) != 1 || got[0].Fatal || got[0].Msg == "" || got[0].Source == nil || *got[0].Source != want {
			t.Errorf("%d Hz, %d channels: warnings %+v, want one with the source %+v", c.rate, c.channels, got, want)
		}
	}
}

// Another microphone picked mid-track brings a headset in call mode back
// to 44.1 kHz stereo: the warning is withdrawn, the same code with no
// message, and raised again when a call takes the headset back.
func TestTheLowQualityWarningFollowsTheCapture(t *testing.T) {
	s, rec := directSession(t)
	frames := make(chan capture.Chunk)
	events := make(chan meta.Event)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Run(ctx, frames, events)
	}()
	events <- meta.Event{Kind: meta.TrackChanged, Track: meta.TrackInfo{ID: "/x/track/a", Title: "A", LengthMs: 60000}, PositionMs: 0}
	events <- meta.Event{Kind: meta.StatusChanged, Status: meta.Playing, PositionMs: 0}
	call := pcm.Format{Rate: 16000, Channels: 1, Sample: pcm.S16LE}
	music := pcm.Format{Rate: 44100, Channels: 2, Sample: pcm.S16LE}
	for i, f := range []pcm.Format{call, music, call} {
		data := pcm.Synth{Seed: int64(i + 1)}.Render(f, f.FramesFromMs(1000))
		step := f.FramesFromMs(10) * int64(f.FrameBytes())
		for j := int64(0); j < int64(len(data)); j += step {
			frames <- capture.Chunk{Data: data[j:min(j+step, int64(len(data)))], Format: f}
		}
	}
	cancel()
	<-done
	got := lowQuality(rec.all())
	if len(got) != 3 || got[0].Source == nil || *got[0].Source != (proto.AudioSource{SampleRate: 16000, Channels: 1}) || got[1].Msg != "" || got[2].Msg != got[0].Msg {
		t.Fatalf("warnings %+v, want raised, withdrawn, raised", got)
	}
}

// Each track recorded in a format too poor for music, under 44.1 kHz or
// in mono, is skipped as low quality and held for track.keep, each in
// turn, the last one until a track is saved. Kept, it is saved as it was
// recorded, at its own rate, as "<title> (low quality)". A capture fit for
// music saves every track.
func TestTracksRecordedInLowQualityAreSkippedAndCanBeKept(t *testing.T) {
	for _, c := range []struct {
		rate, channels int
		low            bool
	}{
		{16000, 1, true},
		{16000, 2, true},
		{44100, 1, true},
		{44100, 2, false},
		{48000, 2, false},
	} {
		tl := loadTimeline(t, "basic")
		tl.Rate, tl.Channels = c.rate, c.channels
		s := newScenario(t, tl, nil)
		s.run(t, tl)
		saved, skipped := outcome(t, s)
		if !c.low {
			if len(saved) != len(tl.Items) || len(skipped) != 0 {
				t.Errorf("%d Hz, %d channels: saved %v, skipped %v", c.rate, c.channels, saved, skipped)
			}
			continue
		}
		if len(saved) != 0 || len(fake.AudioFiles(s.out)) != 0 {
			t.Errorf("%d Hz, %d channels: saved %v", c.rate, c.channels, saved)
		}
		last := len(tl.Items) - 1
		for i, it := range tl.Items {
			want := []bool{true, false}
			if i == last {
				want = []bool{true}
			}
			if skipped[it.Title] != proto.SkipLowQuality || !slices.Equal(keepable(s, it.Title), want) {
				t.Errorf("%d Hz, %d channels: %q skipped %q, keepable %v, want %v", c.rate, c.channels, it.Title, skipped[it.Title], keepable(s, it.Title), want)
			}
		}
		id := idOf(s, tl.Items[last].Title)
		if err := s.core.Keep(id); err != nil {
			t.Fatalf("%d Hz, %d channels: %v", c.rate, c.channels, err)
		}
		f := finished(t, s, id)
		got, _, err := pcm.ReadWAV(f.Path)
		if err != nil || got.Rate != c.rate || got.Channels != c.channels || !strings.Contains(filepath.Base(f.Path), " (low quality).") ||
			!f.Partial || f.DurationMs < tl.Items[last].LengthMs-1000 {
			t.Errorf("%d Hz, %d channels: kept %+v as %s, %v", c.rate, c.channels, f, got, err)
		}
	}
}

// Another microphone picked between two tracks brings a headset in call
// mode back to 44.1 kHz stereo: the tracks played from then on are saved.
func TestTracksAreSavedOnceTheCaptureIsFitAgain(t *testing.T) {
	tl := loadTimeline(t, "basic")
	tl.Rate, tl.Channels = 16000, 1
	for i := 1; i < len(tl.Items); i++ {
		tl.Items[i].Rate, tl.Items[i].Channels = 44100, 2
	}
	s := newScenario(t, tl, nil)
	s.run(t, tl)
	expect := []fake.Expect{{Item: 0, Reason: string(proto.SkipLowQuality)}, {Item: 1, Saved: true}, {Item: 2, Saved: true}}
	if problems := fake.Verify(tl, expect, 2, s.out, s.events.all()); len(problems) > 0 {
		t.Fatalf("%v\n%s", problems, s.logs)
	}
	if got := keepable(s, tl.Items[0].Title); !slices.Equal(got, []bool{true, false}) {
		t.Fatalf("keepable %v: the take in low quality is held until a track is saved", got)
	}
}

// With the output disk full, the recording ends at once with an error,
// and the player is paused when recorder.pause_on_disk_full asks for it.
func TestAFullDiskStopsTheRecording(t *testing.T) {
	old := diskFree
	diskFree = func(string) (int64, bool) { return 10 << 20, true }
	defer func() { diskFree = old }()
	for _, pause := range []bool{true, false} {
		tl := loadTimeline(t, "basic")
		s := newScenario(t, tl, map[string]any{"recorder.pause_on_disk_full": pause})
		s.run(t, tl)
		full := false
		for _, ev := range s.events.all() {
			if ev.Method == proto.EventTrackFinished {
				t.Errorf("pause %v: a track was saved on a full disk", pause)
			}
			var e proto.ErrorEvent
			if ev.Method == proto.EventError && ev.Decode(&e) == nil && e.Code == "disk_full" {
				full = true
			}
		}
		if !full {
			t.Errorf("pause %v: no disk_full error", pause)
		}
		time.Sleep(100 * time.Millisecond)
		if got := slices.Contains(s.player.Commands(), proto.CmdPause); got != pause {
			t.Errorf("pause %v: the player was sent %v", pause, s.player.Commands())
		}
	}
}
