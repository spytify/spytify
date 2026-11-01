//go:build unix && !darwin

package trash

import (
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// trashInfo reads a .trashinfo: its Path, unescaped, and its DeletionDate.
func trashInfo(t *testing.T, path string) (string, time.Time) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	if len(lines) != 3 || lines[0] != "[Trash Info]" || !strings.HasPrefix(lines[1], "Path=") || !strings.HasPrefix(lines[2], "DeletionDate=") {
		t.Fatalf("%s:\n%s", path, b)
	}
	original, err := url.PathUnescape(strings.TrimPrefix(lines[1], "Path="))
	if err != nil {
		t.Fatal(err)
	}
	at, err := time.ParseInLocation("2006-01-02T15:04:05", strings.TrimPrefix(lines[2], "DeletionDate="), time.Local)
	if err != nil {
		t.Fatal(err)
	}
	return original, at
}

// inTrash checks that the file named name in the trash dir holds what the
// file at original held, and that its .trashinfo says it came from path.
func inTrash(t *testing.T, dir, name, original, path string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "files", name))
	if err != nil {
		t.Fatalf("not in the trash as %s: %v", name, err)
	}
	if string(b) != filepath.Base(original) {
		t.Errorf("%s holds %q, not %s", name, b, original)
	}
	got, at := trashInfo(t, filepath.Join(dir, "info", name+".trashinfo"))
	if got != path {
		t.Errorf("%s came from %q, its .trashinfo says %q", name, path, got)
	}
	if d := time.Since(at); d < -time.Second || d > time.Minute {
		t.Errorf("%s deleted at %v", name, at)
	}
}

// A file on the home's device goes to the home trash, made when missing,
// with its absolute path; a second of the same name gets a name of its own.
func TestMoveToTheHomeTrash(t *testing.T) {
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	music := t.TempDir()
	first := testFile(t, music, "M83 - Wait (partial).flac")
	if err := Move(first); err != nil {
		t.Fatal(err)
	}
	gone(t, first)
	home := filepath.Join(data, "Trash")
	inTrash(t, home, "M83 - Wait (partial).flac", first, first)
	for _, sub := range []string{"", "files", "info"} {
		if st, err := os.Stat(filepath.Join(home, sub)); err != nil || st.Mode().Perm() != 0o700 {
			t.Errorf("Trash/%s: %v, %v", sub, st.Mode(), err)
		}
	}

	again := testFile(t, filepath.Join(music, "Other folder"), "M83 - Wait (partial).flac")
	if err := Move(again); err != nil {
		t.Fatal(err)
	}
	inTrash(t, home, "M83 - Wait (partial).2.flac", again, again)
	inTrash(t, home, "M83 - Wait (partial).flac", first, first)
}

// A relative XDG_DATA_HOME is ignored, as the specification says: the
// home trash is then ~/.local/share/Trash.
func TestARelativeDataHomeIsIgnored(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "relative")
	path := testFile(t, t.TempDir(), "Song.mp3")
	if err := Move(path); err != nil {
		t.Fatal(err)
	}
	inTrash(t, filepath.Join(home, ".local", "share", "Trash"), "Song.mp3", path, path)
}

// onDrive makes the folder mount look like a drive of its own: Move sees
// another device there.
func onDrive(t *testing.T, mount string) {
	t.Helper()
	real := deviceOf
	t.Cleanup(func() { deviceOf = real })
	deviceOf = func(path string) (uint64, error) {
		if _, err := real(path); err != nil {
			return 0, err
		}
		if path == mount || strings.HasPrefix(path, mount+string(filepath.Separator)) {
			return 2, nil
		}
		return 1, nil
	}
}

// A file on another drive goes to a trash at the top of that drive, which
// names it from there: a move to the home trash would be a copy.
func TestMoveOnAnotherDrive(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	uid := strconv.Itoa(os.Getuid())
	for _, c := range []struct {
		name  string
		setup func(t *testing.T, mount string)
		trash string
	}{
		{"no .Trash", func(*testing.T, string) {}, ".Trash-" + uid},
		{"a sticky .Trash", func(t *testing.T, mount string) {
			mkdir(t, filepath.Join(mount, ".Trash"), 0o777|os.ModeSticky)
		}, filepath.Join(".Trash", uid)},
		// The specification forbids these two.
		{"a .Trash without the sticky bit", func(t *testing.T, mount string) {
			mkdir(t, filepath.Join(mount, ".Trash"), 0o777)
		}, ".Trash-" + uid},
		{"a .Trash that is a link", func(t *testing.T, mount string) {
			sticky := filepath.Join(mount, "sticky")
			mkdir(t, sticky, 0o777|os.ModeSticky)
			if err := os.Symlink(sticky, filepath.Join(mount, ".Trash")); err != nil {
				t.Fatal(err)
			}
		}, ".Trash-" + uid},
	} {
		t.Run(c.name, func(t *testing.T) {
			mount := t.TempDir()
			onDrive(t, mount)
			c.setup(t, mount)
			path := testFile(t, mount, filepath.Join("Music", "M83", "Reunion (low quality).mp3"))
			if err := Move(path); err != nil {
				t.Fatal(err)
			}
			gone(t, path)
			inTrash(t, filepath.Join(mount, c.trash), "Reunion (low quality).mp3", path, filepath.Join("Music", "M83", "Reunion (low quality).mp3"))
		})
	}
}

// With no trash it can make on the drive, Move leaves the file where it is
// and says why: it never deletes instead.
func TestNoTrashOnTheDriveLeavesTheFile(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root writes in a read-only folder")
	}
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	mount := t.TempDir()
	onDrive(t, mount)
	path := testFile(t, mount, "Song.mp3")
	if err := os.Chmod(mount, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(mount, 0o755) })
	err := Move(path)
	if err == nil || !strings.Contains(err.Error(), "no trash folder") {
		t.Fatalf("Move with no trash to make: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the file is gone: %v", err)
	}
}

func mkdir(t *testing.T, dir string, mode os.FileMode) {
	t.Helper()
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// Chmod, as the umask would take bits off Mkdir's mode.
	if err := os.Chmod(dir, mode); err != nil {
		t.Fatal(err)
	}
}
