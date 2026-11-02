package ui

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/spytify/spytify/internal/ui/lang"
	"github.com/spytify/spytify/internal/ui/model"
)

// spytifyd killed with no session: Record says why Record is off and offers
// to start it, rather than "Ready to record" beside a Record turned off.
func TestRecordSaysSpytifydIsGone(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	m.SetSettings(defaults())
	a.render()

	m.SetConn(model.Disconnected, "spytifyd closed the connection")
	a.render()
	all := texts(a.win.Content())
	for _, want := range []string{lang.RecordIsOff, lang.RecordNeedsRecorder, lang.StartRecorder} {
		if !contains(all, want) {
			t.Errorf("%q not on Record once spytifyd is gone: %v", want, all)
		}
	}
	for _, stale := range []string{lang.ReadyToRecord, "Press Record", "Open Player"} {
		if contains(all, stale) {
			t.Errorf("%q still on Record once spytifyd is gone", stale)
		}
	}
	if !a.action.Disabled() {
		t.Error("Record is on without spytifyd")
	}
	snapshot(t, a, "record-spytifyd-gone")

	// A start that failed says why.
	a.startErr = "spytifyd stopped: listen: address already in use"
	a.render()
	if all := texts(a.win.Content()); !contains(all, a.startErr) || !contains(all, lang.StartRecorder) {
		t.Errorf("failed start: %v", all)
	}
	a.startErr = ""

	// Being started, it is no longer offered to start.
	a.starting = true
	a.render()
	if all := texts(a.win.Content()); !contains(all, lang.StartingRecorder) || contains(all, lang.StartRecorder) {
		t.Errorf("spytifyd starting: %v", all)
	}
	a.starting = false

	m.SetConn(model.Connected, "")
	a.render()
	if all := texts(a.win.Content()); !contains(all, lang.ReadyToRecord) || contains(all, lang.StartRecorder) {
		t.Errorf("spytifyd back: %v", all)
	}
}

// spytifyd killed while it records: the track it was on is no longer shown
// as being recorded, and the session's row says spytifyd is missing.
func TestRecordSaysSpytifydIsGoneDuringASession(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	feedSession(t, m)
	a.render()
	r := a.screens[tabRecord].(*recordScreen)
	if !r.card.Visible() {
		t.Fatal("recording: no card for the track")
	}

	m.SetConn(model.Disconnected, "spytifyd closed the connection")
	a.render()
	if r.card.Visible() {
		t.Error("spytifyd is gone: the track still shows as being recorded")
	}
	all := texts(a.win.Content())
	for _, want := range []string{fmt.Sprintf(lang.NothingRecordedNotRunning, "spytifyd"), lang.StartRecorder, "Reunion"} {
		if !contains(all, want) {
			t.Errorf("%q not on Record once spytifyd is gone: %v", want, all)
		}
	}
	if r.restart.Visible() || r.launch.Visible() || r.play.Visible() || r.blocked.Visible() {
		t.Error("spytifyd is gone: a button for the player or the folder shows")
	}
	snapshot(t, a, "record-spytifyd-gone-session")

	m.SetConn(model.Connected, "")
	a.render()
	if !r.card.Visible() || r.waitRow.Visible() {
		t.Errorf("spytifyd back: card %v, waiting row %v", r.card.Visible(), r.waitRow.Visible())
	}
}
