package openfiles

import (
	"os"
	"path/filepath"
	"testing"
)

// The check sees a file this process holds open, and nothing once it is
// closed.
func TestHeldSeesAnOpenFile(t *testing.T) {
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "track.wav"))
	if err != nil {
		t.Fatal(err)
	}
	held, err := Held(os.Getpid(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(held) == 0 {
		t.Fatal("an open file was not seen")
	}
	f.Close()
	if held, err := Held(os.Getpid(), dir); err != nil || len(held) != 0 {
		t.Fatalf("after closing: %v %v", held, err)
	}
}
