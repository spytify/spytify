// Package openfiles tells which files and folders under a folder a process
// holds open, for tests that check spytify lets go of the output folder
// when it is not recording: an external drive with a file still open
// cannot be ejected safely.
package openfiles

import (
	"path/filepath"
	"strings"
)

// under keeps the paths inside dir, dir itself included.
func under(paths []string, dir string) []string {
	if d, err := filepath.EvalSymlinks(dir); err == nil {
		dir = d
	}
	var out []string
	for _, p := range paths {
		if p == dir || strings.HasPrefix(p, dir+string(filepath.Separator)) {
			out = append(out, p)
		}
	}
	return out
}
