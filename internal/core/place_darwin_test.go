package core

import (
	"os"
	"path/filepath"
	"testing"
)

// A folder on the system's disk: fixed, APFS, in no cloud.
func TestPlaceOnMacOS(t *testing.T) {
	f := outputFacts(t.TempDir(), nil, false)
	if f["output.drive"] != "fixed" || f["output.fs"] != "apfs" || f["output.cloud"] != "none" {
		t.Errorf("facts %v", f)
	}
	oneOf(t, f, "output.music", "home", "elsewhere")
}

// iCloud Drive, the File Provider folders of ~/Library/CloudStorage and a
// cloud's own folder in the home folder are clouds, named without the
// account; a link to one leads there.
func TestCloudFoldersOnMacOS(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for path, want := range map[string]string{
		"Library/CloudStorage/OneDrive-Contoso Ltd/Music/Spytify":           "OneDrive",
		"Library/CloudStorage/GoogleDrive-ada@example.com/My Drive/Spytify": "Google Drive",
		"Library/Mobile Documents/com~apple~CloudDocs/Spytify":              "iCloud",
		"Dropbox/Spytify": "Dropbox",
		"Spytify":         "none",
	} {
		dir := filepath.Join(home, filepath.FromSlash(path))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if got := cloudOf(dir); got != want {
			t.Errorf("%s: %q, want %q", path, got, want)
		}
	}
	link := filepath.Join(home, "Mixes")
	if err := os.Symlink(filepath.Join(home, "Library", "CloudStorage", "OneDrive-Contoso Ltd", "Music"), link); err != nil {
		t.Fatal(err)
	}
	if got := cloudOf(filepath.Join(link, "Spytify")); got != "OneDrive" {
		t.Errorf("through a link: %q", got)
	}
}

// A volume unplugged leaves no folder in /Volumes.
func TestAnUnpluggedVolumeOnMacOS(t *testing.T) {
	if drive, _ := volumeOf("/Volumes/spytify-no-such-disk/Spytify"); drive != "missing" {
		t.Errorf("drive %q", drive)
	}
}

func TestKnownFoldersOnMacOS(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	folders := knownFolders()
	for path, want := range map[string]string{
		"Movies/Spytify": "Videos", "Music/Spytify": "Music", "Spytify": "home",
	} {
		if got := knownFolder(filepath.Join(home, filepath.FromSlash(path)), folders); got != want {
			t.Errorf("%s: %q, want %q", path, got, want)
		}
	}
}
