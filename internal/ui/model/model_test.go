package model

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/spytify/spytify/pkg/proto"
)

func ev(t *testing.T, method string, payload any) proto.Event {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return proto.Event{Method: method, Params: raw}
}

func started(t *testing.T, id, title string) proto.Event {
	return ev(t, proto.EventTrackStarted, proto.TrackStartedEvent{Track: proto.Track{ID: id, Artist: "Artist", Title: title, Album: "Album", LengthMs: 180000}})
}

func TestSessionList(t *testing.T) {
	m := New()
	clock := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return clock }
	changes := 0
	m.OnChange(func() { changes++ })

	m.Apply(ev(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateWaiting}))
	m.Apply(started(t, "s-001", "One"))
	s := m.Snapshot()
	if !s.Recording() || s.Current == nil || s.Current.Title != "One" || s.Current.Status != Recording {
		t.Fatalf("after start: %+v", s)
	}

	m.Apply(started(t, "s-002", "Two"))
	s = m.Snapshot()
	if s.Current.Title != "Two" || len(s.Rows) != 1 || s.Rows[0].Status != Saving {
		t.Fatalf("after second start: current %+v rows %+v", s.Current, s.Rows)
	}

	m.Apply(ev(t, proto.EventTrackFinished, proto.TrackFinishedEvent{ID: "s-001", Path: "/m/One.flac", DurationMs: 179000, Bytes: 20e6, Tagged: true, TaggedBy: "lastfm"}))
	m.Apply(ev(t, proto.EventTrackSkipped, proto.TrackSkippedEvent{ID: "s-002", Reason: proto.SkipAd}))
	s = m.Snapshot()
	if s.Rows[0].Status != Saved || s.Rows[0].Path != "/m/One.flac" {
		t.Fatalf("finished row: %+v", s.Rows[0])
	}
	if got := s.Rows[0].StatusText(); got != "Saved" {
		t.Fatalf("a track tagged online shows %q, want Saved", got)
	}
	if s.Current == nil || s.Current.StatusText() != "Skipped: ad" {
		t.Fatalf("a skipped current track stays current: %+v", s.Current)
	}

	// The skipped item ends and the core waits: the card empties.
	m.Apply(ev(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateWaiting}))
	if s = m.Snapshot(); s.Current != nil || s.Rows[0].Title != "Two" {
		t.Fatalf("waiting kept the skipped card: %+v", s.Current)
	}

	m.Apply(started(t, "s-003", "Three"))
	m.Apply(ev(t, proto.EventTrackFinished, proto.TrackFinishedEvent{ID: "s-003", Path: "/m/Three.flac", Bytes: 1e6, Tagged: true, TaggedBy: "local"}))
	s = m.Snapshot()
	if s.Current != nil {
		t.Fatalf("a finished current track leaves the card: %+v", s.Current)
	}
	if got := []string{s.Rows[0].Title, s.Rows[1].Title, s.Rows[2].Title}; got[0] != "Three" || got[1] != "Two" || got[2] != "One" {
		t.Fatalf("rows not newest first: %v", got)
	}
	if s.Rows[0].Status != Saved {
		t.Fatalf("locally tagged file shows %q, want Saved", s.Rows[0].Status)
	}
	if tot := s.Totals(); tot.Saved != 2 || tot.Skipped != 1 || tot.Ads != 1 || tot.Bytes != 21e6 {
		t.Fatalf("totals %+v", tot)
	}

	m.Apply(ev(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateIdle}))
	if s = m.Snapshot(); s.Recording() || len(s.Rows) != 3 {
		t.Fatalf("idle keeps the list: %+v", s)
	}
	m.Apply(ev(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateWaiting}))
	if s = m.Snapshot(); len(s.Rows) != 0 {
		t.Fatalf("a new session starts a new list: %d rows", len(s.Rows))
	}
	if changes == 0 {
		t.Fatal("listeners not called")
	}
}

func TestReplayRebuildsTheSessionForALateClient(t *testing.T) {
	m := New()
	m.Replay([]proto.Event{
		ev(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateRecording}),
		started(t, "a", "First"),
		started(t, "b", "Second"),
		ev(t, proto.EventTrackSkipped, proto.TrackSkippedEvent{ID: "a", Reason: proto.SkipTooShort}),
		ev(t, proto.EventLog, proto.LogEvent{Level: "info", Msg: "replayed logs are ignored"}),
	})
	s := m.Snapshot()
	if s.Current == nil || s.Current.Title != "Second" || len(s.Rows) != 1 || s.Rows[0].StatusText() != "Skipped: too short" {
		t.Fatalf("state %+v", s)
	}
	if len(s.Logs) != 0 {
		t.Fatal("replay added log lines that log.tail already returned")
	}
}

