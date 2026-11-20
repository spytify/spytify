package opengl

import (
	"errors"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	gdi32                    = windows.NewLazySystemDLL("gdi32.dll")
	d3dkmtOpenAdapterFromHdc = gdi32.NewProc("D3DKMTOpenAdapterFromHdc")
	d3dkmtQueryAdapterInfo   = gdi32.NewProc("D3DKMTQueryAdapterInfo")
	d3dkmtCloseAdapter       = gdi32.NewProc("D3DKMTCloseAdapter")
)

// openAdapterFromHdc is D3DKMT_OPENADAPTERFROMHDC, 24 bytes.
type openAdapterFromHdc struct {
	dc      uintptr
	adapter uint32
	luid    [2]uint32
	source  uint32
}

// queryAdapterInfo is D3DKMT_QUERYADAPTERINFO, 24 bytes.
type queryAdapterInfo struct {
	adapter  uint32
	kind     uint32
	data     uintptr
	dataSize uint32
}

// openGLInfo is D3DKMT_OPENGLINFO, 528 bytes: the driver's OpenGL DLL.
type openGLInfo struct {
	icd     [windows.MAX_PATH]uint16
	version uint32
	flags   uint32
}

// kmtqaitypeUMOpenGLInfo is KMTQAITYPE_UMOPENGLINFO, from d3dkmthk.h.
const kmtqaitypeUMOpenGLInfo = 2

// NoDriverListed reports whether Windows knows for sure that no graphics
// driver offers OpenGL: the screen's adapter names no OpenGL DLL, and no
// older driver is listed under OpenGLDrivers. It asks the way
// opengl32.dll looks for a driver, without loading it, so that Mesa's can
// still be loaded in this process. When it cannot tell, it says false and
// Check decides.
func NoDriverListed() bool {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	class, _ := windows.UTF16PtrFromString("STATIC")
	hwnd, _, _ := createWindowEx.Call(0, uintptr(unsafe.Pointer(class)), 0, wsPopup, 0, 0, 1, 1, 0, 0, 0, 0)
	if hwnd == 0 {
		return false
	}
	defer destroyWindow.Call(hwnd)
	dc, _, _ := getDC.Call(hwnd)
	if dc == 0 {
		return false
	}
	defer releaseDC.Call(hwnd, dc)
	if d3dkmtOpenAdapterFromHdc.Find() != nil || d3dkmtQueryAdapterInfo.Find() != nil || d3dkmtCloseAdapter.Find() != nil {
		return false
	}

	open := openAdapterFromHdc{dc: dc}
	if status, _, _ := d3dkmtOpenAdapterFromHdc.Call(uintptr(unsafe.Pointer(&open))); status != 0 {
		return false
	}
	defer d3dkmtCloseAdapter.Call(uintptr(unsafe.Pointer(&open.adapter)))
	var info openGLInfo
	query := queryAdapterInfo{
		adapter:  open.adapter,
		kind:     kmtqaitypeUMOpenGLInfo,
		data:     uintptr(unsafe.Pointer(&info)),
		dataSize: uint32(unsafe.Sizeof(info)),
	}
	status, _, _ := d3dkmtQueryAdapterInfo.Call(uintptr(unsafe.Pointer(&query)))
	if status == 0 && info.icd[0] != 0 {
		return false
	}
	return !installableDriverListed()
}

// installableDriverListed reports whether a driver of the kind Windows
// had before display drivers named their OpenGL DLL, as virtual machines'
// still do, is listed under OpenGLDrivers.
func installableDriverListed() bool {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion\OpenGLDrivers`, registry.QUERY_VALUE|registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return !errors.Is(err, registry.ErrNotExist)
	}
	defer k.Close()
	st, err := k.Stat()
	return err != nil || st.SubKeyCount > 0 || st.ValueCount > 0
}
