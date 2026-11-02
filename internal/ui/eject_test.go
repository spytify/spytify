package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spytify/spytify/internal/openfiles"
)

// The Library tab reads the output folder and its covers, then lets go of
// it: the folder may be on a USB drive the user ejects.
func TestTheLibraryHoldsNothingInTheOutputFolder(t *testing.T) {
	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	track := filepath.Join(dir, "Artist", "Artist - Song.flac")
	if err := os.MkdirAll(filepath.Dir(track), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(ff, "-v", "error", "-nostdin", "-f", "lavfi", "-t", "0.5", "-i", "anullsrc", "-f", "lavfi", "-i", "color=red:s=8x8:d=1",
		"-map", "0:a", "-map", "1:v", "-frames:v", "1", "-c:a", "flac", "-c:v", "png", "-disposition:v", "attached_pic", track)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make the FLAC: %v: %s", err, b)
	}
	values := defaults()
	values["output.dir"] = dir
	a, _ := connectedTestApp(t, values)
	a.showTab(tabLibrary)
	a.render()
	lib := a.screens[tabLibrary].(*libraryScreen)
	if len(lib.shown) != 1 {
		t.Fatalf("%d files listed", len(lib.shown))
	}
	eventually(t, "the cover never loaded", func() bool { return lib.cover(lib.shown[0]).row != nil })
	held, err := openfiles.Held(os.Getpid(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(held) > 0 {
		t.Fatalf("the library holds the output folder open: %v", held)
	}
}
