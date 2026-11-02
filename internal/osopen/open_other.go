//go:build !windows

package osopen

import (
	"runtime"

	"github.com/spytify/spytify/internal/proc"
)

// Open opens a file or folder with its default application.
func Open(path string) error {
	if runtime.GOOS == "darwin" {
		return proc.Command("open", path).Start()
	}
	return proc.Command("xdg-open", path).Start()
}
