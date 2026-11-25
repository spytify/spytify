package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/spytify/spytify/pkg/proto"
)

// countFFmpeg puts an ffmpeg first on PATH that notes each run's arguments
// then runs the real one, and returns the runs noted so far. A run whose
// arguments hold refuse, when given, fails instead.
func countFFmpeg(t *testing.T, refuse string) func() []string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the counting ffmpeg is a shell script")
	}
	real, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("no ffmpeg")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "runs")
	script := "#!/bin/sh\necho \"$*\" >> '" + log + "'\n"
	if refuse != "" {
		script += "case \"$*\" in *'" + refuse + "'*) echo refused >&2; exit 1;; esac\n"
	}
	script += "exec '" + real + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "ffmpeg"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return func() []string {
		b, _ := os.ReadFile(log)
		return strings.Split(strings.TrimSpace(string(b)), "\n")
	}
}

// Each saved track runs ffmpeg once, its tags and cover written with its
// audio, and once more to measure its loudness when normalized: not a
// second time to tag the file, which rewrote it whole (#837).
func TestASavedTrackRunsFFmpegOnceForItsAudioAndTags(t *testing.T) {
	for _, c := range []struct {
		normalize bool
		perTrack  int
	}{{false, 1}, {true, 2}} {
		runs := countFFmpeg(t, "")
		s := runSettings(t, map[string]any{"encode.format": "mp3", "tags.cover": true, "recorder.normalize": c.normalize})
		var tracks int
		for _, ev := range s.events.all() {
			var f proto.TrackFinishedEvent
			if ev.Method != proto.EventTrackFinished || ev.Decode(&f) != nil {
				continue
			}
			tracks++
			if !f.Tagged {
				t.Errorf("normalize %v: %s saved untagged", c.normalize, f.Path)
			}
		}
		var encodes []string
		for _, r := range runs() {
			// Finding ffmpeg and its encoders is once per session.
			if !strings.HasSuffix(r, " -version") && !strings.HasSuffix(r, " -encoders") {
				encodes = append(encodes, r)
			}
		}
		if tracks == 0 || len(encodes) != c.perTrack*tracks {
			t.Errorf("normalize %v: %d ffmpeg runs for %d tracks, want %d each:\n%s", c.normalize, len(encodes), tracks, c.perTrack, strings.Join(encodes, "\n"))
		}
	}
}

// A track whose tags ffmpeg cannot write with its audio is encoded again
// without them and saved all the same, as when tagging was a run of its own.
func TestATrackWhoseTagsFailIsSavedUntagged(t *testing.T) {
	countFFmpeg(t, "-id3v2_version 3")
	s := runSettings(t, map[string]any{"encode.format": "mp3", "tags.cover": true})
	saved := 0
	for _, ev := range s.events.all() {
		var f proto.TrackFinishedEvent
		if ev.Method != proto.EventTrackFinished || ev.Decode(&f) != nil {
			continue
		}
		saved++
		if f.Tagged {
			t.Errorf("%s: tagged, though its tags were refused", f.Path)
		}
	}
	if saved == 0 || !strings.Contains(s.logs.String(), "tags not written") {
		t.Fatalf("%d saved; log:\n%s", saved, s.logs)
	}
}

// An encode that outlasts the cover's deadline is saved all the same: the
// deadline was shared with ffmpeg and killed a long track's encode, which
// Windows reports as a bare "exit status 1" (#956).
func TestAnEncodeLongerThanTheCoverDeadlineIsSaved(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the slow ffmpeg is a shell script")
	}
	real, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("no ffmpeg")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\ncase \"$*\" in *-progress*) sleep 2;; esac\nexec '" + real + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "ffmpeg"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	old := tagTimeout
	tagTimeout = time.Second
	t.Cleanup(func() { tagTimeout = old })

	s := runSettings(t, map[string]any{"encode.format": "mp3"})
	var tracks int
	for _, ev := range s.events.all() {
		switch ev.Method {
		case proto.EventTrackFinished:
			tracks++
		case proto.EventError:
			var e proto.ErrorEvent
			_ = ev.Decode(&e)
			t.Errorf("%s: %s", e.Code, e.Msg)
		}
	}
	if tracks == 0 {
		t.Fatal("no track saved")
	}
}
