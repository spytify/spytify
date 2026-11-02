//go:build darwin && cgo

package trash

import (
	"os"
	"strings"
	"testing"
)

// The file goes to the Trash, from a process with no Full Disk Access and
// no Automation permission, as the app has.
func TestMoveToTheMacTrash(t *testing.T) {
	realTrash(t)
	path := testFile(t, t.TempDir(), "Spytify trash test.flac")
	trashed, err := move(path)
	if err != nil {
		t.Fatal(err)
	}
	gone(t, path)
	if !strings.Contains(trashed, "/.Trash") {
		t.Errorf("moved to %s, not to a Trash", trashed)
	}
	// Reading the Trash needs Full Disk Access; taking the file out again
	// may not be allowed either, and leaves it there then.
	if err := os.Remove(trashed); err != nil {
		t.Logf("%s stays in the Trash: %v", trashed, err)
	}
}
