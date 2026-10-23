//go:build windows

package target

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// listToolhelp lists every process with its parent from a toolhelp snapshot.
func listToolhelp() []Process {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	var out []Process
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		out = append(out, Process{PID: int(e.ProcessID), Parent: int(e.ParentProcessID), Name: windows.UTF16ToString(e.ExeFile[:])})
	}
	return out
}
