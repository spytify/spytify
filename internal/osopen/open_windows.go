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
	return onShellThread(func() error {
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
	return onShellThread(func() error {
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

// App starts a program by its name, as the Run dialog does: the shell looks
// it up in App Paths (where installers register it) and on PATH with
// PATHEXT (where the Store puts its app aliases), so no cmd /C start is
// needed. A name that resolves to nothing fails at once, with no dialog:
// the recorder that calls it has no window to show one on.
func App(name string) error {
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	info := shellExecuteInfo{
		mask: seeMaskNoAsync | seeMaskFlagNoUI,
		verb: verb,
		file: file,
		show: windows.SW_SHOWNORMAL,
	}
	info.size = uint32(unsafe.Sizeof(info))
	return onShellThread(func() error {
		if r, _, err := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&info))); r == 0 {
			return err
		}
		return nil
	})
}

// onShellThread runs fn with COM on its thread, as ShellExecute wants, since
// a file type's handler can be a shell extension. The thread is one of its
// own: locked to the goroutine, it ends with it.
func onShellThread(fn func() error) error {
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

// x/sys/windows has ShellExecute but not ShellExecuteEx, the one that can
// be told to show no error dialog.
var procShellExecuteExW = shell32.NewProc("ShellExecuteExW")

const (
	seeMaskNoAsync  = 0x00000100 // the call's thread ends right after it
	seeMaskFlagNoUI = 0x00000400
)

// shellExecuteInfo is SHELLEXECUTEINFOW.
type shellExecuteInfo struct {
	size      uint32
	mask      uint32
	hwnd      windows.Handle
	verb      *uint16
	file      *uint16
	params    *uint16
	dir       *uint16
	show      int32
	instApp   windows.Handle
	idList    uintptr
	class     *uint16
	keyClass  windows.Handle
	hotKey    uint32
	iconOrMon windows.Handle
	process   windows.Handle
}
