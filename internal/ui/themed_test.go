package ui

import (
	"fmt"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

// Each track used to cost the window about 17 MB it never gave back: each
// refresh of a theme override had Fyne parse the theme's fonts again
// (#764: 10 GB after a day). 200 tracks through the Record list, the window
// drawn after each as the screen does, leave the heap where it was.
func TestTracksDoNotGrowTheWindowsMemory(t *testing.T) {
	if testing.Short() {
		t.Skip("plays 220 tracks")
	}
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	values := defaults()
	values["output.dir"] = t.TempDir()
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	m.SetSettings(values)
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateRecording}))
	play := func(from, to int) {
		for i := from; i < to; i++ {
			if i > 0 {
				m.Apply(event(t, proto.EventTrackFinished, proto.TrackFinishedEvent{ID: fmt.Sprintf("s-%03d", i), Path: filepath.Join(values["output.dir"].(string), "x.mp3"), DurationMs: 1000, Bytes: 4}))
			}
			m.Apply(event(t, proto.EventTrackStarted, proto.TrackStartedEvent{Track: proto.Track{
				ID: fmt.Sprintf("s-%03d", i+1), Artist: "Artiste", Title: fmt.Sprintf("Track %d", i+1), LengthMs: 1000}}))
			a.render()
			a.win.Canvas().Capture()
		}
	}
	heap := func() uint64 {
		runtime.GC()
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		return ms.HeapAlloc
	}
	// The first tracks fill the caches a window keeps.
	play(0, 20)
	before := heap()
	// Measured every 10 tracks: a leak stops the test early, before it
	// takes the machine's memory (main's code grew it by 15 GB).
	for n := 20; n < 220; n += 10 {
		play(n, n+10)
		if grown := int64(heap()) - int64(before); grown > 50<<20 {
			t.Fatalf("%d tracks grew the heap by %d MB", n+10-20, grown>>20)
		}
	}
}
