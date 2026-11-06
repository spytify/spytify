package ui

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/telemetry/fakesentry"
	"github.com/spytify/spytify/internal/ui/lang"
	"github.com/spytify/spytify/internal/ui/link"
	"github.com/spytify/spytify/internal/ui/model"
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
	found := buttons(overlay(t, a), lang.SendToDevelopers)
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
		errNoAnswer:       "no answer",
		&fs.PathError{Op: "fork/exec", Path: "/x/spytifyd", Err: fs.ErrNotExist}: "not found",
		errors.New("spytifyd stopped: listen: address already in use"):           "failed",
		// What spytifyd said as it stopped is its own: it found itself.
		errors.New("spytifyd stopped: ffmpeg not found"):               "failed",
		errors.New("spytifyd stopped: open config.toml: no such file"): "failed",
	} {
		if got := startErrorKind(err); got != want {
			t.Errorf("startErrorKind(%q) = %q, want %q", err, got, want)
		}
	}
}

// A recorder found nowhere leaves its kind with what its start saw, the
// lines its log got since and not those before, which can name tracks, and
// no path.
func TestARecorderThatDidNotStartLeavesWhatItsStartSaw(t *testing.T) {
	srv := fakesentry.New(t)
	a, m := privacyApp(t, srv, true)
	connect(t, a, m, true)
	t.Setenv("PATH", t.TempDir())
	log := filepath.Join(a.opts.StateDir, "spytifyd.log")
	old := time.Now().Add(-time.Hour).Format(time.RFC3339Nano) + " info  saved Neon Harbor - Midnight Ferry\n"
	if err := os.WriteFile(log, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	err := a.link.StartDaemon()
	f, ferr := os.OpenFile(log, os.O_APPEND|os.O_WRONLY, 0)
	if ferr != nil {
		t.Fatal(ferr)
	}
	fmt.Fprintf(f, "%s error spytifyd: config: player not set\n", time.Now().Format(time.RFC3339Nano))
	f.Close()
	a.startFailed(err)
	a.reports.Wait()
	ev := srv.Wait(t, "event", 1)[0].JSON()
	if ev["message"] != "blocked: recorder_start" {
		t.Errorf("sent %v", ev["message"])
	}
	contexts, _ := ev["contexts"].(map[string]any)
	problem, _ := contexts["problem"].(map[string]any)
	if d := problem["detail"]; d != "not found" && d != "in archive" {
		t.Errorf("detail %v", d)
	}
	if problem["start.found"] != "none" || problem["start.started"] != "false" || problem["start.socket_exists"] != "false" || problem["start.waited_ms"] == nil {
		t.Errorf("problem %v", problem)
	}
	tail, _ := problem["start.log_tail"].(string)
	if !strings.Contains(tail, "player not set") || strings.Contains(tail, "Neon Harbor") {
		t.Errorf("log tail %q", tail)
	}
	for k, v := range problem {
		if s, _ := v.(string); strings.Contains(s, a.opts.StateDir) {
			t.Errorf("%s holds a path: %q", k, s)
		}
	}
}

// A recorder that stopped at once leaves how, and the last 5 lines it
// wrote, their end within 1000 bytes.
func TestStartFactsOfARecorderThatStopped(t *testing.T) {
	a, _, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	var stderr []string
	for i := 1; i <= 8; i++ {
		stderr = append(stderr, fmt.Sprintf("spytifyd: line %d", i))
	}
	stderr = append(stderr, "spytifyd: "+strings.Repeat("é", 600))
	facts := a.startFacts(link.Start{At: time.Now(), Found: "next to the app", ExeBytes: 9 << 20, Started: true, Exited: true,
		ExitCode: 3221225781, Err: "exit status 3221225781", Stderr: strings.Join(stderr, "\r\n") + "\r\n"}, 40*time.Millisecond)
	for k, want := range map[string]string{
		"start.found": "next to the app", "start.exe_bytes": "9437184", "start.started": "true", "start.exited": "true",
		"start.exit_code": "3221225781", "start.error": "exit status 3221225781", "start.waited_ms": "40",
	} {
		if facts[k] != want {
			t.Errorf("%s = %q, want %q", k, facts[k], want)
		}
	}
	if _, ok := facts["start.errno"]; ok {
		t.Error("an errno with none")
	}
	if _, ok := facts["start.log_tail"]; ok {
		t.Error("a log tail with no log")
	}
	got := facts["start.stderr"]
	if len(got) > 1000 || !utf8.ValidString(got) || strings.Contains(got, "line 5") || !strings.HasSuffix(got, "éé") {
		t.Errorf("stderr %d bytes: %q", len(got), got)
	}
	if got := lastLines("a\nb\nc\nd\ne\nf\n", 5, 1000); got != "b\nc\nd\ne\nf" {
		t.Errorf("last lines %q", got)
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
	eventually(t, "fewer than 3 metrics", func() bool {
		for _, mt := range srv.Metrics() {
			got[mt.Name] = mt
		}
		return len(got) >= 3
	})
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
	if b := buttons(diagnostics(t, a), lang.SendToDevelopers); len(b) != 0 {
		t.Fatal("a Send button while every check passes")
	}
	failCheck(a, m)
	snapshot(t, a, "privacy-diagnostics")
	if s := a.screens[tabLogs].(*logsScreen).checksScroll; s.Content.Size().Height == 0 || s.Offset.Y+s.Size().Height < s.Content.Size().Height {
		t.Error("the checks are not scrolled down to the one that fails")
	}
	if send := sendButton(t, a); send.Disabled() || contains(texts(overlay(t, a)), lang.TrackerOptedOut) {
		t.Fatalf("Send to the developers: disabled %v, the dialog %v", send.Disabled(), texts(overlay(t, a)))
	}
	tapButton(t, overlay(t, a), lang.SendToDevelopers)
	snapshot(t, a, "privacy-send-checks")
	tapButton(t, overlay(t, a), lang.Send)
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
	m.Apply(event(t, proto.EventError, proto.ErrorEvent{Code: proto.ErrorTempFile, Msg: "open " + filepath.Join(dir, ".spytify", "a.wav") + ": not found"}))
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
	m.Apply(event(t, proto.EventError, proto.ErrorEvent{Code: proto.ErrorCaptureLowQuality, Msg: "The player plays to a device at 16 kHz mono"}))
	m.Apply(event(t, proto.EventError, proto.ErrorEvent{Code: proto.ErrorCaptureLowQuality}))
	m.Apply(event(t, proto.EventError, proto.ErrorEvent{Code: proto.ErrorCaptureStalled, Msg: "No audio from the player for 10 s"}))
	srv.Wait(t, "event", 1)
	quiet(a, srv)
	for _, item := range srv.Items("event") {
		if ev := item.JSON(); ev["message"] != "blocked: capture_stalled" {
			t.Errorf("reported %v", ev["message"])
		}
	}
}

// Every launch says where the output folder is, written to or not: what
// share of them a fix would reach.
func TestTheLaunchSaysWhereTheOutputFolderIs(t *testing.T) {
	srv := fakesentry.New(t)
	a, m := privacyApp(t, srv, true)
	in := info()
	in.Checks = append(in.Checks, proto.Check{ID: proto.CheckOutputWritable, Name: "output folder writable", OK: true, Detail: "/music",
		Facts: map[string]string{"output.drive": "fixed", "output.cloud": "OneDrive"}})
	m.SetConn(model.Connected, "")
	m.SetInfo(in)
	values := defaults()
	values[trackerKey] = true
	m.SetSettings(values)
	a.render()
	a.tel.Flush(2 * time.Second)
	if l := launchLog(t, srv); l.Attrs["output.drive"] != "fixed" || l.Attrs["output.cloud"] != "OneDrive" {
		t.Errorf("launch %v", l.Attrs)
	}
}
