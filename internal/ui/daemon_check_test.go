package ui

import (
	"fmt"
	"testing"

	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/ui/lang"
	"github.com/spytify/spytify/internal/ui/model"
)

// spytifyd killed under the open diagnostics: they say it is gone, offer to
// start it, and no longer show its last checks as passing. It coming back
// brings its checks again.
func TestTheDiagnosticsFollowSpytifyd(t *testing.T) {
	a, m := connectedTestApp(t, defaults())
	a.showTab(tabRecord)
	a.render()
	d := diagnostics(t, a)
	if all := texts(d); !contains(all, fmt.Sprintf(lang.ChecksPass, 5)) || !contains(all, lang.RecorderRunning) || !contains(all, "v2.0.0-alpha") {
		t.Errorf("spytifyd answers: %v", all)
	}

	m.SetConn(model.Disconnected, "spytifyd closed the connection")
	a.render()
	all := texts(d)
	for _, want := range []string{lang.NothingElseChecked, lang.CheckRecorderNotRunning, "spytifyd closed the connection", lang.StartRecorder} {
		if !contains(all, want) {
			t.Errorf("%q not in the diagnostics once spytifyd is gone: %v", want, all)
		}
	}
	for _, stale := range []string{fmt.Sprintf(lang.ChecksPass, 5), "ffmpeg found", "capture helper", "turn crossfade and gapless playback off in the player"} {
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
	if all := texts(d); !contains(all, lang.StartingRecorder) || contains(all, lang.StartRecorder) {
		t.Errorf("spytifyd starting: %v", all)
	}
	a.starting = false

	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	a.render()
	if all := texts(d); !contains(all, fmt.Sprintf(lang.ChecksPass, 5)) || !contains(all, "ffmpeg found") || contains(all, lang.StartRecorder) {
		t.Errorf("spytifyd back: %v", all)
	}
}
