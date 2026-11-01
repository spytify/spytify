package core

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// volumeOf is the kind of drive dir is on, "external" for a volume of
// its own in /Volumes (a disk, a card, a disk image), and its file system.
func volumeOf(dir string) (drive, fs string) {
	if name := firstUnder(dir, "/Volumes"); name != "" {
		if _, err := os.Stat(filepath.Join("/Volumes", name)); err != nil {
			return "missing", ""
		}
	}
	var st unix.Statfs_t
	if unix.Statfs(existingFolder(dir), &st) != nil {
		return "unknown", ""
	}
	fs = unix.ByteSliceToString(st.Fstypename[:])
	switch mnt := unix.ByteSliceToString(st.Mntonname[:]); {
	case st.Flags&unix.MNT_LOCAL == 0:
		return "network", fs
	case mnt == "/" || mnt == "/System/Volumes/Data":
		return "fixed", fs
	}
	return "external", fs
}

// cloudOf names the cloud that syncs dir: iCloud Drive, a File Provider
// folder in ~/Library/CloudStorage (OneDrive, Google Drive, Dropbox…), or
// a cloud's own folder in the home folder; or says "none".
func cloudOf(dir string) string {
	for _, home := range homes() {
		for _, p := range []string{dir, resolved(dir)} {
			if inFolder(p, filepath.Join(home, "Library", "Mobile Documents")) {
				return "iCloud"
			}
			if name := firstUnder(p, filepath.Join(home, "Library", "CloudStorage")); name != "" {
				return cloudName(name)
			}
		}
	}
	if c := homeCloud(dir); c != "" {
		return c
	}
	return "none"
}

// knownFolders are the user's folders, the home folder last. macOS keeps
// them in English on disk, whatever Finder shows.
func knownFolders() []namedFolder {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	var out []namedFolder
	for _, f := range [][2]string{
		{"Music", "Music"}, {"Documents", "Documents"}, {"Pictures", "Pictures"}, {"Videos", "Movies"},
		{"Desktop", "Desktop"}, {"Downloads", "Downloads"},
	} {
		out = append(out, namedFolder{f[0], filepath.Join(home, f[1])})
	}
	return append(out, namedFolder{"home", home})
}

func systemFacts(map[string]string) {}
