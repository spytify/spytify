package ui

import (
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/ui/model"
)

// spytifyd killed under the open diagnostics: they say it is gone, offer to
// start it, and no longer show its last checks as passing. It coming back
// brings its checks again.
func TestTheDiagnosticsFollowSpytifyd(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	m.SetSettings(defaults())
	a.showTab(tabRecord)
	a.render()
	d := diagnostics(t, a)
	if all := texts(d); !contains(all, "All 5 checks pass.") || !contains(all, "spytifyd running") || !contains(all, "v2.0.0-alpha") {
		t.Errorf("spytifyd answers: %v", all)
	}

	m.SetConn(model.Disconnected, "spytifyd closed the connection")
	a.render()
	all := texts(d)
	for _, want := range []string{"spytifyd is not running, so nothing else can be checked.", "spytifyd not running", "spytifyd closed the connection", "Start spytifyd"} {
		if !contains(all, want) {
			t.Errorf("%q not in the diagnostics once spytifyd is gone: %v", want, all)
		}
	}
	for _, stale := range []string{"All 5 checks pass.", "ffmpeg found", "capture helper", "turn crossfade and gapless playback off in the player"} {
		if contains(all, stale) {
			t.Errorf("%q still in the diagnostics once spytifyd is gone", stale)
		}
	}
	if b := a.screens[tabLogs].(*logsScreen).diagnose; b.Importance != widget.WarningImportance {
		t.Errorf("spytifyd is gone: the Diagnostics button's importance is %v", b.Importance)
	}
	snapshot(t, a, "diagnostics-spytifyd-gone")

	// Being started, it is no longer offered to start.
	a.starting = true
	a.render()
	if all := texts(d); !contains(all, startingText) || contains(all, "Start spytifyd") {
		t.Errorf("spytifyd starting: %v", all)
	}
	a.starting = false

	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	a.render()
	if all := texts(d); !contains(all, "All 5 checks pass.") || !contains(all, "ffmpeg found") || contains(all, "Start spytifyd") {
		t.Errorf("spytifyd back: %v", all)
	}
}
