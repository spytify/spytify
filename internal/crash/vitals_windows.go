package crash

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32              = windows.NewLazySystemDLL("kernel32.dll")
	getProcessMemoryInfo  = kernel32.NewProc("K32GetProcessMemoryInfo")
	getProcessHandleCount = kernel32.NewProc("GetProcessHandleCount")
	getPerformanceInfo    = kernel32.NewProc("K32GetPerformanceInfo")
	getGuiResources       = windows.NewLazySystemDLL("user32.dll").NewProc("GetGuiResources")
)

// GetGuiResources' flags.
const (
	grGDIObjects  = 0
	grUserObjects = 1
)

// processMemoryCounters is PROCESS_MEMORY_COUNTERS_EX.
type processMemoryCounters struct {
	cb             uint32
	_              uint32  // page faults
	_              uintptr // peak working set
	workingSetSize uintptr
	_              [6]uintptr // pool quotas, page file usage and its peak
	privateUsage   uintptr
}

// performanceInformation is PERFORMANCE_INFORMATION: sizes in pages.
type performanceInformation struct {
	cb                       uint32
	commitTotal, commitLimit uintptr
	_                        uintptr // commit peak
	physicalTotal            uintptr
	physicalAvailable        uintptr
	_                        [4]uintptr // system cache and kernel memory
	pageSize                 uintptr
	handleCount              uint32
	_                        [2]uint32 // processes and threads
}

// osVitals are the process's private bytes (what Task Manager calls its
// commit size), working set, handles, threads, and GDI and USER objects,
// then the system's commit charge and its limit, physical memory, and
// handles.
func osVitals() []vital {
	// They all exist from Windows 7 on; Call would panic without one.
	for _, p := range []*windows.LazyProc{getProcessMemoryInfo, getProcessHandleCount, getPerformanceInfo, getGuiResources} {
		if p.Find() != nil {
			return nil
		}
	}
	var v []vital
	self := windows.CurrentProcess()
	pmc := processMemoryCounters{cb: uint32(unsafe.Sizeof(processMemoryCounters{}))}
	if ok, _, _ := getProcessMemoryInfo.Call(uintptr(self), uintptr(unsafe.Pointer(&pmc)), uintptr(pmc.cb)); ok != 0 {
		v = append(v, vital{"process.private_bytes", uint64(pmc.privateUsage)}, vital{"process.working_set_bytes", uint64(pmc.workingSetSize)})
	}
	var handles uint32
	if ok, _, _ := getProcessHandleCount.Call(uintptr(self), uintptr(unsafe.Pointer(&handles))); ok != 0 {
		v = append(v, vital{"process.handles", uint64(handles)})
	}
	if n, ok := threads(); ok {
		v = append(v, vital{"process.threads", uint64(n)})
	}
	// Zero is an answer too: a process without a window has no GDI object.
	gdi, _, _ := getGuiResources.Call(uintptr(self), grGDIObjects)
	user, _, _ := getGuiResources.Call(uintptr(self), grUserObjects)
	v = append(v, vital{"process.gdi_objects", uint64(gdi)}, vital{"process.user_objects", uint64(user)})
	pi := performanceInformation{cb: uint32(unsafe.Sizeof(performanceInformation{}))}
	if ok, _, _ := getPerformanceInfo.Call(uintptr(unsafe.Pointer(&pi)), uintptr(pi.cb)); ok != 0 {
		page := uint64(pi.pageSize)
		v = append(v,
			vital{"system.commit_bytes", uint64(pi.commitTotal) * page},
			vital{"system.commit_limit_bytes", uint64(pi.commitLimit) * page},
			vital{"system.physical_bytes", uint64(pi.physicalTotal) * page},
			vital{"system.physical_available_bytes", uint64(pi.physicalAvailable) * page},
			vital{"system.handles", uint64(pi.handleCount)},
		)
	}
	return v
}

// threads counts the process's threads, the runtime's and those the
// graphics or audio drivers started, from the list of every process.
func threads() (uint32, bool) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0, false
	}
	defer windows.CloseHandle(snap)
	pid := windows.GetCurrentProcessId()
	e := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err := windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if e.ProcessID == pid {
			return e.Threads, true
		}
	}
	return 0, false
}
