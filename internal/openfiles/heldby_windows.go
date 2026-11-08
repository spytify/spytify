package openfiles

import (
	"fmt"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	rstrtmgr            = windows.NewLazySystemDLL("rstrtmgr.dll")
	rmStartSession      = rstrtmgr.NewProc("RmStartSession")
	rmRegisterResources = rstrtmgr.NewProc("RmRegisterResources")
	rmGetList           = rstrtmgr.NewProc("RmGetList")
	rmEndSession        = rstrtmgr.NewProc("RmEndSession")
)

// rmProcessInfo is RM_PROCESS_INFO.
type rmProcessInfo struct {
	pid         uint32
	started     windows.Filetime
	appName     [256]uint16
	serviceName [64]uint16
	appType     uint32
	appStatus   uint32
	tsSession   uint32
	restartable int32
}

// HeldBy names the programs that hold any of paths open, as the Restart
// Manager sees them: a service by its short name, such as WinDefend, any
// other process by its program's file name, such as OneDrive.exe. An
// application's own name can be a window title, which may name a document:
// it is never used. It says nothing when it cannot tell.
func HeldBy(paths ...string) []string {
	if rstrtmgr.Load() != nil {
		return nil
	}
	var session uint32
	var key [33]uint16
	if r, _, _ := rmStartSession.Call(uintptr(unsafe.Pointer(&session)), 0, uintptr(unsafe.Pointer(&key[0]))); r != 0 {
		return nil
	}
	defer rmEndSession.Call(uintptr(session))
	var files []*uint16
	for _, p := range paths {
		if u, err := windows.UTF16PtrFromString(p); err == nil {
			files = append(files, u)
		}
	}
	if len(files) == 0 {
		return nil
	}
	if r, _, _ := rmRegisterResources.Call(uintptr(session), uintptr(len(files)), uintptr(unsafe.Pointer(&files[0])), 0, 0, 0, 0); r != 0 {
		return nil
	}
	infos := make([]rmProcessInfo, 4)
	for {
		var needed, reasons uint32
		n := uint32(len(infos))
		r, _, _ := rmGetList.Call(uintptr(session), uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&n)), uintptr(unsafe.Pointer(&infos[0])), uintptr(unsafe.Pointer(&reasons)))
		if windows.Errno(r) == windows.ERROR_MORE_DATA {
			infos = make([]rmProcessInfo, needed)
			continue
		}
		if r != 0 {
			return nil
		}
		var names []string
		for _, info := range infos[:n] {
			names = append(names, processName(info))
		}
		return names
	}
}

func processName(info rmProcessInfo) string {
	if s := windows.UTF16ToString(info.serviceName[:]); s != "" {
		return s
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, info.pid)
	if err != nil {
		return fmt.Sprintf("process %d", info.pid)
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return fmt.Sprintf("process %d", info.pid)
	}
	return filepath.Base(windows.UTF16ToString(buf[:size]))
}
