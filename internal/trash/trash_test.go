package trash

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// A file that is not there stays an error: Move says so, and the caller
// can tell it from a trash that refused.
func TestMoveSaysAMissingFileIsMissing(t *testing.T) {
	err := Move(filepath.Join(t.TempDir(), "absent.flac"))
	if errors.Is(err, errors.ErrUnsupported) {
		t.Skip("no trash on this system")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Move of a missing file: %v", err)
	}
}

// testFile writes a file to move to the trash, holding its name, and
// returns its path.
func testFile(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(filepath.Base(name)), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func gone(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("%s is still there: %v", path, err)
	}
}
