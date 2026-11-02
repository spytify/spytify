package core

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spytify/spytify/internal/encode"
	"github.com/spytify/spytify/internal/meta"
	"github.com/spytify/spytify/internal/pcm"
	"github.com/spytify/spytify/internal/settings"
	"github.com/spytify/spytify/internal/testkit"
	"github.com/spytify/spytify/pkg/proto"
)

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
