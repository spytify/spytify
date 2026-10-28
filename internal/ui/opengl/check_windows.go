package opengl

import (
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32              = windows.NewLazySystemDLL("user32.dll")
	createWindowEx      = user32.NewProc("CreateWindowExW")
	destroyWindow       = user32.NewProc("DestroyWindow")
	getDC               = user32.NewProc("GetDC")
	releaseDC           = user32.NewProc("ReleaseDC")
	describePixelFormat = windows.NewLazySystemDLL("gdi32.dll").NewProc("DescribePixelFormat")
)

const wsPopup = 0x80000000

// pixelFormatDescriptor is PIXELFORMATDESCRIPTOR, 40 bytes.
type pixelFormatDescriptor struct {
	size, version uint16
	flags         uint32
	pixelType     uint8
	_             [19]uint8 // colour, depth and buffer sizes, layer type
	_             [3]uint32 // layer masks
}

// Check looks, in a hidden window as GLFW makes, for a pixel format GLFW
// takes. When it cannot tell, it says nothing and lets Fyne try.
func Check() error {
	// The window and its device context belong to the thread that made
	// them.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	class, _ := windows.UTF16PtrFromString("STATIC")
	hwnd, _, _ := createWindowEx.Call(0, uintptr(unsafe.Pointer(class)), 0, wsPopup, 0, 0, 1, 1, 0, 0, 0, 0)
	if hwnd == 0 {
		return nil
	}
	defer destroyWindow.Call(hwnd)
	dc, _, _ := getDC.Call(hwnd)
	if dc == 0 {
		return nil
	}
	defer releaseDC.Call(hwnd, dc)

	var pfd pixelFormatDescriptor
	size, ptr := unsafe.Sizeof(pfd), uintptr(unsafe.Pointer(&pfd))
	n, _, _ := describePixelFormat.Call(dc, 1, size, ptr)
	for i := uintptr(1); i <= n; i++ {
		if ok, _, _ := describePixelFormat.Call(dc, i, size, ptr); ok != 0 && usable(pfd.flags, pfd.pixelType) {
			return nil
		}
	}
	return fmt.Errorf("%w: none of the screen's %d pixel formats is drawn by a driver", ErrNoDriver, n)
}
