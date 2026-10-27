package openfiles

import (
	"os"
	"time"
)

// Held tells whether anything holds dir: Windows has no simple way to list
// another process's handles, but it refuses to rename a folder while a
// file or folder in it is open, which is also what keeps a drive from
// being ejected. pid is not used: any process counts.
func Held(pid int, dir string) ([]string, error) {
	moved := dir + ".eject-check"
	var err error
	for i := 0; i < 5; i++ {
		if err = os.Rename(dir, moved); err == nil {
			return nil, os.Rename(moved, dir)
		}
		// A virus scanner may look at a new file for a moment.
		time.Sleep(200 * time.Millisecond)
	}
	return []string{dir + ": " + err.Error()}, nil
}
