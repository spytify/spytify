package ui

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/spytify/spytify/internal/ui/lang"
	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

// alreadyRecordedSwitches are the two switches about tracks already
// recorded: skipping to the player's next one, and stopping the session.
func alreadyRecordedSwitches(t *testing.T, s *settingsScreen) (next, stop *toggle) {
	t.Helper()
	next, ok1 := s.fields["recorder.skip_duplicates_forward"].input.(*toggle)
	stop, ok2 := s.fields["recorder.stop_at_recorded"].input.(*toggle)
	if !ok1 || !ok2 {
		t.Fatal("no switch for the tracks already recorded")
	}
	return next, stop
}

// The switches about tracks already recorded mean something only while
// such tracks are skipped: with Save a copy or Replace the file, both are
// off and greyed out, even when the settings say on (#758).
func TestTheAlreadyRecordedSwitchesNeedSkipping(t *testing.T) {
	a, m, s := settingsApp(t)
	next, stop := alreadyRecordedSwitches(t, s)
	for _, c := range []struct {
		duplicates, shown string
		works             bool
		snap              string
	}{
		{"skip", "Don't save again", true, "settings-already-recorded"},
		{"overwrite", "Replace the file", false, "settings-already-recorded-replace"},
		{"suffix", "Save a copy", false, ""},
	} {
		v := defaults()
		v["recorder.duplicates"] = c.duplicates
		v["recorder.skip_duplicates_forward"] = true
		v["recorder.stop_at_recorded"] = true
		m.SetSettings(v)
		s.search.SetText("recorded")
		a.render()
		if sel := s.fields["recorder.duplicates"].input.(*selectField); sel.Selected != c.shown {
			t.Errorf("%s shows %q, want %q", c.duplicates, sel.Selected, c.shown)
		}
		for name, sw := range map[string]*toggle{"skip to the next track": next, "stop the session": stop} {
			if sw.Disabled() == c.works || sw.on != c.works {
				t.Errorf("with %s, %s: disabled %v, on %v", c.duplicates, name, sw.Disabled(), sw.on)
			}
		}
		if c.snap != "" {
			snapshot(t, a, c.snap)
		}
	}
	light := defaults()
	light["app.theme"] = "light"
	light["recorder.stop_at_recorded"] = true
	m.SetSettings(light)
	a.render()
	snapshot(t, a, "settings-already-recorded-light")
	light["recorder.duplicates"] = "overwrite"
	m.SetSettings(light)
	a.render()
	snapshot(t, a, "settings-already-recorded-replace-light")
}

// Choosing Save a copy or Replace the file turns both switches off in the
// settings, so they stay off once Don't save again is chosen back.
func TestChoosingToSaveAgainTurnsTheAlreadyRecordedSwitchesOff(t *testing.T) {
	a, _, core := connectedApp(t)
	a.showTab(tabSettings)
	a.render()
	s := a.screens[tabSettings].(*settingsScreen)
	sel := s.fields["recorder.duplicates"].input.(*selectField)
	sel.SetSelected("Replace the file")
	waitSets(t, core, 1)
	sel.SetSelected("Don't save again")
	waitSets(t, core, 2)
	core.mu.Lock()
	sets := core.sets
	core.mu.Unlock()
	want := []map[string]any{
		{"recorder.duplicates": "overwrite", "recorder.skip_duplicates_forward": false, "recorder.stop_at_recorded": false},
		{"recorder.duplicates": "skip"},
	}
	if fmt.Sprint(sets) != fmt.Sprint(want) {
		t.Errorf("settings.set sent %v, want %v", sets, want)
	}
}

// A session that ended at a track already recorded says so, with its
// playlist when it wrote one.
func TestASessionEndedAtATrackAlreadyRecordedSaysSo(t *testing.T) {
	list := filepath.Join("/Users/listener/Spytify", "Playlists", "2026-11-08 21.00.m3u8")
	for _, c := range []struct {
		playlist, want, snap string
		light                bool
	}{
		{"", lang.SessionEndedRecorded, "record-ended-at-recorded", false},
		{"", lang.SessionEndedRecorded, "record-ended-at-recorded-light", true},
		{list, fmt.Sprintf(lang.SessionEndedRecordedList, filepath.Join("Playlists", "2026-11-08 21.00.m3u8")), "record-ended-at-recorded-playlist", false},
	} {
		a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
		m.SetConn(model.Connected, "")
		m.SetInfo(info())
		v := defaults()
		if c.light {
			v["app.theme"] = "light"
		}
		m.SetSettings(v)
		m.Apply(event(t, proto.EventTargetState, proto.TargetStateEvent{Running: true, PID: 4242}))
		m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateRecording}))
		// The playlist's two newest tracks are new; the third is already
		// recorded, and the session stops there.
		for i, title := range []string{"Midnight City", "Reunion"} {
			id := "s-00" + string(rune('1'+i))
			m.Apply(event(t, proto.EventTrackStarted, proto.TrackStartedEvent{Track: proto.Track{ID: id, Artist: "M83", Title: title, Album: "Hurry Up, We're Dreaming", LengthMs: 243000 - int64(i)*20000, CoverURL: generatedCover(t, i+1)}}))
			m.Apply(event(t, proto.EventTrackFinished, proto.TrackFinishedEvent{ID: id, Path: "/Users/listener/Spytify/M83/" + title + ".mp3", DurationMs: 243000 - int64(i)*20000, Bytes: 4_800_000, Tagged: true}))
		}
		m.Apply(event(t, proto.EventTrackStarted, proto.TrackStartedEvent{Track: proto.Track{ID: "s-003", Artist: "M83", Title: "Wait", Album: "Hurry Up, We're Dreaming", LengthMs: 174000, CoverURL: generatedCover(t, 3)}}))
		m.Apply(event(t, proto.EventTrackSkipped, proto.TrackSkippedEvent{ID: "s-003", Reason: proto.SkipDuplicate, Path: "/Users/listener/Spytify/M83/Wait.mp3"}))
		m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateIdle, Playlist: c.playlist, AtRecorded: true}))
		a.render()
		if got := a.screens[tabRecord].(*recordScreen).waiting.String(); got != c.want {
			t.Errorf("the alert says %q, want %q", got, c.want)
		}
		snapshot(t, a, c.snap)
		// The next session, an ad alone in it, ends without it.
		m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateWaiting}))
		m.Apply(event(t, proto.EventTrackStarted, proto.TrackStartedEvent{Track: proto.Track{ID: "s-101", Artist: "Sponsor", Title: "Sponsored", LengthMs: 30000}}))
		m.Apply(event(t, proto.EventTrackSkipped, proto.TrackSkippedEvent{ID: "s-101", Reason: proto.SkipAd}))
		m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateIdle}))
		a.render()
		if got := a.screens[tabRecord].(*recordScreen).waiting.String(); got != lang.SessionEnded {
			t.Errorf("the next session's end says %q, want %q", got, lang.SessionEnded)
		}
	}
}
