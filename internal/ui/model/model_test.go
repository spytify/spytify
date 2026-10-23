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
	if s.Rows[0].Status != Tagged || s.Rows[0].Path != "/m/One.flac" {
		t.Fatalf("finished row: %+v", s.Rows[0])
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

func TestTrackStartedKeepsTheSourceFormat(t *testing.T) {
	m := New()
	m.Apply(ev(t, proto.EventTrackStarted, proto.TrackStartedEvent{Track: proto.Track{ID: "s-001", Title: "A"}, Source: &proto.AudioSource{SampleRate: 44100, Channels: 2}}))
	if c := m.Snapshot().Current; c == nil || c.SourceRate != 44100 || c.SourceChannels != 2 {
		t.Fatalf("current %+v", c)
	}
}
