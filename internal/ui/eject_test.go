package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/spytify/spytify/internal/openfiles"
	"github.com/spytify/spytify/internal/ui/model"
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
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	values := defaults()
	values["output.dir"] = dir
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	m.SetSettings(values)
	a.showTab(tabLibrary)
	a.render()
	lib := a.screens[tabLibrary].(*libraryScreen)
	if len(lib.shown) != 1 {
		t.Fatalf("%d files listed", len(lib.shown))
	}
	deadline := time.Now().Add(10 * time.Second)
	for lib.cover(lib.shown[0]).row == nil {
		if time.Now().After(deadline) {
			t.Fatal("the cover never loaded")
		}
		time.Sleep(50 * time.Millisecond)
	}
	held, err := openfiles.Held(os.Getpid(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(held) > 0 {
		t.Fatalf("the library holds the output folder open: %v", held)
	}
}
