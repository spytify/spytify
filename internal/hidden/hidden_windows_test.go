package hidden

import (
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

// Explorer hides a folder by its attribute, not by a dot in its name.
func TestMkdirAllSetsTheHiddenAttribute(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".covers")
	if err := MkdirAll(dir); err != nil {
		t.Fatal(err)
	}
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		t.Fatal(err)
	}
	attrs, err := windows.GetFileAttributes(p)
	if err != nil {
		t.Fatal(err)
	}
	if attrs&windows.FILE_ATTRIBUTE_HIDDEN == 0 || attrs&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
		t.Errorf("attributes %#x, want a hidden folder", attrs)
	}
}
