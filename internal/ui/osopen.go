package ui

import (
	"path/filepath"
	"runtime"

	"github.com/spytify/spytify/internal/proc"
	"github.com/spytify/spytify/internal/trash"
)

// moveToTrash moves a file to the system's trash. Tests replace it.
var moveToTrash = trash.Move

// reveal shows a file in the OS file manager, selected where possible.
// Tests replace it.
var reveal = func(path string) error {
	switch runtime.GOOS {
	case "darwin":
		return proc.Command("open", "-R", path).Start()
	case "windows":
		return proc.Command("explorer", "/select,", path).Start()
	}
	return proc.Command("xdg-open", filepath.Dir(path)).Start()
}

// openPath opens a file or folder with its default application. Tests
// replace it.
var openPath = func(path string) error {
	switch runtime.GOOS {
	case "darwin":
		return proc.Command("open", path).Start()
	case "windows":
		return proc.Command("cmd", "/C", "start", "", path).Start()
	}
	return proc.Command("xdg-open", path).Start()
}
