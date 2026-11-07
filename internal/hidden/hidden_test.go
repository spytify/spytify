package hidden

import (
	"os"
	"path/filepath"
	"testing"
)

// The folder and its missing parents are made, and making it again is fine.
func TestMkdirAllMakesTheFolder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "out", ".covers")
	for range 2 {
		if err := MkdirAll(dir); err != nil {
			t.Fatal(err)
		}
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Fatalf("%s: %v", dir, err)
	}
}