// A take cut short offers Keep anyway until it is let go; kept, it is saving
// until its file reads "Saved: partial".
func TestKeepableTakes(t *testing.T) {
	m := New()
	m.Apply(ev(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateRecording}))
	m.Apply(started(t, "a", "First"))
	m.Apply(ev(t, proto.EventTrackSkipped, proto.TrackSkippedEvent{ID: "a", Reason: proto.SkipIncomplete}))
	m.Apply(started(t, "b", "Second"))
	m.Apply(ev(t, proto.EventTrackKeepable, proto.TrackKeepableEvent{ID: "a", Keepable: true}))
	if s := m.Snapshot(); len(s.Rows) != 1 || !s.Rows[0].Keepable {
		t.Fatalf("rows %+v", s.Rows)
	}

	m.Keeping("a", true)
	if r := m.Snapshot().Rows[0]; r.Keepable || r.Status != Saving {
		t.Fatalf("keeping %+v", r)
	}
	m.Keeping("a", false)
	if r := m.Snapshot().Rows[0]; r.Status != Skipped || r.StatusText() != "Skipped: incomplete" {
		t.Fatalf("not kept %+v", r)
	}

	m.Keeping("a", true)
	m.Apply(ev(t, proto.EventTrackKeepable, proto.TrackKeepableEvent{ID: "a"}))
	m.Apply(ev(t, proto.EventTrackFinished, proto.TrackFinishedEvent{ID: "a", Path: "/m/First (partial).flac", Partial: true}))
	if r := m.Snapshot().Rows[0]; r.Keepable || r.StatusText() != "Saved: partial" {
		t.Fatalf("kept %+v", r)
	}

	// Let go: the button goes, the row stays skipped.
	m.Apply(ev(t, proto.EventTrackSkipped, proto.TrackSkippedEvent{ID: "b", Reason: proto.SkipSeeked}))
	m.Apply(started(t, "c", "Third"))
	m.Apply(ev(t, proto.EventTrackKeepable, proto.TrackKeepableEvent{ID: "b", Keepable: true}))
	m.Apply(ev(t, proto.EventTrackKeepable, proto.TrackKeepableEvent{ID: "b"}))
	for _, r := range m.Snapshot().Rows {
		if r.ID == "b" && (r.Keepable || r.StatusText() != "Skipped: track bar moved") {
			t.Fatalf("let go %+v", r)
		}
	}
}

// A take in low quality says so, offers Keep anyway like a take cut short,
// and once kept reads "Saved: low quality", not partial.
func TestALowQualityTakeSaysSo(t *testing.T) {
	m := New()
	m.Apply(ev(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateRecording}))
	m.Apply(started(t, "a", "First"))
	m.Apply(started(t, "b", "Second"))
	m.Apply(ev(t, proto.EventTrackSkipped, proto.TrackSkippedEvent{ID: "a", Reason: proto.SkipLowQuality}))
	m.Apply(ev(t, proto.EventTrackKeepable, proto.TrackKeepableEvent{ID: "a", Keepable: true}))
	if r := m.Snapshot().Rows[0]; !r.Keepable || r.StatusText() != "Skipped: low quality" {
		t.Fatalf("skipped %+v, %q", r, r.StatusText())
	}
	m.Keeping("a", true)
	m.Apply(ev(t, proto.EventTrackKeepable, proto.TrackKeepableEvent{ID: "a"}))
	m.Apply(ev(t, proto.EventTrackFinished, proto.TrackFinishedEvent{ID: "a", Path: "/m/First (low quality).flac", Partial: true}))
	if r := m.Snapshot().Rows[0]; r.StatusText() != "Saved: low quality" {
		t.Fatalf("kept %+v, %q", r, r.StatusText())
	}
}

func TestSettingsLogsAndErrors(t *testing.T) {
	m := New()
	m.SetSettings(map[string]any{"encode.format": "mp3", "output.dir": "/m"})
	m.Apply(ev(t, proto.EventSettingsChanged, proto.SettingsValues{Values: map[string]any{"encode.format": "flac"}}))
	s := m.Snapshot()
	if s.Settings["encode.format"] != "flac" || s.Settings["output.dir"] != "/m" {
		t.Fatalf("settings %v", s.Settings)
	}
	s.Settings["encode.format"] = "changed by the caller"
	if m.Snapshot().Settings["encode.format"] != "flac" {
		t.Fatal("Snapshot shares the settings map")
	}
	for i := 0; i < maxLogs+10; i++ {
		m.Apply(ev(t, proto.EventLog, proto.LogEvent{Level: "info", Msg: "line"}))
	}
	if n := len(m.Snapshot().Logs); n != maxLogs {
		t.Fatalf("%d log lines kept", n)
	}
	m.Apply(ev(t, proto.EventError, proto.ErrorEvent{Code: "disk_full", Msg: "only 10 MB free"}))
	if e := m.Snapshot().LastError; e == nil || e.Code != "disk_full" {
		t.Fatalf("last error %+v", e)
	}
	m.Apply(ev(t, proto.EventTargetState, proto.TargetStateEvent{Running: true, PID: 42}))
	if s := m.Snapshot(); !s.TargetKnown || !s.Target.Running || s.Target.PID != 42 {
		t.Fatalf("target %+v", s.Target)
	}
}

