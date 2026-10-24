package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spytify/spytify/internal/encode"
	"github.com/spytify/spytify/internal/fake"
)

// Recordings that are the tracks, late and quieter, pass; a recording that
// ends with the next track's first 200 ms fails.
func TestCheckFindsANeighbourInARecording(t *testing.T) {
	ff, err := encode.Find(ffmpegName())
	if err != nil {
		t.Skip(err)
	}
	tl, err := fake.Load(filepath.Join("..", "..", "..", "testdata", "timelines", "jitter.json"))
	if err != nil {
		t.Fatal(err)
	}
	dir, out := t.TempDir(), t.TempDir()
	if err := export(ff.Path, tl, dir, "flac"); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		if b, err := exec.Command(ff.Path, append([]string{"-v", "error", "-nostdin", "-y"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("ffmpeg %v: %v %s", args, err, b)
		}
	}
	ref := func(i int) string { return filepath.Join(dir, fmt.Sprintf("%02d %s.flac", i+1, tl.Items[i].Title)) }
	rec := func(i int) string {
		return filepath.Join(out, strings.Join(tl.Items[i].Artists, ", ")+" - "+tl.Items[i].Title+".wav")
	}
	for i := range tl.Items {
		run("-i", ref(i), "-af", "adelay=300:all=1,volume=0.8,apad=pad_dur=0.2", "-c:a", "pcm_s16le", rec(i))
	}
	results, err := check(ff.Path, dir, out)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 10 {
		t.Fatalf("%d results", len(results))
	}
	for _, r := range results {
		if !r.ok() {
			t.Errorf("clean recording failed: %v", r)
		}
	}

	// Track 4 runs 200 ms into track 5.
	bled := filepath.Join(t.TempDir(), "bled.wav")
	run("-i", ref(3), "-i", ref(4), "-filter_complex", "[1:a]atrim=0:0.2[n];[0:a][n]concat=n=2:v=0:a=1", "-c:a", "pcm_s16le", bled)
	if err := os.Rename(bled, rec(3)); err != nil {
		t.Fatal(err)
	}
	results, err = check(ff.Path, dir, out)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		bad := r.title == tl.Items[3].Title
		if r.ok() == bad {
			t.Errorf("%v (expected to fail: %v)", r, bad)
		}
		if bad && !strings.HasPrefix(r.String(), "FAIL") {
			t.Errorf("report %q", r)
		}
	}
}
