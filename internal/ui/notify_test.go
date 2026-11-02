package ui

import (
	"fmt"
	"path/filepath"
	"slices"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/ui/lang"
	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

// noticeApp is a test app that keeps the notifications it is sent.
type noticeApp struct {
	fyne.App
	sent []*fyne.Notification
}

func (n *noticeApp) SendNotification(x *fyne.Notification) { n.sent = append(n.sent, x) }

// noticed is what was sent, as "title: text".
func (n *noticeApp) noticed() []string {
	var out []string
	for _, x := range n.sent {
		out = append(out, x.Title+": "+x.Content)
	}
	return out
}

// listen sends a's notifications to a test app that keeps them.
func listen(a *App) *noticeApp {
	sink := &noticeApp{App: a.fa}
	a.fa = sink
	return sink
}

// hiddenSession hides the window in the tray, or hides it and brings it
// back, with app.notifications at mode, then runs a session the recorder
// ends on a full disk: one track saved, one skipped as a duplicate, the
// capture in low quality for a while.
func hiddenSession(t *testing.T, mode string, back bool) []string {
	t.Helper()
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	sink := listen(a)
	values := defaults()
	values["app.notifications"] = mode
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	m.SetSettings(values)
	a.render()
	a.hideToTray()
	if back {
		a.showWindow()
	}
	for _, ev := range []proto.Event{
		event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateWaiting}),
		event(t, proto.EventTrackStarted, proto.TrackStartedEvent{Track: proto.Track{ID: "s-001", Artist: "M83", Title: "Midnight City"}}),
		event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateRecording}),
		event(t, proto.EventTrackFinished, proto.TrackFinishedEvent{ID: "s-001", Path: "/music/M83 - Midnight City.mp3"}),
		event(t, proto.EventTrackStarted, proto.TrackStartedEvent{Track: proto.Track{ID: "s-002", Artist: "M83", Title: "Wait"}}),
		event(t, proto.EventTrackSkipped, proto.TrackSkippedEvent{ID: "s-002", Reason: proto.SkipDuplicate}),
		event(t, proto.EventTargetState, proto.TargetStateEvent{Running: false}),
		event(t, proto.EventError, proto.ErrorEvent{Code: "capture_low_quality", Msg: "the player's output device runs at 16 kHz mono"}),
		event(t, proto.EventError, proto.ErrorEvent{Code: "capture_low_quality"}),
		event(t, proto.EventError, proto.ErrorEvent{Code: "ring_overflow", Msg: "12 frames lost"}),
		event(t, proto.EventError, proto.ErrorEvent{Code: "disk_full", Msg: "only 80 MB free in the output folder"}),
		event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateStopping}),
		event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateIdle}),
	} {
		m.Apply(ev)
	}
	return sink.noticed()
}

// Problems only, the default, tells of a session that stopped on its own and
// of one recording in low quality; Every saved track adds each saved track;
// Off tells nothing. Skips, the player closing and lost frames are routine.
func TestNotificationsFollowTheSetting(t *testing.T) {
	low := lang.RecordingLowQuality + ": The player's output device runs at 16 kHz mono"
	stopped := lang.RecordingStopped + ": Only 80 MB free in the output folder"
	saved := lang.TrackSaved + ": Midnight City · M83"
	for _, c := range []struct {
		mode string
		want []string
	}{
		{"problems", []string{low, stopped}},
		{"tracks", []string{saved, low, stopped}},
		{"off", nil},
	} {
		if got := hiddenSession(t, c.mode, false); !slices.Equal(got, c.want) {
			t.Errorf("%s: sent %q, want %q", c.mode, got, c.want)
		}
	}
}

// The window says it all while it shows: back from the tray, no
// notification comes.
func TestNotificationsOnlyWhileHiddenInTheTray(t *testing.T) {
	if got := hiddenSession(t, "tracks", true); len(got) != 0 {
		t.Errorf("sent %q with the window back", got)
	}
}

// A session stopped from the app bar or the tray ends with no
// notification; one that ends on its own, on the session timer here, says
// so with its counts.
func TestOnlyASessionThatEndsOnItsOwnIsNews(t *testing.T) {
	a, m, core := connectedApp(t)
	sink := listen(a)
	m.SetSettings(defaults())
	a.hideToTray()
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateRecording}))
	a.render()
	test.Tap(a.action)
	waitStops(t, core, 1)
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateStopping}))
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateIdle}))
	if got := sink.noticed(); len(got) != 0 {
		t.Fatalf("sent %q for a session stopped from the app", got)
	}
	for _, ev := range []proto.Event{
		event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateWaiting}),
		event(t, proto.EventTrackStarted, proto.TrackStartedEvent{Track: proto.Track{ID: "s-003", Artist: "M83", Title: "Outro"}}),
		event(t, proto.EventTrackFinished, proto.TrackFinishedEvent{ID: "s-003", Path: "/music/M83 - Outro.mp3"}),
		event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateStopping}),
		event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateIdle}),
	} {
		m.Apply(ev)
	}
	if got, want := sink.noticed(), []string{lang.RecordingStopped + ": " + fmt.Sprintf(lang.SavedSkipped, 1, 0)}; !slices.Equal(got, want) {
		t.Errorf("sent %q, want %q", got, want)
	}
}

// Notifications sit in Settings > App, at Problems only until changed.
func TestTheNotificationsSettingStartsAtProblemsOnly(t *testing.T) {
	a, _, s := settingsApp(t)
	sel, ok := s.fields["app.notifications"].input.(*widget.Select)
	if !ok {
		t.Fatalf("app.notifications is a %T", s.fields["app.notifications"].input)
	}
	if want := []string{"Problems only", "Every saved track", "Off"}; !slices.Equal(sel.Options, want) {
		t.Errorf("choices %q, want %q", sel.Options, want)
	}
	if sel.Selected != "Problems only" {
		t.Errorf("shows %q by default", sel.Selected)
	}
	snapshot(t, a, "settings-notifications")
}
