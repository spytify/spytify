package osopen

import (
	"runtime"

	"golang.org/x/sys/windows"
)

// Open hands a file or folder to the Windows shell, as a double-click in
// Explorer does. It never goes through cmd /C start: Go quotes an argument
// only when it holds a space, and cmd runs whatever follows an unquoted &,
// which a file name, made from a track's title, can hold.
func Open(path string) error {
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	// ShellExecute wants COM on its thread, since a file type's handler can
	// be a shell extension. The thread is one of its own: locked to the
	// goroutine, it ends with it.
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		if windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE) == nil {
			defer windows.CoUninitialize()
		}
		done <- windows.ShellExecute(0, verb, file, nil, nil, windows.SW_SHOWNORMAL)
	}()
	return <-done
}
