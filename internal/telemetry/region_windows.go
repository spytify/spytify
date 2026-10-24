package telemetry

import (
	"syscall"
	"unsafe"
)

var getUserDefaultGeoName = syscall.NewLazyDLL("kernel32.dll").NewProc("GetUserDefaultGeoName")

// osLocale is the country of Windows's region setting (Windows 10 1709
// and later).
func osLocale() string {
	if getUserDefaultGeoName.Find() != nil {
		return ""
	}
	buf := make([]uint16, 16)
	n, _, _ := getUserDefaultGeoName.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}
