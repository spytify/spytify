//go:build windows

package crash

import (
	"os"

	"golang.org/x/sys/windows"
)

// captured is the file CaptureStderr gave the runtime, kept so that the
// garbage collector never closes it.
//
//lint:ignore U1000 written only: holding the file is its use
var captured *os.File

// CaptureStderr has the runtime print to StderrOf(path) when the program
// has no stderr, as Spytify.exe opened from Explorer has none: a fatal
// error's message goes to stderr only (see Headline), and WithLost finds
// it there at the next launch. A console, a pipe or a file the program was
// given keeps it. Only the runtime looks the handle up at each write:
// os.Stderr, and the logs that write to it, keep the handle the program
// started with.
func CaptureStderr(path string) {
	if h, err := windows.GetStdHandle(windows.STD_ERROR_HANDLE); err == nil && h != 0 && h != windows.InvalidHandle {
		var mode uint32
		if t, _ := windows.GetFileType(h); t != windows.FILE_TYPE_CHAR || windows.GetConsoleMode(h, &mode) == nil {
			return
		}
		// A character device that is no console: the null device.
	}
	f, err := os.OpenFile(StderrOf(path), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return
	}
	if windows.SetStdHandle(windows.STD_ERROR_HANDLE, windows.Handle(f.Fd())) != nil {
		f.Close()
		return
	}
	captured = f
}