func TestStatusSnapshotForALateClient(t *testing.T) {
	m := New()
	m.SetStatus(proto.SessionStatusResult{State: proto.StateRecording, SessionID: "x", Track: &proto.Track{ID: "x-004", Title: "Now"}})
	s := m.Snapshot()
	if s.Current == nil || s.Current.ID != "x-004" || s.SessionID != "x" {
		t.Fatalf("state %+v", s)
	}
}

func TestElapsedIsCappedAtLength(t *testing.T) {
	m := New()
	start := time.Now()
	m.now = func() time.Time { return start.Add(10 * time.Minute) }
	if got := m.Elapsed(Row{Started: start, LengthMs: 180000}); got != 3*time.Minute {
		t.Fatalf("elapsed %v", got)
	}
}

func state(t *testing.T, st proto.SessionState) proto.Event {
	return ev(t, proto.EventSessionState, proto.SessionStateEvent{State: st})
}

// Paused at 0:59 for 20 s, the track showed 1:19 once resumed while the
// player was at 1:00: the core cuts the pause out of the take.
func TestElapsedLeavesPausesOut(t *testing.T) {
	m := New()
	clock := time.Date(2026, 11, 1, 13, 37, 48, 0, time.UTC)
	m.now = func() time.Time { return clock }
	elapsed := func() time.Duration { c, _, _ := m.Live(); return m.Elapsed(*c) }

	m.Apply(state(t, proto.StateRecording))
	m.Apply(started(t, "s-001", "One"))
	clock = clock.Add(59 * time.Second)
	m.Apply(state(t, proto.StatePaused))
	clock = clock.Add(20 * time.Second)
	if got := elapsed(); got != 59*time.Second {
		t.Fatalf("paused for 20 s: %v, want 59s", got)
	}
	m.Apply(state(t, proto.StateRecording))
	clock = clock.Add(time.Second)
	if got := elapsed(); got != time.Minute {
		t.Fatalf("a second after resuming: %v, want 1m0s", got)
	}
	m.Apply(state(t, proto.StatePaused))
	clock = clock.Add(2 * time.Minute)
	m.Apply(state(t, proto.StateRecording))
	clock = clock.Add(10 * time.Second)
	if got := elapsed(); got != 70*time.Second {
		t.Fatalf("after a second pause: %v, want 1m10s", got)
	}
}

func TestElapsedOfATrackStartedWhilePaused(t *testing.T) {
	m := New()
	clock := time.Date(2026, 11, 1, 13, 37, 48, 0, time.UTC)
	m.now = func() time.Time { return clock }
	elapsed := func() time.Duration { c, _, _ := m.Live(); return m.Elapsed(*c) }

	m.Apply(state(t, proto.StateRecording))
	m.Apply(state(t, proto.StatePaused))
	m.Apply(started(t, "s-001", "One"))
	clock = clock.Add(30 * time.Second)
	if got := elapsed(); got != 0 {
		t.Fatalf("started while paused: %v, want 0s", got)
	}
	m.Apply(state(t, proto.StateRecording))
	clock = clock.Add(5 * time.Second)
	if got := elapsed(); got != 5*time.Second {
		t.Fatalf("after resuming: %v, want 5s", got)
	}

	// A client that connects during a pause starts the clock stopped too.
	late := New()
	late.now = m.now
	late.SetStatus(proto.SessionStatusResult{State: proto.StatePaused, SessionID: "x", Track: &proto.Track{ID: "x-004", Title: "Now"}})
	clock = clock.Add(time.Minute)
	if c, _, _ := late.Live(); late.Elapsed(*c) != 0 {
		t.Fatalf("snapshot while paused: %v, want 0s", late.Elapsed(*c))
	}
}

func TestSortRows(t *testing.T) {
	now := time.Now()
	rows := []Row{{Title: "b", Artist: "Z", Started: now}, {Title: "a", Artist: "Y", Started: now.Add(time.Minute)}}
	if got := SortRows(rows, "date"); got[0].Title != "a" {
		t.Fatalf("date: %v", got)
	}
	if got := SortRows(rows, "artist"); got[0].Artist != "Y" {
		t.Fatalf("artist: %v", got)
	}
	if rows[0].Title != "b" {
		t.Fatal("SortRows changed its input")
	}
}

