package core

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spytify/spytify/internal/capture"
	"github.com/spytify/spytify/internal/encode"
	"github.com/spytify/spytify/internal/fake"
	"github.com/spytify/spytify/internal/meta"
	"github.com/spytify/spytify/internal/pcm"
	"github.com/spytify/spytify/internal/testkit"
	"github.com/spytify/spytify/pkg/proto"
)

// directSession is a Session a test runs by hand, without Core, with a
// finisher that saves files.
func directSession(t *testing.T) (*Session, *recorder) {
	t.Helper()
	dir := t.TempDir()
	store := testStore(t, dir, map[string]any{"output.dir": filepath.Join(dir, "out")}, baseSettings)
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

// A track played with no sound at all, as from a player muted after the
// first track, is skipped as too quiet, whether recorder.trim_silence cuts
// the files or not. The tracks around it are saved.
func TestATrackWithNoSoundIsSkippedAsTooQuiet(t *testing.T) {
	for _, trim := range []bool{true, false} {
		t.Run(fmt.Sprintf("trim_silence=%v", trim), func(t *testing.T) {
			tl := loadTimeline(t, "basic")
			mid := &tl.Items[1]
			f := tl.Format()
			mid.WAV = testkit.WriteWAV(t, filepath.Join(t.TempDir(), "silence.wav"), f, make([]byte, f.FramesFromMs(mid.LengthMs)*int64(f.FrameBytes())))
			s := newScenario(t, tl, map[string]any{"recorder.trim_silence": trim})
			s.run(t, tl)
			saved, skipped := outcome(t, s)
			slices.Sort(saved)
			want := []string{tl.Items[0].Title, tl.Items[2].Title}
			slices.Sort(want)
			if !slices.Equal(saved, want) || len(skipped) != 1 || skipped[mid.Title] != proto.SkipTooQuiet {
				t.Errorf("saved %q, skipped %v; want %q saved and %q skipped as %s", saved, skipped, want, mid.Title, proto.SkipTooQuiet)
			}
		})
	}
}
