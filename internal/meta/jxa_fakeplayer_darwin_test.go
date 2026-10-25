//go:build darwin

package meta

import (
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spytify/spytify/internal/target"
)

// TestJXAWithTheFakePlayer drives the AppleScript backend against
// FakePlayer, a scriptable app that plays a timeline (macos/fakeplayer).
// Build it with scripts/build-fakeplayer-macos.sh and set SPYTIFY_FAKE_APP
// to the .app. FakePlayer runs this test binary again as its child, so the
// Apple events come from FakePlayer itself and macOS asks for no consent.
func TestJXAWithTheFakePlayer(t *testing.T) {
	if os.Getenv("SPYTIFY_FAKE_INNER") == "1" {
		scriptTheFakePlayer(t)
		return
	}
	app := os.Getenv("SPYTIFY_FAKE_APP")
	if app == "" {
		t.Skip("set SPYTIFY_FAKE_APP to FakePlayer.app, built by scripts/build-fakeplayer-macos.sh")
	}
	app, _ = filepath.Abs(app)
	dir := t.TempDir()
	timeline := filepath.Join(dir, "timeline.json")
	// One has its own cover, beside the timeline; the others none, as no
	// ../covers folder is there to pick from.
	cover, err := os.ReadFile(filepath.Join("..", "..", "testdata", "covers", "cover-01.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cover.jpg"), cover, 0o600); err != nil {
		t.Fatal(err)
	}
	tl := `{"items": [
		{"artist": "Fake Artist", "title": "One", "album": "Fakes", "length_ms": 4000, "art": "cover.jpg"},
		{"artist": "Fake Artist", "title": "Two", "album": "Fakes", "length_ms": 60000},
		{"artist": "Fake Artist", "title": "Three", "album": "Fakes", "length_ms": 60000}]}`
	if err := os.WriteFile(timeline, []byte(tl), 0o600); err != nil {
		t.Fatal(err)
	}
	status, logs := filepath.Join(dir, "status"), filepath.Join(dir, "child.log")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	// open starts FakePlayer as an app of its own, not as a child of this
	// test, which would make the test's terminal the sender of the events.
	cmd := exec.CommandContext(ctx, "open", "-W", "-n", "--stdout", logs, "--stderr", logs, "--env", "SPYTIFY_FAKE_INNER=1",
		app, "--args", "--timeline", timeline, "--silent", "--status", status,
		"--exec", os.Args[0], "-test.run", "^TestJXAWithTheFakePlayer$", "-test.v")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("open: %v %s", err, out)
	}
	code, _ := os.ReadFile(status)
	log, _ := os.ReadFile(logs)
	if strings.TrimSpace(string(code)) != "0" {
		t.Fatalf("against FakePlayer (exit status %q):\n%s", strings.TrimSpace(string(code)), log)
	}
	t.Logf("against FakePlayer:\n%s", log)
}

// scriptTheFakePlayer runs inside FakePlayer.
func scriptTheFakePlayer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	// One plain call first, so a blocked Apple event shows as such.
	probe, cancelProbe := context.WithTimeout(ctx, 20*time.Second)
	out, err := exec.CommandContext(probe, "osascript", "-l", "JavaScript", "-e", `Application("org.spytify.fakeplayer").playerState()`).CombinedOutput()
	cancelProbe()
	if err != nil {
		t.Fatalf("osascript asking FakePlayer its state: %v %s", err, out)
	}
	t.Logf("FakePlayer state: %s", strings.TrimSpace(string(out)))
	j := NewJXA()
	j.Interval = 100 * time.Millisecond
	j.Logf = t.Logf
	events, err := j.Watch(ctx, target.Target{BundleID: "org.spytify.fakeplayer", DurationUnit: "ms"})
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
	track := func(title string) func(Event) bool {
		return func(e Event) bool { return e.Kind == TrackChanged && e.Track.Title == title }
	}
	status := func(s Status) func(Event) bool {
		return func(e Event) bool { return e.Kind == StatusChanged && e.Status == s }
	}

	one := next("first track", track("One"))
	if one.Track.Artist() != "Fake Artist" || one.Track.Album != "Fakes" || one.Track.LengthMs != 4000 || one.Track.ID == "" {
		t.Errorf("first track %+v", one.Track)
	}
	if u := one.Track.ArtURL; !strings.HasPrefix(u, "file:///") || !strings.HasSuffix(u, "/cover.jpg") {
		t.Errorf("first track's cover %q, want the file beside the timeline", u)
	}
	next("playing", status(Playing))
	if err := j.SetVolume(0.4); err != nil {
		t.Fatal(err)
	}
	if v, err := j.Volume(); err != nil || math.Abs(v-0.4) > 0.001 {
		t.Errorf("volume %v %v", v, err)
	}
	// The first item ends on its own; then the commands move the player.
	if two := next("second track", track("Two")); two.Track.ArtURL != "" {
		t.Errorf("second track's cover %q, want none", two.Track.ArtURL)
	}
	if err := j.Control("next"); err != nil {
		t.Fatal(err)
	}
	next("third track after next", track("Three"))
	if err := j.Control("pause"); err != nil {
		t.Fatal(err)
	}
	next("paused", status(Paused))
	if err := j.Control("play"); err != nil {
		t.Fatal(err)
	}
	next("playing again", status(Playing))
	time.Sleep(4 * time.Second)
	if err := j.Control("restart"); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if e := next("restart", track("Three")); !e.Restart {
		t.Errorf("restart reported as %+v", e)
	}
}
