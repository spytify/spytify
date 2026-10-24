package ui

import (
	"os/exec"
	"path/filepath"
	"runtime"
)

// reveal shows a file in the OS file manager, selected where possible.
// Tests replace it.
var reveal = func(path string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", "-R", path).Start()
	case "windows":
		return exec.Command("explorer", "/select,", path).Start()
	}
	return exec.Command("xdg-open", filepath.Dir(path)).Start()
}

// openPath opens a file or folder with its default application. Tests
// replace it.
var openPath = func(path string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", path).Start()
	case "windows":
		return exec.Command("cmd", "/C", "start", "", path).Start()
	}
	return exec.Command("xdg-open", path).Start()
}
