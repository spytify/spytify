package ui

import (
	"fmt"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/spytify/spytify/internal/ui/lang"
	"github.com/spytify/spytify/pkg/proto"
)

// Write a playlist for each session is off by default, in Output under
// Group by folders, out of Advanced.
func TestThePlaylistSwitchIsUnderGroupByFolders(t *testing.T) {
	a, _, s := settingsApp(t)
	f := s.fields["output.playlist"]
	if f == nil {
		t.Fatal("no field for output.playlist")
	}
	if tg, ok := f.input.(*toggle); !ok || tg.on {
		t.Fatalf("output.playlist is a %T, on %v; want a switch, off", f.input, ok && tg.on)
	}
	if folded(s, "output", f.row) {
		t.Error("under Advanced")
	}
	d := fyne.CurrentApp().Driver()
	y := func(key string) float32 { return d.AbsolutePositionForObject(s.fields[key].row).Y }
	advanced := d.AbsolutePositionForObject(s.advanced["output"].header).Y
	if !(y("output.group_by") < y("output.playlist") && y("output.playlist") < advanced) {
		t.Errorf("Group by folders at %v, the playlist at %v, Advanced at %v", y("output.group_by"), y("output.playlist"), advanced)
	}
	// The Output card at the top of the window.
	s.scroll.Offset = fyne.NewPos(0, y("output.dir")-d.AbsolutePositionForObject(s.scroll).Y-60)
	s.scroll.Refresh()
	on := defaults()
	on["output.playlist"] = true
	a.m.SetSettings(on)
	a.render()
	if !f.input.(*toggle).on {
		t.Error("the switch does not follow the setting")
	}
	snapshot(t, a, "settings-output-playlist")
	light := defaults()
	light["app.theme"] = "light"
	a.m.SetSettings(light)
	a.render()
	snapshot(t, a, "settings-output-playlist-light")
}

// The session that ended wrote a playlist: the alert names it, and Show
// playlist shows it in its folder. Without one, the alert says the session
// ended, as before.
func TestTheSessionEndedAlertShowsThePlaylist(t *testing.T) {
	var revealed []string
	defer func(r func(string) error) { reveal = r }(reveal)
	reveal = func(p string) error { revealed = append(revealed, p); return nil }

	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	feedSession(t, m)
	m.Apply(event(t, proto.EventTrackFinished, proto.TrackFinishedEvent{ID: "s-004", Path: "/Users/listener/Spytify/M83/Midnight City.flac", DurationMs: 243000, Bytes: 31_400_000, Tagged: true, TaggedBy: "lastfm"}))
	// Wait, already recorded, has its file.
	m.Apply(event(t, proto.EventTrackSkipped, proto.TrackSkippedEvent{ID: "s-003", Reason: proto.SkipDuplicate, Path: "/Users/listener/Spytify/M83/Wait.flac"}))
	list := filepath.Join("/Users/listener/Spytify", "Playlists", "2026-11-03 14.20.m3u8")
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateIdle, Playlist: list}))
	a.render()
	r := a.screens[tabRecord].(*recordScreen)
	want := fmt.Sprintf(lang.SessionEndedPlaylist, filepath.Join("Playlists", "2026-11-03 14.20.m3u8"))
	if got := r.waiting.String(); got != want {
		t.Errorf("the alert says %q, want %q", got, want)
	}
	tapButton(t, a.win.Content(), lang.ShowPlaylist)
	if len(revealed) != 1 || revealed[0] != list {
		t.Errorf("revealed %v, want %s", revealed, list)
	}
	r.list.ScrollToBottom()
	snapshot(t, a, "record-session-playlist")
	light := defaults()
	light["app.theme"] = "light"
	m.SetSettings(light)
	a.render()
	snapshot(t, a, "record-session-playlist-light")

	// The next session starts a list of its own, an ad alone in it; it
	// ends with no playlist.
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateWaiting}))
	m.Apply(event(t, proto.EventTrackStarted, proto.TrackStartedEvent{Track: proto.Track{ID: "s-101", Artist: "Sponsor", Title: "Sponsored", LengthMs: 30000}}))
	m.Apply(event(t, proto.EventTrackSkipped, proto.TrackSkippedEvent{ID: "s-101", Reason: proto.SkipAd}))
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateIdle}))
	a.render()
	if got := r.waiting.String(); got != lang.SessionEnded {
		t.Errorf("the alert says %q, want %q", got, lang.SessionEnded)
	}
	if len(buttons(a.win.Content(), lang.ShowPlaylist)) != 0 {
		t.Error("Show playlist with no playlist")
	}
	test.Tap(r.showPlaylist)
	if len(revealed) != 1 {
		t.Errorf("revealed %v", revealed)
	}
}
