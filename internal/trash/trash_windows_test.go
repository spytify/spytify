package trash

import (
	"fmt"
	"sync"
	"testing"
	"unsafe"
)

// The structure is as big as Windows' SHFILEOPSTRUCTW on 64-bit Windows.
func TestTheFileOperationIsLaidOutAsWindowsHasIt(t *testing.T) {
	if got := unsafe.Sizeof(shFileOpStruct{}); got != 56 {
		t.Fatalf("SHFILEOPSTRUCTW is %d bytes, Windows has 56", got)
	}
	if got := unsafe.Offsetof(shFileOpStruct{}.fFlags); got != 32 {
		t.Fatalf("fFlags at %d, Windows has it at 32", got)
	}
}

// The file goes to the Recycle Bin, which leaves it out of its folder.
func TestMoveToTheRecycleBin(t *testing.T) {
	realTrash(t)
	path := testFile(t, t.TempDir(), "Spytify trash test.flac")
	if err := Move(path); err != nil {
		t.Fatal(err)
	}
	gone(t, path)
}

// Files sent to the Recycle Bin at the same time all go, as when the
// Library's bin is clicked on row after row: each click moves its file
// from a goroutine of its own. One such move failed with SHFileOperation
// error 0x2, "file not found", for a file that was there (#767).
func TestFilesMovedToTheRecycleBinAtOnceAllGo(t *testing.T) {
	realTrash(t)
	const rounds, files = 5, 20
	failed := 0
	for round := range rounds {
		dir := t.TempDir()
		paths := make([]string, files)
		for i := range paths {
			paths[i] = testFile(t, dir, fmt.Sprintf("Spytify trash test %d-%02d.mp3", round, i))
		}
		errs := make([]error, files)
		var wg sync.WaitGroup
		for i, p := range paths {
			wg.Go(func() { errs[i] = Move(p) })
		}
		wg.Wait()
		for i, err := range errs {
			if err != nil {
				failed++
				t.Errorf("round %d, file %d: %v", round, i, err)
				continue
			}
			gone(t, paths[i])
		}
	}
	if failed > 0 {
		t.Errorf("%d of %d moves made at once failed", failed, rounds*files)
	}
}
