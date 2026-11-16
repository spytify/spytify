//go:build !windows

package osopen

import (
	"runtime"

	"github.com/spytify/spytify/internal/proc"
)

// Open opens a file or folder with its default application, or a web page
// in the default browser.
func Open(path string) error {
	if runtime.GOOS == "darwin" {
		return proc.Command("/usr/bin/open", path).Start()
	}
	return proc.Command("xdg-open", path).Start()
}
