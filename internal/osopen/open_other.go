//go:build !windows

package osopen

import (
	"path/filepath"
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

// Reveal shows a file in the file manager, selected where the system can.
func Reveal(path string) error {
	if runtime.GOOS == "darwin" {
		return proc.Command("open", "-R", path).Start()
	}
	return proc.Command("xdg-open", filepath.Dir(path)).Start()
}