func TestClearRowsSurvivesAReplay(t *testing.T) {
	m := New()
	events := []proto.Event{
		ev(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateRecording}),
		started(t, "s-001", "One"),
		ev(t, proto.EventTrackFinished, proto.TrackFinishedEvent{ID: "s-001", Path: "/m/One.flac"}),
		started(t, "s-002", "Two"),
	}
	m.Replay(events)
	m.ClearRows()
	s := m.Snapshot()
	if len(s.Rows) != 0 || s.Current == nil || s.Current.Title != "Two" {
		t.Fatalf("after clear: rows %+v current %+v", s.Rows, s.Current)
	}
	// A reconnect replays the same events: the cleared row stays gone.
	m.Replay(events)
	if s = m.Snapshot(); len(s.Rows) != 0 {
		t.Fatalf("replay brought back %+v", s.Rows)
	}
	m.Apply(started(t, "s-003", "Three"))
	if s = m.Snapshot(); len(s.Rows) != 1 || s.Rows[0].Title != "Two" {
		t.Fatalf("a later track: rows %+v", s.Rows)
	}
}

func TestAudioBackClearsTheNoAudioWarning(t *testing.T) {
	m := New()
	m.Apply(ev(t, proto.EventError, proto.ErrorEvent{Code: "capture_stalled", Msg: "no audio"}))
	m.Apply(ev(t, proto.EventLevel, proto.LevelEvent{}))
	if e := m.Snapshot().LastError; e != nil {
		t.Fatalf("warning kept after audio came back: %+v", e)
	}
	m.Apply(ev(t, proto.EventError, proto.ErrorEvent{Code: "disk_full", Msg: "only 10 MB free"}))
	m.Apply(ev(t, proto.EventLevel, proto.LevelEvent{}))
	if e := m.Snapshot().LastError; e == nil || e.Code != "disk_full" {
		t.Fatalf("other errors must stay: %+v", e)
	}
}

// The warning of a capture too poor to record from holds while audio
// flows, and goes when the core withdraws it: the same code, no message.
func TestAGoodCaptureAgainClearsTheLowQualityWarning(t *testing.T) {
	m := New()
	m.Apply(ev(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateWaiting}))
	m.Apply(ev(t, proto.EventError, proto.ErrorEvent{Code: "capture_low_quality", Msg: "16 kHz mono"}))
	m.Apply(ev(t, proto.EventLevel, proto.LevelEvent{}))
	if e := m.Snapshot().LastError; e == nil || e.Code != "capture_low_quality" {
		t.Fatalf("warning %+v", e)
	}
	m.Apply(ev(t, proto.EventError, proto.ErrorEvent{Code: "capture_low_quality"}))
	if e := m.Snapshot().LastError; e != nil {
		t.Fatalf("warning kept with the capture fit again: %+v", e)
	}
	m.Apply(ev(t, proto.EventError, proto.ErrorEvent{Code: "disk_full", Msg: "only 10 MB free"}))
	m.Apply(ev(t, proto.EventError, proto.ErrorEvent{Code: "capture_low_quality"}))
	if e := m.Snapshot().LastError; e == nil || e.Code != "disk_full" || e.Msg != "only 10 MB free" {
		t.Fatalf("other errors must stay: %+v", e)
	}
}

func TestTrackStartedKeepsTheSourceFormat(t *testing.T) {
	m := New()
	m.Apply(ev(t, proto.EventTrackStarted, proto.TrackStartedEvent{Track: proto.Track{ID: "s-001", Title: "A"}, Source: &proto.AudioSource{SampleRate: 44100, Channels: 2}}))
	if c := m.Snapshot().Current; c == nil || c.SourceRate != 44100 || c.SourceChannels != 2 {
		t.Fatalf("current %+v", c)
	}
}

// QA found the app taking a whole core while recording: every 10 Hz level
// event ran a full redraw. Level events reach only the level listeners.
func TestLevelEventsDoNotRedrawTheWindow(t *testing.T) {
	m := New()
	var full, level int
	m.OnChange(func() { full++ })
	m.OnLevel(func() { level++ })
	m.Apply(started(t, "s-001", "A"))
	full = 0
	for i := 0; i < 50; i++ {
		m.Apply(ev(t, proto.EventLevel, proto.LevelEvent{RMSDBFS: -20}))
	}
	if full != 0 || level != 50 {
		t.Fatalf("50 level events ran %d full redraws and %d level updates", full, level)
	}
}
