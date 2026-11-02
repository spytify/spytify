package core

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// mount is one line of /proc/self/mounts.
type mount struct{ dir, fstype string }

func readMounts() []mount {
	b, err := os.ReadFile("/proc/self/mounts")
	if err != nil {
		return nil
	}
	return parseMounts(string(b))
}

func parseMounts(text string) []mount {
	var out []mount
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		out = append(out, mount{dir: unescapeMount(f[1]), fstype: f[2]})
	}
	return out
}

// unescapeMount undoes the octal escapes of /proc/self/mounts: a space is
// \040.
func unescapeMount(s string) string {
	return strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`).Replace(s)
}

// mountOf is the mount path is on: the deepest that holds it, the last
// of those mounted on the same folder.
func mountOf(path string, mounts []mount) (mount, bool) {
	var found mount
	ok := false
	for _, m := range mounts {
		if inFolder(path, m.dir) && (!ok || len(m.dir) >= len(found.dir)) {
			found, ok = m, true
		}
	}
	return found, ok
}

var networkFS = map[string]bool{
	"nfs": true, "nfs4": true, "cifs": true, "smb3": true, "smbfs": true, "afs": true, "ceph": true, "glusterfs": true,
	"davfs": true, "fuse.davfs2": true, "fuse.sshfs": true, "fuse.rclone": true, "fuse.gvfsd-fuse": true,
}

// onMedia says whether path is where the desktop mounts disks and cards.
func onMedia(path string) bool {
	return inFolder(path, "/media") || inFolder(path, "/run/media")
}

// volumeOf is the kind of drive dir is on, "external" for a disk or a
// card the desktop mounted, and its file system as mounted.
func volumeOf(dir string) (drive, fs string) {
	return volumeIn(dir, readMounts())
}

func volumeIn(dir string, mounts []mount) (drive, fs string) {
	m, ok := mountOf(resolved(dir), mounts)
	switch {
	case !ok:
		return "unknown", ""
	// Unplugged, its folder in /media is gone with it.
	case onMedia(dir) && !onMedia(m.dir):
		return "missing", ""
	case networkFS[m.fstype]:
		return "network", m.fstype
	case onMedia(m.dir):
		return "external", m.fstype
	}
	return "fixed", m.fstype
}

// cloudOf names the cloud that syncs dir: a FUSE mount of one (rclone,
// onedriver…), GNOME's online accounts, or a cloud's own folder in the
// home folder, such as ~/Dropbox; or says "none".
func cloudOf(dir string) string {
	return cloudIn(dir, readMounts())
}

func cloudIn(dir string, mounts []mount) string {
	at := resolved(dir)
	if m, ok := mountOf(at, mounts); ok {
		if sub, fuse := strings.CutPrefix(m.fstype, "fuse."); fuse {
			if c := cloudName(sub); c != "other" || sub == "rclone" {
				return c
			}
		}
	}
	for _, p := range []string{dir, at} {
		if _, rest, ok := strings.Cut(p, "/gvfs/"); ok {
			name, _, _ := strings.Cut(rest, "/")
			return cloudName(name)
		}
	}
	if c := homeCloud(dir); c != "" {
		return c
	}
	return "none"
}

// userDirs are the XDG user folders, by key such as MUSIC, from
// user-dirs.dirs: their names follow the system's language (~/Musique).
func userDirs(home string) map[string]string {
	config, err := os.UserConfigDir()
	if err != nil {
		return nil
	}
	f, err := os.Open(filepath.Join(config, "user-dirs.dirs"))
	if err != nil {
		return nil
	}
	defer f.Close()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, path, ok := parseUserDir(sc.Text(), home)
		if ok {
			out[key] = path
		}
	}
	return out
}

// parseUserDir reads one line such as XDG_MUSIC_DIR="$HOME/Music". A
// folder set to the home folder itself is turned off.
func parseUserDir(line, home string) (key, path string, ok bool) {
	line = strings.TrimSpace(line)
	name, value, found := strings.Cut(line, "=")
	if !found || !strings.HasPrefix(name, "XDG_") || !strings.HasSuffix(name, "_DIR") {
		return "", "", false
	}
	value = strings.Trim(value, `"`)
	if rest, ok := strings.CutPrefix(value, "$HOME"); ok {
		value = home + rest
	}
	if !filepath.IsAbs(value) || samePath(value, home) {
		return "", "", false
	}
	return strings.TrimSuffix(strings.TrimPrefix(name, "XDG_"), "_DIR"), filepath.Clean(value), true
}

// knownFolders are the user's folders, the home folder last.
func knownFolders() []namedFolder {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	set := userDirs(home)
	var out []namedFolder
	for _, f := range [][3]string{
		{"Music", "MUSIC", "Music"}, {"Documents", "DOCUMENTS", "Documents"}, {"Pictures", "PICTURES", "Pictures"},
		{"Videos", "VIDEOS", "Videos"}, {"Desktop", "DESKTOP", "Desktop"}, {"Downloads", "DOWNLOAD", "Downloads"},
	} {
		path, ok := set[f[1]]
		if !ok {
			path = filepath.Join(home, f[2])
		}
		out = append(out, namedFolder{f[0], path})
	}
	return append(out, namedFolder{"home", home})
}

func systemFacts(map[string]string) {}
