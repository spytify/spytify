//go:build windows

package core

import (
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

// What Windows says of a folder on the system drive: every answer one the
// bug tracker can count, and no cloud.
func TestPlaceOnWindows(t *testing.T) {
	f := outputFacts(t.TempDir(), nil, false)
	if f["output.drive"] != "fixed" || f["output.fs"] == "" {
		t.Errorf("drive %q, file system %q", f["output.drive"], f["output.fs"])
	}
	// The CI runner, Windows Server 2022, answers 0x80070001
	// (ERROR_INVALID_FUNCTION): an error of the file system's. A wrong call
	// would answer E_INVALIDARG or ERROR_INSUFFICIENT_BUFFER.
	oneOf(t, f, "output.cloud", "none", "unknown 0x80070001")
	oneOf(t, f, "output.known", "Music", "Documents", "Pictures", "Videos", "Desktop", "Downloads", "Favorites", "home", "elsewhere")
	oneOf(t, f, "output.music", "home", "moved")
	oneOf(t, f, "output.cfa", "off", "on", "audit", "disks", "disks audit", "unreadable")
}

// A drive letter no drive has reads as missing: the drive was unplugged.
func TestAMissingDrive(t *testing.T) {
	drives, err := windows.GetLogicalDrives()
	if err != nil {
		t.Fatal(err)
	}
	for i := 25; i >= 3; i-- {
		if drives&(1<<i) == 0 {
			dir := filepath.Join(string(rune('A'+i))+`:\`, "Spytify")
			if f := outputFacts(dir, nil, false); f["output.drive"] != "missing" {
				t.Errorf("%s on a %q drive", dir, f["output.drive"])
			}
			return
		}
	}
	t.Skip("every drive letter is taken")
}

func oneOf(t *testing.T, facts map[string]string, key string, want ...string) {
	t.Helper()
	for _, w := range want {
		if facts[key] == w {
			return
		}
	}
	t.Errorf("%s %q, want one of %v", key, facts[key], want)
}
