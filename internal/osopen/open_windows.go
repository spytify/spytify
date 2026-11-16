package osopen

import (
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	shell32                        = windows.NewLazySystemDLL("shell32.dll")
	procILCreateFromPathW          = shell32.NewProc("ILCreateFromPathW")
	procILFree                     = shell32.NewProc("ILFree")
	procSHOpenFolderAndSelectItems = shell32.NewProc("SHOpenFolderAndSelectItems")
)

// Open hands a file, a folder or a web page to the Windows shell, as a
// double-click in Explorer does. It never goes through cmd /C start: Go quotes an argument
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
	return onCOMThread(func() error {
		return windows.ShellExecute(0, verb, file, nil, nil, windows.SW_SHOWNORMAL)
	})
}

// Reveal shows a file selected in its folder, in an Explorer window. The
// shell does it in-process: no explorer.exe is started with the file on its
// command line.
func Reveal(path string) error {
	file, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	return onCOMThread(func() error {
		pidl, _, _ := procILCreateFromPathW.Call(uintptr(unsafe.Pointer(file)))
		if pidl == 0 {
			return fmt.Errorf("no shell item for %s", path)
		}
		defer procILFree.Call(pidl)
		// With no child items, the shell selects the item pidl names.
		hr, _, _ := procSHOpenFolderAndSelectItems.Call(pidl, 0, 0, 0)
		if int32(hr) < 0 {
			return fmt.Errorf("show %s in its folder: %w", path, windows.Errno(uint32(hr)))
		}
		return nil
	})
}

// onCOMThread runs fn on a thread of its own with COM initialised, as the
// shell wants, since a file type's handler can be a shell extension. Locked
// to the goroutine, the thread ends with it.
func onCOMThread(fn func() error) error {
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		if windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE) == nil {
			defer windows.CoUninitialize()
		}
		done <- fn()
	}()
	return <-done
}
