//go:build windows

package meta

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/spytify/spytify/internal/target"
)

// TestSMTCWithAPlayer reads a real media session: a Windows PowerShell
// process playing through MediaPlayer (scripts/ci/tone.ps1 -Smtc), then
// pauses and plays it. It needs an audio output; CI installs a virtual
// one and sets SPYTIFY_SMTC_PLAYER.
func TestSMTCWithAPlayer(t *testing.T) {
	if os.Getenv("SPYTIFY_SMTC_PLAYER") == "" {
		t.Skip("set SPYTIFY_SMTC_PLAYER on a machine with an audio output")
	}
	script, _ := filepath.Abs(filepath.Join("..", "..", "scripts", "ci", "tone.ps1"))
	player := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", script,
		"-Path", filepath.Join(t.TempDir(), "tone.wav"), "-Smtc")
	var playerOut bytes.Buffer
	player.Stdout, player.Stderr = &playerOut, &playerOut
	if err := player.Start(); err != nil {
		t.Fatal(err)
	}
	defer player.Process.Kill()
	defer func() {
		if t.Failed() {
			t.Logf("player output:\n%s", playerOut.String())
			list := smtcPrelude + "foreach ($s in $manager.GetSessions()) { $s.SourceAppUserModelId }"
			out, err := exec.Command("powershell", "-NoProfile", "-Command", list).CombinedOutput()
			t.Logf("media sessions (%v):\n%s", err, out)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	m := NewSMTC()
	m.Logf = t.Logf
	events, err := m.Watch(ctx, target.Target{ProcessNames: []string{"powershell"}})
	if err != nil {
		t.Fatal(err)
	}
	next := func(what string, ok func(Event) bool) Event {
		t.Helper()
		for {
			select {
			case e := <-events:
				t.Logf("event %+v", e)
				if ok(e) {
					return e
				}
			case <-ctx.Done():
				t.Fatalf("no %s", what)
			}
		}
	}
	tr := next("track", func(e Event) bool { return e.Kind == TrackChanged && e.Track.Title == "Runner Tone" })
	if tr.Track.Artist() != "Spytify CI" || tr.Track.Album != "Runner" {
		t.Errorf("track %+v", tr.Track)
	}
	next("playing", func(e Event) bool { return e.Kind == StatusChanged && e.Status == Playing })
	if err := m.Control("pause"); err != nil {
		t.Fatalf("pause: %v", err)
	}
	next("paused", func(e Event) bool { return e.Kind == StatusChanged && e.Status == Paused })
	if err := m.Control("play"); err != nil {
		t.Fatalf("play: %v", err)
	}
	next("playing again", func(e Event) bool { return e.Kind == StatusChanged && e.Status == Playing })
}
