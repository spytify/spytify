package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPlaceOnLinux(t *testing.T) {
	f := outputFacts(t.TempDir(), nil, false)
	if f["output.drive"] != "fixed" || f["output.fs"] == "" || f["output.cloud"] != "none" {
		t.Errorf("facts %v", f)
	}
	oneOf(t, f, "output.music", "home", "elsewhere")
}

// The deepest mount holds a path, the last one mounted on a folder wins,
// and a space in a mount's folder is unescaped.
func TestMounts(t *testing.T) {
	mounts := parseMounts("/dev/sda2 / ext4 rw 0 0\n" +
		"/dev/sdb1 /media/ada/My\\040Disk vfat rw 0 0\n" +
		"nas:/music /mnt/nas nfs4 rw 0 0\n" +
		"tmpfs /mnt/nas tmpfs rw 0 0\n")
	for path, want := range map[string]string{
		"/home/ada/Music":            "ext4",
		"/media/ada/My Disk/Spytify": "vfat",
		"/mnt/nas/Spytify":           "tmpfs",
	} {
		if m, ok := mountOf(path, mounts); !ok || m.fstype != want {
			t.Errorf("%s on %+v, want %s", path, m, want)
		}
	}
}

// A disk on the desktop's mount folder is external, one gone from it is
// missing, and a network mount is network.
func TestVolumesOnLinux(t *testing.T) {
	tmp := t.TempDir()
	nas := filepath.Join(tmp, "nas")
	if err := os.MkdirAll(filepath.Join(nas, "Spytify"), 0o755); err != nil {
		t.Fatal(err)
	}
	mounts := []mount{{"/", "ext4"}, {nas, "nfs4"}}
	if drive, fs := volumeIn(filepath.Join(nas, "Spytify"), mounts); drive != "network" || fs != "nfs4" {
		t.Errorf("on the NAS: %s %s", drive, fs)
	}
	if drive, _ := volumeIn("/media/ada/spytify-no-such-disk/Spytify", mounts); drive != "missing" {
		t.Errorf("unplugged: %s", drive)
	}
	if drive, _ := volumeIn(tmp, mounts); drive != "fixed" {
		t.Errorf("on /: %s", drive)
	}
}

// The XDG user folders follow the system's language; one set to the home
// folder is turned off.
func TestUserDirs(t *testing.T) {
	home := "/home/ada"
	for line, want := range map[string][2]string{
		`XDG_MUSIC_DIR="$HOME/Musique"`:    {"MUSIC", "/home/ada/Musique"},
		`XDG_VIDEOS_DIR="/data/videos"`:    {"VIDEOS", "/data/videos"},
		`XDG_DESKTOP_DIR="$HOME/"`:         {},
		`# XDG_MUSIC_DIR="$HOME/Music"`:    {},
		`XDG_DOWNLOAD_DIR="relative/path"`: {},
	} {
		key, path, ok := parseUserDir(line, home)
		if ok != (want[0] != "") || key != want[0] || path != want[1] {
			t.Errorf("%s: %q %q %v, want %q", line, key, path, ok, want)
		}
	}
}

// A cloud's FUSE mount, rclone's included, and GNOME's online accounts are
// clouds, named without the account.
func TestCloudsOnLinux(t *testing.T) {
	if got := cloudOf("/run/user/1000/gvfs/google-drive:host=example.com,user=ada/Spytify"); got != "Google Drive" {
		t.Errorf("GNOME's Google Drive: %q", got)
	}
	tmp := t.TempDir()
	mounts := []mount{{"/", "ext4"}, {filepath.Join(tmp, "remote"), "fuse.rclone"}, {filepath.Join(tmp, "od"), "fuse.onedriver"}, {filepath.Join(tmp, "ssh"), "fuse.sshfs"}}
	for name, want := range map[string]string{"remote": "other", "od": "OneDrive", "ssh": "none"} {
		dir := filepath.Join(tmp, name, "Spytify")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if got := cloudIn(dir, mounts); got != want {
			t.Errorf("on %s: %q, want %q", name, got, want)
		}
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, "Nextcloud", "Spytify")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := cloudOf(dir); got != "Nextcloud" {
		t.Errorf("~/Nextcloud: %q", got)
	}
}
