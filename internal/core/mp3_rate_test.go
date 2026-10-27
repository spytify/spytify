package core

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/spytify/spytify/pkg/proto"
)

// A Mac whose output runs at 96 kHz delivers 96 kHz audio: an MP3 of it,
// which cannot be at that rate, is saved at 48 kHz.
func TestAnMP3OfA96kHzSourceIsSaved(t *testing.T) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed")
	}
	tl := loadTimeline(t, "basic")
	tl.Rate, tl.Sample = 96000, "f32"
	s := newScenario(t, tl, map[string]any{"encode.format": "mp3"})
	s.run(t, tl)
	var saved []string
	for _, ev := range s.events.all() {
		var f proto.TrackFinishedEvent
		if ev.Method == proto.EventTrackFinished && ev.Decode(&f) == nil {
			saved = append(saved, f.Path)
		}
	}
	if len(saved) != len(tl.Items) {
		t.Fatalf("saved %d of %d tracks; log:\n%s", len(saved), len(tl.Items), s.logs)
	}
	for _, p := range saved {
		out, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "stream=sample_rate", "-of", "csv=p=0", p).Output()
		if err != nil || strings.TrimSpace(string(out)) != "48000" {
			t.Errorf("%s: sample rate %q %v, want 48000", p, out, err)
		}
	}
}
