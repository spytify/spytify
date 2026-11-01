package ui

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/telemetry/fakesentry"
	"github.com/spytify/spytify/internal/ui/link"
	"github.com/spytify/spytify/pkg/proto"
)

// diagnostics opens the diagnostics' dialog and returns it.
func diagnostics(t *testing.T, a *App) fyne.CanvasObject {
	t.Helper()
	a.showDiagnostics()
	return overlay(t, a)
}

// sendButton is the Send to the developers button of the diagnostics'
// dialog, which shows.
func sendButton(t *testing.T, a *App) *widget.Button {
	t.Helper()
	found := buttons(overlay(t, a), "Send to the developers")
	if len(found) != 1 {
		t.Fatalf("%d Send to the developers buttons", len(found))
	}
	return found[0]
}

// A failed start leaves as its kind only: the message holds paths.
func TestStartErrorKinds(t *testing.T) {
	for err, want := range map[error]string{
		link.ErrInArchive: "in archive",
		link.ErrNoDaemon:  "not found",
		errors.New("spytifyd started but does not answer; see its log in Logs"): "no answer",
		errors.New("spytifyd stopped: listen: address already in use"):          "failed",
	} {
		if got := startErrorKind(err); got != want {
			t.Errorf("startErrorKind(%q) = %q, want %q", err, got, want)
		}
	}
}

// With the bug tracker on, the recording counts leave: each track saved or
// skipped and how each start was cut, with no names.
func TestRecordingCountsLeave(t *testing.T) {
	srv := fakesentry.New(t)
	a, m := privacyApp(t, srv, true)
	connect(t, a, m, true)
	m.Apply(event(t, proto.EventTrackStarted, proto.TrackStartedEvent{Track: proto.Track{ID: "s-1", Artist: "Neon Harbor", Title: "Midnight Ferry", LengthMs: 200000},
		Cut: &proto.TrackCut{Kind: "boundary", By: "silence", OffsetMs: -480}}))
	m.Apply(event(t, proto.EventTrackFinished, proto.TrackFinishedEvent{ID: "s-1", Path: "/music/Neon Harbor - Midnight Ferry.flac", Timings: &proto.FinishTimings{Format: "flac", EncodeMs: 900, TotalMs: 1000}}))
	m.Apply(event(t, proto.EventTrackSkipped, proto.TrackSkippedEvent{ID: "s-2", Reason: proto.SkipAd}))
	a.tel.Flush(2 * time.Second)
	got := map[string]fakesentry.Metric{}
	deadline := time.Now().Add(5 * time.Second)
	for len(got) < 3 && time.Now().Before(deadline) {
		for _, mt := range srv.Metrics() {
			got[mt.Name] = mt
		}
		time.Sleep(20 * time.Millisecond)
	}
	if c := got["cut.offset"]; c.Type != "distribution" || c.Value != -480 || c.Attrs["kind"] != "boundary" || c.Attrs["by"] != "silence" {
		t.Errorf("cut.offset %+v", c)
	}
	if s := got["track.saved"]; s.Type != "counter" || s.Value != 1 || s.Attrs["format"] != "flac" {
		t.Errorf("track.saved %+v", s)
	}
	if s := got["track.skipped"]; s.Type != "counter" || s.Value != 1 || s.Attrs["reason"] != "ad" {
		t.Errorf("track.skipped %+v", s)
	}
	for _, mt := range got {
		for k, v := range mt.Attrs {
			if strings.Contains(v, "Neon") || strings.Contains(v, "Midnight") || strings.Contains(v, "/music") {
				t.Errorf("%s names the track in %s: %q", mt.Name, k, v)
			}
		}
	}
}

// The diagnostics offer to send the checks only when one fails, and send
// them on the click, with the comment.
func TestSendToTheDevelopers(t *testing.T) {
	srv := fakesentry.New(t)
	a, m := privacyApp(t, srv, true)
	connect(t, a, m, true)
	a.showTab(tabLogs)
	a.render()
	if b := buttons(diagnostics(t, a), "Send to the developers"); len(b) != 0 {
		t.Fatal("a Send button while every check passes")
	}
	failCheck(a, m)
	snapshot(t, a, "privacy-diagnostics")
	if s := a.screens[tabLogs].(*logsScreen).checksScroll; s.Content.Size().Height == 0 || s.Offset.Y+s.Size().Height < s.Content.Size().Height {
		t.Error("the checks are not scrolled down to the one that fails")
	}
	if send := sendButton(t, a); send.Disabled() || contains(texts(overlay(t, a)), optedOut) {
		t.Fatalf("Send to the developers: disabled %v, the dialog %v", send.Disabled(), texts(overlay(t, a)))
	}
	tapButton(t, overlay(t, a), "Send to the developers")
	snapshot(t, a, "privacy-send-checks")
	tapButton(t, overlay(t, a), "Send")
	ev := srv.Wait(t, "event", 1)[0].JSON()
	if ev["message"] != "stuck: ffmpeg found" {
		t.Errorf("sent %v", ev["message"])
	}
}

// The output folder leaves a report as <output>: its name is the user's
// own, which can be theirs.
func TestReportsLeaveTheOutputFolderOut(t *testing.T) {
	srv := fakesentry.New(t)
	a, m := privacyApp(t, srv, true)
	connect(t, a, m, true)
	dir := filepath.Join(t.TempDir(), "Mixtapes")
	m.Apply(event(t, proto.EventSettingsChanged, proto.SettingsValues{Values: map[string]any{"output.dir": dir}}))
	a.render()
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateRecording}))
	m.Apply(event(t, proto.EventError, proto.ErrorEvent{Code: "temp_file", Msg: "open " + filepath.Join(dir, ".spytify", "a.wav") + ": not found"}))
	ev := srv.Wait(t, "event", 1)[0].JSON()
	contexts, _ := ev["contexts"].(map[string]any)
	problem, _ := contexts["problem"].(map[string]any)
	if want := "open " + filepath.Join("<output>", ".spytify", "a.wav") + ": not found"; problem["detail"] != want {
		t.Errorf("detail %v, want %q", problem["detail"], want)
	}
}

// An output device too poor to record from, a Bluetooth headset in a
// call, is the user's to change, not a bug: it leaves no report.
func TestALowQualityDeviceLeavesNoReport(t *testing.T) {
	srv := fakesentry.New(t)
	a, m := privacyApp(t, srv, true)
	connect(t, a, m, true)
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateWaiting}))
	m.Apply(event(t, proto.EventError, proto.ErrorEvent{Code: "capture_low_quality", Msg: "The player plays to a device at 16 kHz mono"}))
	m.Apply(event(t, proto.EventError, proto.ErrorEvent{Code: "capture_low_quality"}))
	m.Apply(event(t, proto.EventError, proto.ErrorEvent{Code: "capture_stalled", Msg: "No audio from the player for 10 s"}))
	srv.Wait(t, "event", 1)
	quiet(a, srv)
	for _, item := range srv.Items("event") {
		if ev := item.JSON(); ev["message"] != "blocked: capture_stalled" {
			t.Errorf("reported %v", ev["message"])
		}
	}
}
