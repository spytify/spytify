package openfiles

import (
	"os"
	"path/filepath"
	"strconv"
)

// Held lists what process pid holds open under dir: its open files and its
// working folder.
func Held(pid int, dir string) ([]string, error) {
	proc := filepath.Join("/proc", strconv.Itoa(pid))
	fds, err := os.ReadDir(filepath.Join(proc, "fd"))
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, fd := range fds {
		if p, err := os.Readlink(filepath.Join(proc, "fd", fd.Name())); err == nil {
			paths = append(paths, p)
		}
	}
	if cwd, err := os.Readlink(filepath.Join(proc, "cwd")); err == nil {
		paths = append(paths, cwd)
	}
	return under(paths, dir), nil
}
