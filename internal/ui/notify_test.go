package ui

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"fyne.io/fyne/v2/test"

	"github.com/spytify/spytify/internal/ui/lang"
	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

// noticeSink keeps the notifications an app shows.
type noticeSink struct {
	sent []string
}

// noticed is what was shown, as "title: text".
func (n *noticeSink) noticed() []string { return n.sent }

// listen keeps a's notifications instead of showing them.
func listen(a *App) *noticeSink {
	sink := &noticeSink{}
	a.toast = func(title, text string) { sink.sent = append(sink.sent, title+": "+text) }
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
		event(t, proto.EventError, proto.ErrorEvent{Code: proto.ErrorCaptureLowQuality, Msg: "the player's output device runs at 16 kHz mono"}),
		event(t, proto.EventError, proto.ErrorEvent{Code: proto.ErrorCaptureLowQuality}),
		event(t, proto.EventError, proto.ErrorEvent{Code: proto.ErrorCaptureSilent, Msg: "the recorder's own words"}),
		event(t, proto.EventError, proto.ErrorEvent{Code: proto.ErrorCaptureSilent}),
		event(t, proto.EventError, proto.ErrorEvent{Code: proto.ErrorRingOverflow, Msg: "12 frames lost"}),
		event(t, proto.EventError, proto.ErrorEvent{Code: proto.ErrorDiskFull, Msg: "only 80 MB free in the output folder"}),
		event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateStopping}),
		event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateIdle}),
	} {
		m.Apply(ev)
	}
	return sink.noticed()
}

// Problems only, the default, tells of a session that stopped on its own and
// of one recording in low quality or only silence, the silence in the
// window's words; Every saved track adds each saved track; Off tells
// nothing. Skips, the player closing and lost frames are routine.
func TestNotificationsFollowTheSetting(t *testing.T) {
	low := lang.RecordingLowQuality + ": The player's output device runs at 16 kHz mono"
	silent := lang.NothingRecorded + ": " + fmt.Sprintf(lang.OnlySilenceHeard, "Player")
	stopped := lang.RecordingStopped + ": Only 80 MB free in the output folder"
	saved := lang.TrackSaved + ": Midnight City · M83"
	for _, c := range []struct {
		mode string
		want []string
	}{
		{"problems", []string{low, silent, stopped}},
		{"tracks", []string{saved, low, silent, stopped}},
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
	sel, ok := s.fields["app.notifications"].input.(*selectField)
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

// notifiers may call Fyne's notifications: not on Windows, where Fyne
// shows them from a hidden PowerShell that security software flags.
var notifiers = map[string]string{
	"internal/ui/notify_other.go":   "systemToast on Linux and macOS",
	"internal/ui/nowindow_linux.go": "tells, on Linux, that the window cannot open",
}

// Every notification goes through systemToast, for every OS: the parser
// reads the files of all three, whatever this test runs on.
func TestEveryNotificationGoesThroughSystemToast(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, dir := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			if notifiers[filepath.ToSlash(rel)] != "" {
				return nil
			}
			f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			ast.Inspect(f, func(n ast.Node) bool {
				if sel, ok := n.(*ast.SelectorExpr); ok {
					switch sel.Sel.Name {
					case "SendNotification", "ScheduleNotification":
						t.Errorf("%s: %s; use App.toast", fset.Position(sel.Pos()), sel.Sel.Name)
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
