//go:build unix && !darwin

package trash

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Move moves the file at path to a trash of the freedesktop.org Trash
// specification: the home trash, $XDG_DATA_HOME/Trash, for a file on its
// device; for a file on another device, the trash at the top of that
// device, which keeps the move a rename. The specification lets a program
// copy such a file to the home trash instead: Move does not, as a copy of
// a big file is slow and can fill the home's disk. Each file goes with its
// .trashinfo, which file managers read to put it back.
func Move(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	dev, err := deviceOf(abs)
	if err != nil {
		return err
	}
	home, err := homeTrash()
	if err != nil {
		return err
	}
	if d, err := deviceOf(existing(home)); err == nil && d == dev {
		return moveInto(home, abs, abs)
	}
	top := topDir(abs, dev)
	dir, err := topTrash(top)
	if err != nil {
		return &os.PathError{Op: "trash", Path: path, Err: fmt.Errorf("its drive has no trash folder Spytify can use: %w", err)}
	}
	// A trash at the top of a drive names its files from there, so they go
	// back where they were wherever the drive is mounted next.
	rel, err := filepath.Rel(top, abs)
	if err != nil {
		return err
	}
	return moveInto(dir, abs, rel)
}

// deviceOf is the device the file at path is on. Tests replace it to give
// a folder a device of its own.
var deviceOf = func(path string) (uint64, error) {
	st, err := os.Lstat(path)
	if err != nil {
		return 0, err
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, fmt.Errorf("%s: no device", path)
	}
	return uint64(sys.Dev), nil
}

// homeTrash is $XDG_DATA_HOME/Trash, ~/.local/share/Trash when the variable
// is unset or, as the specification says to ignore it then, relative.
func homeTrash() (string, error) {
	data := os.Getenv("XDG_DATA_HOME")
	if !filepath.IsAbs(data) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		data = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(data, "Trash"), nil
}

// existing is path, or the closest folder above it that exists: the home
// trash is made only when a file goes there.
func existing(path string) string {
	for {
		if _, err := os.Lstat(path); err == nil {
			return path
		}
		parent := filepath.Dir(path)
		if parent == path {
			return path
		}
		path = parent
	}
}

// topDir is the top folder of device dev that holds path: its mount point.
func topDir(path string, dev uint64) string {
	dir := filepath.Dir(path)
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			return dir
		}
		if d, err := deviceOf(parent); err != nil || d != dev {
			return dir
		}
		dir = parent
	}
}

// topTrash is the trash at top: $top/.Trash/$uid when an administrator made
// $top/.Trash a sticky folder that every user can write to, else
// $top/.Trash-$uid, made when missing.
func topTrash(top string) (string, error) {
	uid := strconv.Itoa(os.Getuid())
	shared := filepath.Join(top, ".Trash")
	// Lstat: the specification forbids a .Trash that is a link.
	if st, err := os.Lstat(shared); err == nil && st.IsDir() && st.Mode()&fs.ModeSticky != 0 {
		dir := filepath.Join(shared, uid)
		if makeTrash(dir) == nil {
			return dir, nil
		}
	}
	dir := filepath.Join(top, ".Trash-"+uid)
	if err := makeTrash(dir); err != nil {
		return "", err
	}
	return dir, nil
}

// makeTrash makes the trash folder dir and its files and info folders, for
// the user alone, when they are missing.
func makeTrash(dir string) error {
	for _, sub := range []string{"files", "info"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o700); err != nil {
			return err
		}
	}
	if st, err := os.Lstat(dir); err != nil || !st.IsDir() {
		return fmt.Errorf("%s is not a folder", dir)
	}
	return nil
}

// moveInto moves the file at path into the trash folder dir, under a name
// that no file there has, with the .trashinfo that says it came from
// original.
func moveInto(dir, path, original string) error {
	if err := makeTrash(dir); err != nil {
		return err
	}
	if _, err := os.Lstat(path); err != nil {
		return err
	}
	info := "[Trash Info]\nPath=" + (&url.URL{Path: original}).EscapedPath() +
		"\nDeletionDate=" + time.Now().Format("2006-01-02T15:04:05") + "\n"
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	for n := 1; ; n++ {
		name := base
		if n > 1 {
			name = strings.TrimSuffix(base, ext) + "." + strconv.Itoa(n) + ext
		}
		dest := filepath.Join(dir, "files", name)
		if _, err := os.Lstat(dest); err == nil {
			continue
		}
		// The .trashinfo comes first, and only as a new file: it holds the
		// name against another program trashing a file of the same name.
		infoPath := filepath.Join(dir, "info", name+".trashinfo")
		f, err := os.OpenFile(infoPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return err
		}
		_, err = f.WriteString(info)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err == nil {
			err = os.Rename(path, dest)
		}
		if err != nil {
			os.Remove(infoPath)
		}
		return err
	}
}
