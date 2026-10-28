package opengl

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"

	"github.com/spytify/spytify/internal/proc"
)

var allowSetForegroundWindow = user32.NewProc("AllowSetForegroundWindow")

// Software reports whether this copy of the app draws with Mesa's OpenGL.
func Software() bool { return os.Getenv(SoftwareEnv) == "software" }

// mesaDir is opengl\ beside Spytify.exe, where scripts/build-windows.sh
// puts Mesa.
func mesaDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(filepath.Dir(exe), "opengl")
	if _, err := os.Stat(filepath.Join(dir, "opengl32.dll")); err != nil {
		return "", fmt.Errorf("no Mesa beside Spytify.exe: %w", err)
	}
	return dir, nil
}

// UseSoftware loads Mesa's OpenGL, which Fyne then draws with. Nothing may
// have loaded Windows' opengl32.dll before: a Spytify.exe that links it
// has it loaded before main, and scripts/build-windows.sh builds one that
// does not.
func UseSoftware() error {
	dir, err := mesaDir()
	if err != nil {
		return err
	}
	name, _ := windows.UTF16PtrFromString("opengl32.dll")
	var loaded windows.Handle
	if windows.GetModuleHandleEx(windows.GET_MODULE_HANDLE_EX_FLAG_UNCHANGED_REFCOUNT, name, &loaded) == nil {
		return errors.New("Windows' opengl32.dll is loaded already: this Spytify.exe links it")
	}
	// With its path, Windows finds libgallium_wgl.dll beside it.
	if _, err := windows.LoadLibraryEx(filepath.Join(dir, "opengl32.dll"), 0, windows.LOAD_WITH_ALTERED_SEARCH_PATH); err != nil {
		return fmt.Errorf("Mesa's opengl32.dll: %w", err)
	}
	return nil
}

// StartSoftware starts the app again to draw with Mesa's OpenGL, which
// this copy cannot load any more: Check loaded Windows' opengl32.dll, and
// Windows hands that one to whatever asks for opengl32.dll by name.
func StartSoftware() error {
	if _, err := mesaDir(); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := proc.Command(exe, os.Args[1:]...)
	cmd.Env = append(os.Environ(), SoftwareEnv+"=software")
	// Mesa tries its Direct3D 12 driver first; llvmpipe, which it falls
	// back on, is the renderer shipped for and tested. Mesa reads it from
	// the environment the copy starts with.
	if os.Getenv("GALLIUM_DRIVER") == "" {
		cmd.Env = append(cmd.Env, "GALLIUM_DRIVER=llvmpipe")
	}
	// The copy writes where this one would have. A GUI program has stdout
	// and stderr only when started with them, as CI does.
	if hasStdHandle(windows.STD_OUTPUT_HANDLE) {
		cmd.Stdout = os.Stdout
	}
	if hasStdHandle(windows.STD_ERROR_HANDLE) {
		cmd.Stderr = os.Stderr
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	// Its window comes to the front, as this one's would have.
	allowSetForegroundWindow.Call(uintptr(cmd.Process.Pid))
	return cmd.Process.Release()
}

func hasStdHandle(which uint32) bool {
	h, err := windows.GetStdHandle(which)
	return err == nil && h != 0 && h != windows.InvalidHandle
}
