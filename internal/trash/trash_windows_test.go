package trash

import (
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
