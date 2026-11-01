package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"fyne.io/fyne/v2"

	"github.com/spytify/spytify/internal/settings"
	"github.com/spytify/spytify/internal/ui/lang"
	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

// notices is what the notifications keep from one event to the next, on
// the toolkit's goroutine.
type notices struct {
	// hidden says the window is hidden in the tray: only then do
	// notifications come, the window saying it all otherwise.
	hidden bool
	// stopAsked says the app bar or the tray asked the session to stop.
	stopAsked bool
	// stopped is why the recorder is ending the session on its own.
	stopped string
}

// app.notifications' values, each telling what the one before it does and
// more.
const (
	noticeOff = iota
	noticeProblems
	noticeTracks
)

// stopCodes are the recorder's errors after which it ends the session on
// its own: the disk is full, the output folder is gone, or the counter is
// past its last number.
var stopCodes = map[string]bool{"disk_full": true, "output_dir": true, "counter_full": true}

// warnTitles title the problems that leave a session running with nothing
// worth keeping: tracks that sound like a phone call, or no audio at all.
var warnTitles = map[string]string{
	"capture_low_quality": lang.RecordingLowQuality,
	"capture_stalled":     lang.NothingRecorded,
}

// notify tells, while the window is hidden in the tray, what would
// otherwise wait for it to open (app.notifications): a session that ended
// with no Stop pressed, one recording nothing worth keeping, and with
// "tracks", each saved track. Skips, ads and the player's comings and
// goings are routine. It runs on the link's goroutine.
func (a *App) notify(ev proto.Event) {
	switch ev.Method {
	case proto.EventSessionState:
		// The recorder says idle only once a session ended.
		var e proto.SessionStateEvent
		if ev.Decode(&e) == nil && e.State == proto.StateIdle {
			fyne.Do(a.sessionEnded)
		}
	case proto.EventTrackFinished:
		var e proto.TrackFinishedEvent
		if ev.Decode(&e) == nil {
			fyne.Do(func() { a.trackSaved(e) })
		}
	case proto.EventError:
		// No message withdraws a warning: nothing to tell.
		var e proto.ErrorEvent
		if ev.Decode(&e) == nil && e.Msg != "" && (stopCodes[e.Code] || warnTitles[e.Code] != "") {
			fyne.Do(func() { a.recorderError(e) })
		}
	}
}

// sessionEnded tells of a session that ended with no Stop pressed in the
// app: on the recorder's error, or on its own as set (the session timer,
// a single track), or from spytify-cli.
func (a *App) sessionEnded() {
	asked, why := a.notes.stopAsked, a.notes.stopped
	a.notes.stopAsked, a.notes.stopped = false, ""
	if asked {
		return
	}
	s, ok := a.noticeState(noticeProblems)
	if !ok {
		return
	}
	if why == "" {
		t := s.Totals()
		why = fmt.Sprintf(lang.SavedSkipped, t.Saved, t.Skipped)
	}
	a.fa.SendNotification(fyne.NewNotification(lang.RecordingStopped, why))
}

// recorderError keeps why the recorder is ending the session, told once it
// ended, and tells at once of a session recording nothing worth keeping.
func (a *App) recorderError(e proto.ErrorEvent) {
	if stopCodes[e.Code] {
		a.notes.stopped = sentence(e.Msg)
		return
	}
	if _, ok := a.noticeState(noticeProblems); ok {
		a.fa.SendNotification(fyne.NewNotification(warnTitles[e.Code], sentence(e.Msg)))
	}
}

// trackSaved tells of each saved track, by its title and artist.
func (a *App) trackSaved(e proto.TrackFinishedEvent) {
	s, ok := a.noticeState(noticeTracks)
	if !ok {
		return
	}
	name := strings.TrimSuffix(filepath.Base(e.Path), filepath.Ext(e.Path))
	for _, r := range s.Rows {
		if r.ID == e.ID && r.Title != "" {
			name = r.Title
			if r.Artist != "" {
				name += " · " + r.Artist
			}
		}
	}
	a.fa.SendNotification(fyne.NewNotification(lang.TrackSaved, name))
}

// noticeState is the model's state when a notification of level is due:
// the window is hidden in the tray and app.notifications asks for it.
func (a *App) noticeState(level int) (model.State, bool) {
	if !a.notes.hidden {
		return model.State{}, false
	}
	s := a.m.Snapshot()
	return s, noticeLevel(s.Settings) >= level
}

// noticeLevel is app.notifications as a level. A recorder of an earlier
// build does not have the setting: its default holds.
func noticeLevel(values map[string]any) int {
	mode, _ := values[settings.AppNotifications.String()].(string)
	if mode == "" {
		mode = settings.Defaults().String(settings.AppNotifications)
	}
	switch mode {
	case "tracks":
		return noticeTracks
	case "problems":
		return noticeProblems
	}
	return noticeOff
}

// sentence starts the recorder's message with a capital, as a
// notification's text.
func sentence(msg string) string {
	r, n := utf8.DecodeRuneInString(msg)
	if n == 0 {
		return msg
	}
	return string(unicode.ToUpper(r)) + msg[n:]
}
